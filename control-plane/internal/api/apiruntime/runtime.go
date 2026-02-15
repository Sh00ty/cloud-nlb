package apiruntime

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/semaphore"
)

type dataPlaneCacheEntry struct {
	dplInfo       models.DataPlanePlacementInfo
	subscriptions map[uint64]Notifier
	mu            sync.Mutex
}

func (e *dataPlaneCacheEntry) notifyAll(ctx context.Context) {
	for _, sub := range e.subscriptions {
		sub.Notify(ctx)
		delete(e.subscriptions, sub.ID())
	}
}

type targetGroupCacheEntry struct {
	tg            models.TargetGroup
	subscriptions map[uint64]Notifier
	mu            sync.Mutex
}

func (e *targetGroupCacheEntry) appendIntoChangelog(ctx context.Context, ev models.EndpointEvent) {
	e.tg.EndpointsChangelog = append(e.tg.EndpointsChangelog, ev)
	e.tg.EndpointVersion = ev.DesiredVersion
	e.notifyAll(ctx)
}

func (e *targetGroupCacheEntry) notifyAll(ctx context.Context) {
	for _, sub := range e.subscriptions {
		sub.Notify(ctx)
		delete(e.subscriptions, sub.ID())
	}
}

func (e *targetGroupCacheEntry) updateTargetGroup(ctx context.Context, newTg models.TargetGroup) {
	e.mu.Lock()
	updated := false
	if e.tg.SpecVersion < newTg.SpecVersion {
		e.tg.Spec = newTg.Spec
		e.tg.SpecVersion = newTg.SpecVersion
		updated = true
	}
	updateChangelog := e.tg.EndpointVersion < newTg.EndpointVersion
	if e.tg.SnapshotLastVersion < newTg.SnapshotLastVersion {
		e.tg.EndpointsSnapshot = newTg.EndpointsSnapshot
		updateChangelog = true
	}
	if updateChangelog {
		e.tg.EndpointVersion = newTg.EndpointVersion
		e.tg.EndpointsChangelog = newTg.EndpointsChangelog
	}
	if updated || updateChangelog {
		e.notifyAll(ctx)
	}
	e.mu.Unlock()
}

type ApiRuntime struct {
	targetGroupCache map[models.TargetGroupID]*targetGroupCacheEntry
	tgGuard          *sync.RWMutex

	dataPlaneCache map[models.DataPlaneID]*dataPlaneCacheEntry
	dplGuard       *sync.RWMutex

	notifierIdSaltGen atomic.Uint64
	gcTicker          *time.Ticker
	crd               Coordinator
	coordinatorSema   *semaphore.Weighted
}

func NewApiRuntime(
	crd Coordinator,
	gcInterval time.Duration,
	crdSemaCapacity int64,
) *ApiRuntime {
	ticker := time.NewTicker(gcInterval)
	ar := &ApiRuntime{
		targetGroupCache: make(map[models.TargetGroupID]*targetGroupCacheEntry, 128),
		tgGuard:          &sync.RWMutex{},

		dataPlaneCache: make(map[models.DataPlaneID]*dataPlaneCacheEntry, 128),
		dplGuard:       &sync.RWMutex{},

		notifierIdSaltGen: atomic.Uint64{},
		gcTicker:          ticker,
		crd:               crd,
		coordinatorSema:   semaphore.NewWeighted(crdSemaCapacity),
	}
	return ar
}

func (ar *ApiRuntime) Init(placements map[models.DataPlaneID]models.Placement) {
	for dplID, placement := range placements {
		pl := placement
		ar.dataPlaneCache[dplID] = &dataPlaneCacheEntry{
			subscriptions: make(map[uint64]Notifier),
			dplInfo: models.DataPlanePlacementInfo{
				NodeID:  dplID,
				Desired: &pl,
			},
		}
	}
}

func (ar *ApiRuntime) Close() error {
	ar.gcTicker.Stop()
	ar.collectGarbage()
	return nil
}

func (ar *ApiRuntime) RunGarbageCollection(ctx context.Context) {
	// TODO: make self removing after successful request
	// second iteration must be with clean flag
	// or we can store all entries in notifier and later make self cleaning
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ar.gcTicker.C:
			if !ok {
				return
			}
			deleted := ar.collectGarbage()
			log.Info().Msgf("end notifiers garbage collection cycle, deleted %d subs", deleted)
		}
	}
}

func (ar *ApiRuntime) collectGarbage() int {
	ar.tgGuard.RLock()
	defer ar.tgGuard.RUnlock()

	deleted := 0
	for _, tgCacheEntry := range ar.targetGroupCache {
		tgCacheEntry.mu.Lock()
		for _, sub := range tgCacheEntry.subscriptions {
			if sub.IsExpired() {
				deleted++
				delete(tgCacheEntry.subscriptions, sub.ID())
			}
		}
		tgCacheEntry.mu.Unlock()
	}
	return deleted
}

func (ar *ApiRuntime) getAllTargetGroupIDs(ctx context.Context) []models.TargetGroupID {
	// TODO:
	return nil
}

// assumes that target group cache entry already exists
func (ar *ApiRuntime) updateTargetGroup(ctx context.Context, tgID models.TargetGroupID) error {
	ar.tgGuard.RLock()
	cacheEntry, _ := ar.targetGroupCache[tgID]
	ar.tgGuard.RUnlock()

	state := TargetGroupState{
		TgID: tgID,
	}

	cacheEntry.mu.Lock()
	state.SpecVersion = cacheEntry.tg.SpecVersion
	state.EndpointVersion = cacheEntry.tg.EndpointVersion
	state.SnapshotLastVersion = cacheEntry.tg.SnapshotLastVersion
	cacheEntry.mu.Unlock()

	newTgInfo, err := ar.crd.GetTargetGroupDiff(ctx, state)
	if err != nil {
		return fmt.Errorf("failed to get target group from coordinator: %w", err)
	}
	cacheEntry.updateTargetGroup(ctx, newTgInfo)
	return nil
}

func (ar *ApiRuntime) HandleEndpointChange(
	ctx context.Context,
	ev models.EndpointEvent,
	rev uint64,
) {
	ar.tgGuard.RLock()
	defer ar.tgGuard.RUnlock()

	tgCacheEntry, exists := ar.targetGroupCache[ev.TargetGroupID]
	if !exists {
		log.Error().Msgf("try to add endpoint to unknown target group: %s", ev.TargetGroupID)
		return
	}
	tgCacheEntry.mu.Lock()
	defer tgCacheEntry.mu.Unlock()

	if tgCacheEntry.tg.SnapshotLastVersion >= ev.DesiredVersion {
		log.Info().Msgf("endpoint event with timestamp %d already in snapshot", ev.DesiredVersion)
		return
	}

	if len(tgCacheEntry.tg.EndpointsChangelog) == 0 {
		tgCacheEntry.appendIntoChangelog(ctx, ev)
		return
	}
	last := tgCacheEntry.tg.EndpointsChangelog[len(tgCacheEntry.tg.EndpointsChangelog)-1]
	if last.DesiredVersion >= ev.DesiredVersion {
		log.Info().Msgf("endpoint event with timestamp %d already in changelog", ev.DesiredVersion)
		return
	}
	if last.DesiredVersion+1 != ev.DesiredVersion {
		log.Warn().Msgf("income version is too big; must drop all tg %s cache", ev.TargetGroupID)
		// TODO:
		return
	}
	tgCacheEntry.appendIntoChangelog(ctx, ev)
}

func (ar *ApiRuntime) HandleTgSpecChange(
	ctx context.Context,
	spec models.TargetGroupSpec,
	desiredVersion uint64,
	rev uint64,
) {

	ar.tgGuard.Lock()
	defer ar.tgGuard.Unlock()

	tgCacheEntry, exists := ar.targetGroupCache[spec.ID]
	if !exists {
		ar.targetGroupCache[spec.ID] = &targetGroupCacheEntry{
			tg: models.TargetGroup{
				Spec:        spec,
				SpecVersion: desiredVersion,
			},
			subscriptions: make(map[uint64]Notifier, 128),
		}
		log.Info().Msgf(
			"added new target group into cache with desired version: %d",
			desiredVersion,
		)
		return
	}
	tgCacheEntry.mu.Lock()
	defer tgCacheEntry.mu.Unlock()

	if tgCacheEntry.tg.SpecVersion >= desiredVersion {
		log.Info().Msgf("not updated tg %s spec: has local version more then %d", spec.ID, desiredVersion)
		return
	}
	tgCacheEntry.tg.Spec = spec
	tgCacheEntry.tg.SpecVersion = desiredVersion
	tgCacheEntry.notifyAll(ctx)

	log.Info().Msgf("updated tg %s spec, set desired version %d", spec.ID, desiredVersion)
}

func (ar *ApiRuntime) HandlePlacementChange(
	ctx context.Context,
	dplID models.DataPlaneID,
	pl models.Placement,
	rev uint64,
) {
	ar.dplGuard.Lock()
	defer ar.dplGuard.Unlock()

	dplEntry, exists := ar.dataPlaneCache[dplID]
	if !exists {
		ar.dataPlaneCache[dplID] = &dataPlaneCacheEntry{
			dplInfo: models.DataPlanePlacementInfo{
				NodeID:  dplID,
				Desired: &pl,
			},
			subscriptions: make(map[uint64]Notifier),
		}
		log.Info().Msgf("added placement for node %s: %+v", dplID, pl)
		return
	}
	dplEntry.mu.Lock()
	defer dplEntry.mu.Unlock()

	if dplEntry.dplInfo.Desired != nil && dplEntry.dplInfo.Desired.Version >= pl.Version {
		log.Warn().Msgf(
			"not updated node %s placement: cache version is more then incoming (%d) >= (%d)",
			dplID, dplEntry.dplInfo.Desired.Version, pl.Version,
		)
	}
	dplEntry.dplInfo.Desired = &pl
	dplEntry.notifyAll(ctx)

	log.Info().Msgf("updated placement for node %s: %+v", dplID, pl)
}
