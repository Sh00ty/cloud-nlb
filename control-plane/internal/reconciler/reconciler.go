package reconciler

import (
	"cmp"
	"container/list"
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
	"github.com/avast/retry-go/v4"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

/*
В данный момент как-то умной балансировки не предполагается, но можно вычислять вес
таргет групп в виде кол-ва эндпоинтов, тогда можно было бы как-то примеряться к capacity
data-plane(ов). Но для PoC можно себе позволить consistent-hashing или просто
как-то глупо вычислять ноду с минимальным кол-вом таргет групп, наверное так и сделаю.
*/

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

type EventType string

const (
	TargetGroupCreated EventType = "target-group-created"
	DataPlaneAlive     EventType = "data-plane-alive"
	DataPlaneDrained   EventType = "data-plane-drained"
	DataPlaneDead      EventType = "data-plane-dead"
	RunReconcile       EventType = "run-reconcile"
)

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

type scheduledEvent struct {
	ev      Event
	applyAt time.Time
}

func (e scheduledEvent) String() string {
	return fmt.Sprintf("{event=%s, apply_at=%s}", e.ev, e.applyAt.Format(time.DateTime))
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
	nodeDeathScheduleDuration     time.Duration

	eventCh             chan Event
	reconcileTicker     *time.Ticker
	scheduledEventTimer *time.Timer
	scheduledEvents     *list.List

	log zerolog.Logger
}

func NewReconciler(
	coordinator CoordinatorService,
	tgReplicationFactor int,
	nodeDeathScheduleDuration time.Duration,
	rescheduleInternal time.Duration,
	logger zerolog.Logger,
) *Reconciler {
	return &Reconciler{
		dplStatuses:         make(map[models.DataPlaneID]DataPlaneStatus),
		placements:          make(map[models.DataPlaneID]models.Placement),
		targetGroups:        make(map[models.TargetGroupID]TargetGroupStatus),
		coordinator:         coordinator,
		eventCh:             make(chan Event, 1024),
		scheduledEvents:     list.New(),
		scheduledEventTimer: time.NewTimer(time.Minute),

		reconcileTicker: time.NewTicker(rescheduleInternal),

		targetGroupsReplicationFactor: tgReplicationFactor,
		eventTimestamp:                1,
		nodeDeathScheduleDuration:     nodeDeathScheduleDuration,

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
	r.scheduleEvent(
		scheduledEvent{
			ev: Event{
				Type: RunReconcile,
			},
			// once we start it should be applied
			applyAt: time.Now(),
		},
	)
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
			r.log.Info().Msgf("got new event: %v", event)
			r.processIncomingEvent(event, true)
		case <-r.scheduledEventTimer.C:
			if r.scheduledEvents.Len() == 0 {
				continue
			}
			r.handleScheduledEvent()
		case <-r.reconcileTicker.C:
			r.reconcile()
		}
	}
}

func (r *Reconciler) handleScheduledEvent() {
	var (
		front = r.scheduledEvents.Front()
		ev    = front.Value.(scheduledEvent)
	)
	r.scheduledEvents.Remove(front)

	r.log.Info().Msgf("got scheduled event: %v", ev.ev)

	r.processIncomingEvent(ev.ev, false)
	if r.scheduledEvents.Len() == 0 {
		return
	}
	newFront := r.scheduledEvents.Front()
	r.scheduledEventTimer.Reset(time.Until(newFront.Value.(scheduledEvent).applyAt))
}

func (r *Reconciler) scheduleEvent(ev scheduledEvent) {
	r.log.Info().Msgf("scheduled event: %v", ev)

	front := r.scheduledEvents.Front()
	if front == nil {
		r.scheduledEventTimer.Reset(time.Until(ev.applyAt))
		r.scheduledEvents.PushFront(ev)
		return
	}
	frontEv := front.Value.(scheduledEvent)
	if frontEv.applyAt.After(ev.applyAt) {
		r.scheduledEventTimer.Reset(time.Until(ev.applyAt))
		r.scheduledEvents.PushFront(ev)
		return
	}
	for i := front.Next(); i != nil; i = i.Next() {
		listEv := i.Value.(scheduledEvent)
		if listEv.applyAt.After(ev.applyAt) {
			r.scheduledEvents.InsertBefore(ev, i)
			return
		}
	}
	r.scheduledEvents.PushBack(ev)
}

func (r *Reconciler) processIncomingEvent(event Event, canSchedule bool) {
	r.eventTimestamp++
	event.timestamp = r.eventTimestamp
	switch event.Type {
	case DataPlaneDead:
		if !canSchedule {
			r.handleDataPlaneDeath(event)
			return
		}
		r.scheduleEvent(
			scheduledEvent{
				ev:      event,
				applyAt: time.Now().Add(r.nodeDeathScheduleDuration),
			},
		)
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
	dplState.Timestamp = event.timestamp
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

	dplState.Timestamp = event.timestamp
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

func (r *Reconciler) reconcile() {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err := retry.Do(
		func() error {
			return r.reconcileAttempt(ctx)
		},
		retry.Context(ctx),
		retry.Attempts(3),
		retry.DelayType(retry.BackOffDelay),
		retry.OnRetry(func(attempt uint, err error) {
			log.Warn().Err(err).Msgf("failed to complete reconciliation, attempt: %d", attempt)
		}),
	)
	if err != nil {
		log.Error().Err(err).Msg("failed to complete reconciliation event, will try later")
		r.scheduleEvent(scheduledEvent{
			ev:      Event{Type: RunReconcile},
			applyAt: time.Now().Add(30 * time.Second),
		})
		return
	}
}

type dplLoad struct {
	id   models.DataPlaneID
	load int
}

func (r *Reconciler) reconcileAttempt(ctx context.Context) error {
	log.Debug().Msg("start reconciliation")

	// TODO: not an ideal algo, it can make superfluous movements
	// due to two step calculation:
	// 1. randomly replace target groups from dead nodes
	// 2. fixing distribution
	desiredPlacements := r.calculateDesiredLazy()
	return r.fixDataplanePlacements(ctx, desiredPlacements)
}

// try to replace target groups from dead nodes to the other random alive
// later we fill fix distribution
func (r *Reconciler) calculateDesiredLazy() map[models.DataPlaneID]models.Placement {
	var (
		desiredPlacements = make(map[models.DataPlaneID]models.Placement, len(r.placements))

		// put all target groups from dead/drained nodes here
		// then we will randomly assign them to alive nodes
		// WARNING: there is can be duplicated tg if many nodes
		// with the same target group sill go off
		needReplace = make(map[models.TargetGroupID]int, 128)
	)

	log.Debug().Msgf("reconciliation: current data-planes states: %+v", r.dplStatuses)
	log.Debug().Msgf("reconciliation: current placements: %+v", r.placements)
	log.Debug().Msgf("reconciliation: current target groups: %+v", r.targetGroups)

	for id, curPlacement := range r.placements {
		if r.dplStatuses[id].State == models.Alive {
			desiredPlacements[id] = models.Placement{
				Version:      curPlacement.Version,
				TargetGroups: maps.Clone(curPlacement.TargetGroups),
			}
			continue
		}
		version := curPlacement.Version
		if len(curPlacement.TargetGroups) != 0 {
			version++
		}
		desiredPlacements[id] = models.Placement{
			Version:      version,
			TargetGroups: map[models.TargetGroupID]struct{}{},
		}
		for tg := range curPlacement.TargetGroups {
			needReplace[tg]++
		}
	}
	for tgID, as := range r.targetGroups {
		replicate := r.targetGroupsReplicationFactor - len(as.Assignments)
		for range replicate {
			needReplace[tgID] = max(needReplace[tgID]+1, r.targetGroupsReplicationFactor)
		}
	}
	return r.replaceMissedTargetGroups(desiredPlacements, needReplace)
}

func (r *Reconciler) replaceMissedTargetGroups(
	desiredPlacements map[models.DataPlaneID]models.Placement,
	problemTgs map[models.TargetGroupID]int,
) map[models.DataPlaneID]models.Placement {
targetsLoop:
	for tg, needReplicate := range problemTgs {
		for range needReplicate {
			for dplID, pl := range desiredPlacements {
				if r.dplStatuses[dplID].State != models.Alive {
					continue
				}
				_, exists := pl.TargetGroups[tg]
				if exists {
					continue
				}
				oldPlVersion := r.placements[dplID].Version

				pl.TargetGroups[tg] = struct{}{}
				// we don't want bump version too much
				pl.Version = oldPlVersion + 1
				desiredPlacements[dplID] = pl
				continue targetsLoop
			}
		}
	}
	return desiredPlacements
}

func (r *Reconciler) fixDataplanePlacements(ctx context.Context, desiredPlacements map[models.DataPlaneID]models.Placement) error {
	var (
		tgCount       = len(r.targetGroups)
		aliveDplCount = 0
		loads         = make([]dplLoad, 0, len(desiredPlacements))
	)

	for id, placement := range desiredPlacements {
		if r.dplStatuses[id].State != models.Alive {
			continue
		}
		aliveDplCount++
		loads = append(loads, dplLoad{
			id:   id,
			load: len(placement.TargetGroups),
		})
	}
	if aliveDplCount == 0 {
		log.Error().Msg("can't reconciliate target groups: all data-planes dead")
		return nil
	}

	var (
		tgWithRFCount        = float64(tgCount * r.targetGroupsReplicationFactor)
		dplShouldHaveAtLeast = int(tgWithRFCount / float64(aliveDplCount))
	)
	slices.SortFunc(loads, func(first, second dplLoad) int {
		if first.load-second.load != 0 {
			return first.load - second.load
		}
		return cmp.Compare(first.id, second.id)
	})

	var (
		leftPtr  = 0
		rightPtr = len(loads) - 1
	)
	for leftPtr < rightPtr {
		var (
			left  = &loads[leftPtr]
			right = &loads[rightPtr]
		)
		newLeft, newRight, ok := r.generateTgMovement(
			desiredPlacements,
			dplShouldHaveAtLeast,
			&leftPtr,
			&rightPtr,
			left.id,
			right.id,
		)
		if !ok {
			continue
		}
		left.load = len(newLeft.TargetGroups)
		right.load = len(newRight.TargetGroups)

		desiredPlacements[left.id] = newLeft
		desiredPlacements[right.id] = newRight
	}
	return r.applyDesiredState(ctx, desiredPlacements)
}

func (r *Reconciler) generateTgMovement(
	desiredPlacements map[models.DataPlaneID]models.Placement,
	dplShouldHaveAtLeast int,
	leftPtr *int,
	rightPtr *int,
	to models.DataPlaneID,
	from models.DataPlaneID,
) (models.Placement, models.Placement, bool) {
	var (
		toBefore   = desiredPlacements[to]
		fromBefore = desiredPlacements[from]

		needMoveTo  = dplShouldHaveAtLeast - len(toBefore.TargetGroups)
		canMoveFrom = len(fromBefore.TargetGroups) - dplShouldHaveAtLeast
		move        = 0

		toPl = models.Placement{
			Version:      toBefore.Version + 1,
			TargetGroups: maps.Clone(toBefore.TargetGroups),
		}
		fromPl = models.Placement{
			Version:      fromBefore.Version + 1,
			TargetGroups: maps.Clone(fromBefore.TargetGroups),
		}
	)
	// TODO: we can calculate unique tgs from each pl
	// and calc can-move in more accurate way
	switch {
	case needMoveTo > canMoveFrom:
		move = canMoveFrom
		*rightPtr--
	case canMoveFrom > needMoveTo:
		move = needMoveTo
		*leftPtr++
	case needMoveTo == canMoveFrom:
		*rightPtr--
		*leftPtr++
		move = needMoveTo
	}
	if move == 0 {
		return toPl, fromPl, false
	}
	for tgID := range fromBefore.TargetGroups {
		if move == 0 {
			break
		}
		if _, exists := toPl.TargetGroups[tgID]; exists {
			continue
		}
		move--
		toPl.TargetGroups[tgID] = struct{}{}
		delete(fromPl.TargetGroups, tgID)
	}
	return toPl, fromPl, true
}

func (r *Reconciler) applyDesiredState(ctx context.Context, desiredPlacements map[models.DataPlaneID]models.Placement) error {
	var (
		newPlacements = make([]models.Placement, 0, 16)
		dataPlaneIDs  = make([]models.DataPlaneID, 0, 16)
	)
	for id, desiredPl := range desiredPlacements {
		oldPl, exists := r.placements[id]
		if !exists || oldPl.Version != desiredPl.Version {
			newPlacements = append(newPlacements, desiredPl)
			dataPlaneIDs = append(dataPlaneIDs, id)
		}
	}
	if len(newPlacements) == 0 {
		log.Info().Msg("reconciliation: no changes")
		return nil
	}

	err := r.coordinator.UpdatePlacements(ctx, dataPlaneIDs, newPlacements)
	if err != nil {
		return err
	}
	r.applyLocal(dataPlaneIDs, newPlacements)
	log.Warn().Msgf("reconciliation: set desired state: %+v", desiredPlacements)
	return nil
}

func (r *Reconciler) applyLocal(dplIDs []models.DataPlaneID, newPls []models.Placement) {
	for i, dplID := range dplIDs {
		oldPl := r.placements[dplID]
		for tgID := range oldPl.TargetGroups {
			if r.targetGroups[tgID].Assignments == nil {
				continue
			}
			delete(r.targetGroups[tgID].Assignments, dplID)
		}
		r.placements[dplID] = newPls[i]
		for tgID := range newPls[i].TargetGroups {
			r.targetGroups[tgID].Assignments[dplID] = struct{}{}
		}
	}
}
