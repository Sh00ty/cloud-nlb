package reconciler

import (
	"context"
	"fmt"
	"time"

	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/models"
	"github.com/cespare/xxhash/v2"
	"github.com/rs/zerolog"
)

type Reconciler struct {
	log                    zerolog.Logger
	stor                   Storage
	endpointsStatusManager EndpointsStatusManager

	reconcileTaskChan    chan []models.TargetGroupID
	forceReconcileTicker *time.Ticker

	reconcileWorkers []*worker
}

func New(
	reconcileTaskChan chan []models.TargetGroupID,
	stateStor Storage,
	endpointsStatusManager EndpointsStatusManager,
	endpointStatusCache EndpointStatusCache,
	maxReconcileAttempts uint8,
	forceReconcileInterval time.Duration,
	concurrency uint8,
	vpp VPPManager,
	log *zerolog.Logger,
) *Reconciler {
	workers := make([]*worker, 0, concurrency)
	for i := range concurrency {
		workers = append(workers, &worker{
			id:                     int(i),
			maxAttempts:            maxReconcileAttempts,
			stor:                   stateStor,
			endpointsStatusManager: endpointsStatusManager,
			endpointStatusCache:    endpointStatusCache,
			vpp:                    vpp,
			pending:                make(map[models.TargetGroupID]struct{}),
			queue:                  make(chan reconcileTask, 1),
			log: log.With().
				Str("component", "reconcile_wrk").
				Uint8("worker_id", i).
				Logger(),
		})
	}

	return &Reconciler{
		stor:                   stateStor,
		endpointsStatusManager: endpointsStatusManager,

		reconcileTaskChan:    reconcileTaskChan,
		forceReconcileTicker: time.NewTicker(forceReconcileInterval),
		reconcileWorkers:     workers,

		log: log.With().Str("component", "reconciler").Logger(),
	}
}

func (r *Reconciler) Run(ctx context.Context) {
	for _, wrk := range r.reconcileWorkers {
		go wrk.run(ctx)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				r.log.Warn().Msg("reconciler stopped")
				return
			case <-r.forceReconcileTicker.C:
				tgIDs := r.stor.GetAllTargetGroupIDs()
				targetGroupsTotal.Set(float64(len(tgIDs)))
				r.enqueueReconcileTask(tgIDs)
			case task := <-r.reconcileTaskChan:
				r.enqueueReconcileTask(task)
			}
		}
	}()
}

func (r *Reconciler) enqueueReconcileTask(task []models.TargetGroupID) {
	for _, tgID := range task {
		var (
			tgHash = xxhash.Sum64([]byte(tgID))
			wrkID  = tgHash % uint64(len(r.reconcileWorkers))
		)

		r.reconcileWorkers[wrkID].enqueue(reconcileTask{tgID: tgID, attempt: 1})
	}
}

func (r *Reconciler) UpdateDesired(ctx context.Context, recUnit *models.ReconciliationUnit) error {
	if recUnit == nil {
		r.log.Info().Msg("reconciler got empty reconciliation unit")
		return nil
	}
	err := r.updateTargetGroups(ctx, recUnit.Updated)
	if err != nil {
		r.log.Error().Err(err).Msg("updating existing target groups specs")
	}
	desiredChangesTotal.WithLabelValues("updated").Add(float64(len(recUnit.Updated)))

	err = r.addNewTargetGroups(ctx, recUnit.Added)
	if err != nil {
		return fmt.Errorf("adding new target groups: %w", err)
	}
	desiredChangesTotal.WithLabelValues("added").Add(float64(len(recUnit.Updated)))

	// may be removed have to be before add to make replaces correctly
	err = r.removeTargetGroups(ctx, recUnit.Removed)
	if err != nil {
		return fmt.Errorf("removing target groups: %w", err)
	}
	desiredChangesTotal.WithLabelValues("removed").Add(float64(len(recUnit.Updated)))

	updated, err := r.stor.SavePlacementVersion(ctx, recUnit.PlacementVersion)
	if err != nil {
		return fmt.Errorf("saving placement version: %d", recUnit.PlacementVersion)
	}
	if updated {
		placementVersionUpdatesTotal.Inc()
		r.log.Info().Uint64("placement_version", recUnit.PlacementVersion).Msg("updated placement version")
	}
	placementVersion.Set(float64(recUnit.PlacementVersion))
	return r.makeReconcileEvent(ctx, recUnit)
}

func (r *Reconciler) makeReconcileEvent(ctx context.Context, recUnit *models.ReconciliationUnit) error {
	needReconcileTargetGroupIDs := make(
		[]models.TargetGroupID,
		0,
		len(recUnit.Added)+len(recUnit.Removed)+len(recUnit.Updated),
	)
	for _, added := range recUnit.Added {
		needReconcileTargetGroupIDs = append(needReconcileTargetGroupIDs, added.ID)
	}
	for _, updated := range recUnit.Updated {
		needReconcileTargetGroupIDs = append(needReconcileTargetGroupIDs, updated.ID)
	}
	needReconcileTargetGroupIDs = append(needReconcileTargetGroupIDs, recUnit.Removed...)
	select {
	case <-ctx.Done():
		return fmt.Errorf("sending target groups to reconciliation event channel")
	case r.reconcileTaskChan <- needReconcileTargetGroupIDs:
		return nil
	}
}

func (r *Reconciler) addNewTargetGroups(ctx context.Context, added []*models.TargetGroupChange) (err error) {
	var (
		log                    = r.log.With().Str("action", "add new target groups").Logger()
		insertedSpecs          = 0
		insertedEndpointStates = 0
	)
	for _, tgChange := range added {
		inLoopLog := log.With().Str("target_group_id", string(tgChange.ID)).Logger()

		if tgChange.Spec == nil {
			return fmt.Errorf("spec can't be nil for new target group %s", tgChange.ID)
		}
		_, err = r.stor.SetDesiredSpec(ctx, tgChange.ID, *tgChange.Spec, tgChange.SpecVersion)
		if err != nil {
			return fmt.Errorf("setting desired spec for tg %s: %w", tgChange.ID, err)
		}
		insertedSpecs++

		err = r.endpointsStatusManager.WatchForTargetGroup(ctx, tgChange.ID)
		if err != nil {
			return fmt.Errorf("adding target group into endpoint status watcher: %w", err)
		}

		endpoints := constructEndpoints(nil, nil, tgChange.Changelog)
		_, err = r.stor.SetDesiredEndpoints(ctx, tgChange.ID, endpoints, tgChange.EndpointsVersion)
		if err != nil {
			inLoopLog.Error().Err(err).Msg("setting endpoint state for new tg")
		}
		insertedEndpointStates++
	}
	log.Info().
		Int("income_len", len(added)).
		Int("inserted_specs", insertedSpecs).
		Int("inserted_endpoint_states", insertedEndpointStates).
		Msg("inserted new target groups")

	return nil
}

func (r *Reconciler) removeTargetGroups(ctx context.Context, ids []models.TargetGroupID) error {
	for _, tgID := range ids {
		err := r.endpointsStatusManager.StopWatchForTargetGroup(ctx, tgID)
		if err != nil {
			r.log.Error().
				Str("action", "remove target groups").
				Str("tg_id", string(tgID)).
				Err(err).
				Msg("stopping watch for target group")
		}
	}

	err := r.stor.DeleteDesired(ctx, ids)
	if err != nil {
		return fmt.Errorf("deleting desired tg state: %w", err)
	}

	r.log.Info().
		Str("action", "remove target groups").
		Int("removed", len(ids)).
		Msg("removed target group desired states")
	return nil
}

func (r *Reconciler) updateTargetGroups(ctx context.Context, updated []*models.TargetGroupChange) error {
	var (
		logger           = r.log.With().Str("action", "update target groups")
		updatedTgSpecs   = 0
		updatedEndpoints = 0
	)
	for _, tg := range updated {
		inLoopLog := logger.Str("target_group_id", string(tg.ID)).Logger()

		if tg.Spec != nil {
			ok, err := r.stor.SetDesiredSpec(ctx, tg.ID, *tg.Spec, tg.SpecVersion)
			if err != nil {
				inLoopLog.Error().Err(err).Msg("setting target group desired spec version")
			}
			if ok {
				updatedTgSpecs++
				inLoopLog.Debug().Msgf("updated desired spec to version: %d", tg.SpecVersion)
			}
		}

		if len(tg.Changelog) != 0 {
			var eps []models.EndpointSpec
			endpoints, found := r.stor.GetDesiredEndpoints(ctx, tg.ID)
			if found {
				eps = constructEndpoints(endpoints.Endpoints, nil, tg.Changelog)
			} else {
				eps = constructEndpoints(nil, nil, tg.Changelog)
			}
			ok, err := r.stor.SetDesiredEndpoints(ctx, tg.ID, eps, tg.EndpointsVersion)
			if err != nil {
				inLoopLog.Error().Err(err).Msg("setting endpoints desired state")
			}
			if ok {
				updatedEndpoints++
				inLoopLog.Debug().Msgf("updated desired endpoints state to version: %d", tg.EndpointsVersion)
			}
		}
	}
	log := logger.Logger()
	log.Info().
		Int("income_update_len", len(updated)).
		Int("updated_specs", updatedTgSpecs).
		Int("updated_endpoints", updatedEndpoints).
		Msg("updated target groups desired specs")
	return nil
}

func (r *Reconciler) ScheduleReconcileTargetGroup(ctx context.Context, tgID models.TargetGroupID) {
	r.enqueueReconcileTask([]models.TargetGroupID{tgID})
}

func constructEndpoints(stored []models.EndpointSpec, newSnapshot []models.EndpointSpec, newChangelog []models.EndpointEvent) []models.EndpointSpec {
	type key struct {
		ip   string
		port uint16
	}
	resultSet := make(map[key]models.EndpointSpec, len(stored)+len(newSnapshot)+len(newChangelog))

	if len(newSnapshot) == 0 {
		for _, storedEp := range stored {
			k := key{ip: storedEp.IP.String(), port: storedEp.Port}
			resultSet[k] = storedEp
		}
	}
	for _, snapEp := range newSnapshot {
		k := key{ip: snapEp.IP.String(), port: snapEp.Port}
		resultSet[k] = snapEp
	}

	for _, epEvent := range newChangelog {
		k := key{ip: epEvent.Spec.IP.String(), port: epEvent.Spec.Port}
		if epEvent.Removed {
			delete(resultSet, k)
			continue
		}
		resultSet[k] = epEvent.Spec
	}
	result := make([]models.EndpointSpec, 0, len(resultSet))
	for _, spec := range resultSet {
		result = append(result, spec)
	}
	return result
}
