package reconciler

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
}

type Reconciler struct {
	targetGroupsDesired      map[models.TargetGroupID]TargetGroupSnapshot
	targetGroupPendingEvents map[models.TargetGroupID][]*models.Event
	eventChan                chan *models.Event
	// here we will store all event, that we can't apply, may be reorder, may be
	// something else
	// pendingEventNotiyer chan models.TargetGroupID
	coordinator CoordinatorService
}

func NewReconciler(coordinator CoordinatorService, eventChan chan *models.Event) *Reconciler {
	return &Reconciler{
		targetGroupsDesired:      make(map[models.TargetGroupID]TargetGroupSnapshot, 128),
		targetGroupPendingEvents: make(map[models.TargetGroupID][]*models.Event, 128),
		eventChan:                eventChan,
		coordinator:              coordinator,
	}
}

func (r *Reconciler) RunEventWatcher(ctx context.Context) {
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
		}
	}
}

func (r *Reconciler) handleEventlogEvent(ctx context.Context, event *models.Event) error {
	if event.Deadline.Before(time.Now()) {
		r.handleOutdatedEvent(ctx, event)
		return nil
	}
	if event.Status == models.EventStatusApplied {
		log.Warn().Msg("reconciler: got applied event: skip")
		return nil
	}
	switch event.Type {
	case models.EventTypeCreateTargetGroup:
		err := r.addTargetGroup(ctx, event.TargetGroupSpec, event.DesiredVersion)
		if err != nil {
			return fmt.Errorf("failed to add target group via eventlog event: %w", err)
		}
		return nil
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

func (r *Reconciler) handleOutdatedEvent(ctx context.Context, event *models.Event) {
	// TODO:
}

func (r *Reconciler) addTargetGroup(
	ctx context.Context,
	tgSpec *models.TargetGroupSpec,
	desiredVersion uint,
) error {
	_, exists := r.targetGroupsDesired[tgSpec.ID]
	if exists {
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
	r.targetGroupsDesired[tgSpec.ID] = TargetGroupSnapshot{
		TargetGroupSpec: tgSpec,
		Version:         uint64(desiredVersion),
	}
	log.Info().Msg("sucessfully created tg spec")
	return nil
}

func (r *Reconciler) updateTargetGroup(
	ctx context.Context,
	tgSpec *models.TargetGroupSpec,
	desiredVersion uint,
) error {
	desiredTg, exists := r.targetGroupsDesired[tgSpec.ID]
	if !exists {
		// TODO:
		log.Warn().Msgf("target group %s doesn't exists: store event in pending queue", tgSpec.ID)
		return nil
	}
	if desiredTg.Version+1 < uint64(desiredVersion) {
		// TODO:
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
		log.Error().Msg("failed to set target group spec version: outdated or concurrent access")
		return nil
	}
	r.targetGroupsDesired[tgSpec.ID] = TargetGroupSnapshot{
		TargetGroupSpec: tgSpec,
		Version:         uint64(desiredVersion),
	}
	log.Info().Msg("successfully updated tg spec")
	return nil
}
