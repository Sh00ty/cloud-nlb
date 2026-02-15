package reconciler

import (
	"container/list"
	"context"
	"fmt"
	"time"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
	"github.com/rs/zerolog"
)

type EventType string

const (
	TargetGroupCreated EventType = "target-group-created"
	DataPlaneAlive     EventType = "data-plane-alive"
	DataPlaneDrained   EventType = "data-plane-drained"
	DataPlaneDead      EventType = "data-plane-dead"
	RunReconcile       EventType = "run-reconcile"
)

type CoordinatorService interface {
	UpdatePlacements(ctx context.Context, ids []models.DataPlaneID, pls []models.Placement) error
}

type DataPlaneStatus struct {
	State models.DataPlaneStatus

	LastEvent *EventType
	Timestamp uint64
}

type TargetGroupStatus struct {
	Assignments map[models.DataPlaneID]struct{}
}

type Event struct {
	Type          EventType
	NodeID        *models.DataPlaneID
	TargetGroupID *models.TargetGroupID

	// local timestamp to make scheduled events
	// to determine if some event happened after it was
	// scheduled
	timestamp uint64
}

func (e Event) String() string {
	if e.NodeID != nil {
		return fmt.Sprintf("{type=%s, node_id=%s}", e.Type, *e.NodeID)
	}
	if e.TargetGroupID != nil {
		return fmt.Sprintf("{type=%s, target_group_id=%s}", e.Type, *e.TargetGroupID)
	}
	return fmt.Sprintf("{type=%s}", e.Type)
}

type Reconciler struct {
	// must be in sync with etcd
	placements map[models.DataPlaneID]models.Placement

	// local target group state, which must help reconciler to make decisions
	// this one will help to check if all target groups have desired replication
	// factor
	targetGroups map[models.TargetGroupID]TargetGroupStatus
	// local dpl state, which must help reconciler make decisions
	dplStatuses map[models.DataPlaneID]DataPlaneStatus

	coordinator CoordinatorService

	targetGroupsReplicationFactor int
	eventTimestamp                uint64
	nodeDeathEventDelay           time.Duration

	eventCh              chan Event
	forceReconcileTicker *time.Ticker
	delayEventTimer      *time.Timer
	delayedEvents        *list.List

	log zerolog.Logger
}

func NewReconciler(
	coordinator CoordinatorService,
	tgReplicationFactor int,
	nodeDeathEventDelay time.Duration,
	forceReconcileInternal time.Duration,
	logger zerolog.Logger,
) *Reconciler {
	return &Reconciler{
		dplStatuses:     make(map[models.DataPlaneID]DataPlaneStatus),
		placements:      make(map[models.DataPlaneID]models.Placement),
		targetGroups:    make(map[models.TargetGroupID]TargetGroupStatus),
		coordinator:     coordinator,
		eventCh:         make(chan Event, 1024),
		delayedEvents:   list.New(),
		delayEventTimer: time.NewTimer(time.Minute),

		forceReconcileTicker: time.NewTicker(forceReconcileInternal),

		targetGroupsReplicationFactor: tgReplicationFactor,
		eventTimestamp:                1,
		nodeDeathEventDelay:           nodeDeathEventDelay,

		log: logger.With().Str("component", "reconciler").Logger(),
	}
}

func (r *Reconciler) GetEventsChan() chan Event {
	return r.eventCh
}

func (r *Reconciler) Init(
	placements map[models.DataPlaneID]models.Placement,
	dataPlaneStates []models.DataPlaneState,
	targetGroups []models.TargetGroupID,
) {
	r.targetGroups = make(map[models.TargetGroupID]TargetGroupStatus, len(targetGroups))
	for _, tgID := range targetGroups {
		r.targetGroups[tgID] = TargetGroupStatus{
			Assignments: make(map[models.DataPlaneID]struct{}, r.targetGroupsReplicationFactor),
		}
	}
	r.dplStatuses = make(map[models.DataPlaneID]DataPlaneStatus, len(dataPlaneStates))
	for _, state := range dataPlaneStates {
		r.dplStatuses[state.ID] = DataPlaneStatus{
			State:     state.Status,
			LastEvent: nil,
			Timestamp: 0,
		}
		r.placements[state.ID] = models.Placement{
			TargetGroups: make(map[models.TargetGroupID]struct{}),
		}
	}
	for dplID, pl := range placements {
		_, exists := r.dplStatuses[dplID]
		if !exists {
			r.dplStatuses[dplID] = DataPlaneStatus{
				State:     models.Drained,
				LastEvent: nil,
				Timestamp: 0,
			}
		}
		r.placements[dplID] = pl
		for tgID := range pl.TargetGroups {
			r.targetGroups[tgID].Assignments[dplID] = struct{}{}
		}
	}
	r.delayEvent(Event{Type: RunReconcile}, 0)
}

func (r *Reconciler) RunReconciler(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-r.eventCh:
			if !ok {
				return nil
			}
			r.eventTimestamp++
			event.timestamp = r.eventTimestamp

			r.log.Info().Msgf("got new event: %v", event)
			r.processIncomingEvent(event, true)
		case <-r.delayEventTimer.C:
			if r.delayedEvents.Len() == 0 {
				continue
			}
			r.handleDelayedEvent()
		case <-r.forceReconcileTicker.C:
			r.reconcile()
		}
	}
}

func (r *Reconciler) processIncomingEvent(event Event, canSchedule bool) {
	switch event.Type {
	case DataPlaneDead:
		if !canSchedule {
			r.handleDataPlaneDeath(event)
			return
		}
		r.delayEvent(event, r.nodeDeathEventDelay)
	case DataPlaneAlive:
		r.handleDataPlaneAlive(event)
	case DataPlaneDrained:
		r.log.Error().Msg("data-plane draining is unimplemented for now")
	case TargetGroupCreated:
		r.handleTargetGroupEvent(event)
	case RunReconcile:
		r.reconcile()
	}
}

func (r *Reconciler) handleTargetGroupEvent(event Event) {
	tgID := *event.TargetGroupID
	if _, exists := r.targetGroups[tgID]; exists {
		return
	}
	r.targetGroups[tgID] = TargetGroupStatus{
		Assignments: make(map[models.DataPlaneID]struct{}),
	}
	r.reconcile()
}

func (r *Reconciler) handleDataPlaneDeath(event Event) {
	var (
		nodeID   = *event.NodeID
		dplState = r.dplStatuses[nodeID]
		oldState = dplState.State
	)
	// if scheduled we need to check if node become alive
	if dplState.LastEvent != nil &&
		*dplState.LastEvent == DataPlaneAlive &&
		dplState.Timestamp > event.timestamp {

		r.log.Warn().Msgf("skip death event: have alive event with bigger timestamp")
		return
	}
	dplState.LastEvent = &event.Type
	dplState.Timestamp = max(event.timestamp, dplState.Timestamp)
	dplState.State = models.Dead
	r.dplStatuses[nodeID] = dplState

	if oldState != models.Alive {
		r.log.Info().Msgf("skip event %s, already applied", event)
		return
	}
	r.reconcile()
}

func (r *Reconciler) handleDataPlaneAlive(event Event) {
	nodeID := *event.NodeID

	dplState, ok := r.dplStatuses[nodeID]
	if !ok {
		r.placements[nodeID] = models.Placement{
			TargetGroups: make(map[models.TargetGroupID]struct{}),
		}
		r.dplStatuses[nodeID] = DataPlaneStatus{
			State:     models.Alive,
			LastEvent: &event.Type,
			Timestamp: r.eventTimestamp,
		}
		r.reconcile()
		return
	}
	// не откладываем alive, нет смысла проверять timestamp

	dplState.Timestamp = max(event.timestamp, dplState.Timestamp)
	dplState.LastEvent = &event.Type
	if dplState.State == models.Alive {
		r.dplStatuses[nodeID] = dplState

		r.log.Info().Msgf("node already alive: %s", event)
		return
	}
	dplState.State = models.Alive
	r.dplStatuses[nodeID] = dplState
	r.reconcile()
}
