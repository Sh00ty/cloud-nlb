package reconciller

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/Sh00ty/network-lb/control-plane/internal/models"
)

type TargetGroupSnapshot struct {
	TargetGroupSpec *models.TargetGroupSpec
	Version         uint64
}

type DataPlaneSnapshot struct {
}

type CoordinatorService interface {
	SetTargetGroupSpec(ctx context.Context, tgSpec *models.TargetGroupSpec, desiredVersion uint) (bool, error)
	GetAllPendingOperations(ctx context.Context) ([]*models.Event, error)
	GetAllTargetGroupsDesired(ctx context.Context) (map[models.TargetGroupID]*models.TargetGroup, error)
	SkipEvent(ctx context.Context, tgID models.TargetGroupID, desiredVersion uint64) (bool, error)
}

type Reconciller struct {
	targetGroupsDesired      map[models.TargetGroupID]*models.TargetGroup
	targetGroupPendingEvents map[models.TargetGroupID][]*models.Event
	eventChan                chan *models.Event
	close                    chan struct{}
	// here we will store all event, that we can't apply, may be reorder, may be
	// something else
	// pendingEventNotiyer chan models.TargetGroupID
	coordinator CoordinatorService
}

func NewReconciller(coordinator CoordinatorService, eventChan chan *models.Event) *Reconciller {
	return &Reconciller{
		targetGroupsDesired:      make(map[models.TargetGroupID]*models.TargetGroup, 128),
		targetGroupPendingEvents: make(map[models.TargetGroupID][]*models.Event, 128),
		eventChan:                eventChan,
		close:                    make(chan struct{}),
		coordinator:              coordinator,
	}
}

func (r *Reconciller) RunEventWatcher(ctx context.Context) error {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-r.eventChan:
				// TODO: retry + failed array
				err := r.handleEventlogEvent(ctx, event)
				if err != nil {
					log.Error().Err(err).Msgf("failed to handle event from eventlog: %+v", *event)
				}
			case <-r.close:
				return
			}
		}
	}()
	desiredTargetGroup, err := r.coordinator.GetAllTargetGroupsDesired(ctx)
	if err != nil {
		return fmt.Errorf("failed to get target groups desired state: %w", err)
	}
	events, err := r.coordinator.GetAllPendingOperations(ctx)
	if err != nil {
		return fmt.Errorf("failed to get pending events: %w", err)
	}
	r.targetGroupsDesired = desiredTargetGroup
	for _, event := range events {
		r.eventChan <- event
	}
	return nil
}

func (r *Reconciller) handleEventlogEvent(ctx context.Context, event *models.Event) error {
	if event.Deadline.Before(time.Now()) {
		r.handleOutdatedEvent(ctx, event)
		return nil
	}
	if event.Status == models.EventStatusApplied {
		log.Warn().Msg("reconciller: got applied event: skip")
		return nil
	}
	switch event.Type {
	case models.EventTypeUpdateTargetGroup:
		err := r.updateTargetGroup(ctx, event.TargetGroupSpec, event.DesiredVersion)
		if err != nil {
			return fmt.Errorf("failed to update target group version via eventlog event: %w", err)
		}
		return nil
	default:
		log.Info().Msgf("skip event with type %s", event.Type)
	}
	return nil
}

func (r *Reconciller) handleOutdatedEvent(ctx context.Context, event *models.Event) {
	log.Warn().Msgf("got outdated event, skip it: %+v", event)
	ok, err := r.coordinator.SkipEvent(ctx, event.TargetGroupSpec.ID, uint64(event.DesiredVersion))
	if err != nil || !ok {
		log.Error().Err(err).Bool("ok", ok).Msg("failed to skip outdated event")
		pendingEvents := r.targetGroupPendingEvents[event.TargetGroupSpec.ID]
		pendingEvents = append(pendingEvents, event)
		r.targetGroupPendingEvents[event.TargetGroupSpec.ID] = pendingEvents
	}
}

func (r *Reconciller) updateTargetGroup(
	ctx context.Context,
	tgSpec *models.TargetGroupSpec,
	desiredVersion uint,
) error {
	desiredTg, exists := r.targetGroupsDesired[tgSpec.ID]
	if !exists {
		desiredTg = &models.TargetGroup{
			Version: 0,
		}
	}
	if desiredTg.Version+1 < uint64(desiredVersion) {
		// TODO: refetch pending events and desiredStates
		log.Warn().Msgf(
			"target group %s has local desired version too low got %d want set %d: store in pending queue",
			tgSpec.ID, desiredTg.Version, desiredVersion,
		)
		return nil
	}
	// add to etcd and only then add into local cache
	// TODO:assign data-planes
	set, err := r.coordinator.SetTargetGroupSpec(ctx, tgSpec, desiredVersion)
	if err != nil {
		return err
	}
	if !set {
		// TODO:
		// видимо это что-то уже старое
		log.Error().Msg("failed to set target group spec version: outated or concurrent access")
		return nil
	}
	r.targetGroupsDesired[tgSpec.ID] = &models.TargetGroup{
		Spec:    tgSpec,
		Version: uint64(desiredVersion),
	}
	log.Info().Msg("sucessfully updated tg spec")
	return nil
}
