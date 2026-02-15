package apiruntime

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
	"github.com/rs/zerolog/log"
)

type DataPlaneCurrentState struct {
	NodeID             models.DataPlaneID
	Notifier           Notifier
	Placement          models.Placement
	TargetGroupsStatus map[models.TargetGroupID]models.TargetGroupPlacement
}

type DataPlaneChanges struct {
	PlacementVersion uint64
	New              []TargetGroupChange
	Removed          []models.TargetGroupID
	Update           []TargetGroupChange
	NeedWait         bool
}

func (ar *ApiRuntime) GetChangesForDataPlane(
	ctx context.Context,
	state DataPlaneCurrentState,
) (DataPlaneChanges, error) {
	placementDiff, needWait := ar.getDataplanePlacement(ctx, state)
	if needWait {
		return DataPlaneChanges{
			NeedWait: true,
		}, nil
	}
	changes, needWait := ar.getTargetGroupChanges(ctx, state, placementDiff)
	if needWait {
		return DataPlaneChanges{
			NeedWait: true,
		}, nil
	}
	changes.PlacementVersion = placementDiff.placement
	return changes, nil
}

type placementDiff struct {
	placement uint64
	added     []models.TargetGroupID
	deleted   map[models.TargetGroupID]struct{}
}

func (ar *ApiRuntime) getDataplanePlacement(
	ctx context.Context,
	dplCurState DataPlaneCurrentState,
) (placementDiff, bool) {
	ar.dplGuard.RLock()
	dpl, exists := ar.dataPlaneCache[dplCurState.NodeID]
	ar.dplGuard.RUnlock()

	if !exists {
		addedDpl := ar.processNewNode(ctx, dplCurState)
		if addedDpl == nil {
			return placementDiff{placement: dplCurState.Placement.Version}, true
		}
		dpl = addedDpl
	}

	dpl.mu.Lock()
	defer dpl.mu.Unlock()

	if dpl.dplInfo.Desired == nil ||
		dpl.dplInfo.Desired.Version < dplCurState.Placement.Version {

		dpl.subscriptions[dplCurState.Notifier.ID()] = dplCurState.Notifier
		return placementDiff{placement: dplCurState.Placement.Version}, true
	}
	return ar.getPlacementDiff(dpl.dplInfo.Desired, &dplCurState.Placement), false
}

func (ar *ApiRuntime) getPlacementDiff(desired, income *models.Placement) placementDiff {
	pd := placementDiff{
		added:     make([]models.TargetGroupID, 0),
		deleted:   make(map[models.TargetGroupID]struct{}),
		placement: desired.Version,
	}

	if desired.Version == income.Version {
		return pd
	}
	for desiredTgID := range desired.TargetGroups {
		_, exists := income.TargetGroups[desiredTgID]
		if !exists {
			pd.added = append(pd.added, desiredTgID)
		}
	}
	for incomeTgID := range income.TargetGroups {
		_, exists := desired.TargetGroups[incomeTgID]
		if !exists {
			pd.deleted[incomeTgID] = struct{}{}
		}
	}
	return pd
}

func (ar *ApiRuntime) processNewNode(
	ctx context.Context,
	curDplState DataPlaneCurrentState,
) *dataPlaneCacheEntry {
	ar.dplGuard.Lock()
	defer ar.dplGuard.Unlock()

	dpl, exists := ar.dataPlaneCache[curDplState.NodeID]
	if exists {
		return dpl
	}
	dpl = &dataPlaneCacheEntry{
		subscriptions: map[uint64]Notifier{curDplState.Notifier.ID(): curDplState.Notifier},
		mu:            sync.Mutex{},
		dplInfo: models.DataPlanePlacementInfo{
			NodeID:  curDplState.NodeID,
			Desired: nil,
		},
	}
	ar.dataPlaneCache[curDplState.NodeID] = dpl
	go ar.fetchDataplaneInfo(ctx, curDplState.NodeID)
	return nil
}

func (ar *ApiRuntime) getTargetGroupChanges(
	ctx context.Context,
	curDplState DataPlaneCurrentState,
	placementDiff placementDiff,
) (DataPlaneChanges, bool) {
	var (
		needWait  = len(placementDiff.added) == 0 && len(placementDiff.deleted) == 0
		changes   = DataPlaneChanges{}
		needFetch = []models.TargetGroupID{}
	)

	ar.tgGuard.RLock()
	defer func() {
		ar.tgGuard.RUnlock()

		if len(needFetch) == 0 {
			return
		}
		ar.tgGuard.Lock()
		for _, tgID := range needFetch {
			ar.targetGroupCache[tgID] = &targetGroupCacheEntry{
				subscriptions: map[uint64]Notifier{curDplState.Notifier.ID(): curDplState.Notifier},
				tg:            models.TargetGroup{},
			}
		}
		ar.tgGuard.Unlock()

		go ar.fetchTargetGroupInfo(ctx, needFetch)
	}()

	for tgID := range placementDiff.deleted {
		changes.Removed = append(changes.Removed, tgID)
	}
	// TODO: check correct
	for _, tgID := range placementDiff.added {
		tgCacheEntry, exists := ar.targetGroupCache[tgID]
		if !exists {
			needFetch = append(needFetch, tgID)
			changes.New = append(changes.New, TargetGroupChange{
				ID: tgID,
			})
			continue
		}
		tgCacheEntry.mu.Lock()
		change, _ := ar.getDiffForTg(
			models.TargetGroupPlacement{
				TgID: tgID,
			}, tgCacheEntry,
		)
		tgCacheEntry.mu.Unlock()
		changes.New = append(changes.New, change)
	}
	for tgID, tgStatus := range curDplState.TargetGroupsStatus {
		if _, skip := placementDiff.deleted[tgID]; skip {
			continue
		}
		tgCacheEntry, exists := ar.targetGroupCache[tgID]
		if !exists {
			needFetch = append(needFetch, tgID)
			continue
		}
		tgCacheEntry.mu.Lock()
		change, ok := ar.getDiffForTg(tgStatus, tgCacheEntry)
		if !ok && needWait {
			tgCacheEntry.subscriptions[curDplState.Notifier.ID()] = curDplState.Notifier
		}
		if ok {
			needWait = false
			changes.Update = append(changes.Update, change)
		}
		tgCacheEntry.mu.Unlock()
	}
	changes.NeedWait = needWait
	return changes, needWait
}

func (ar *ApiRuntime) getDiffForTg(
	tgStatus models.TargetGroupPlacement,
	tgCacheEntry *targetGroupCacheEntry,
) (TargetGroupChange, bool) {
	var (
		updated = false
		change  = TargetGroupChange{
			ID:              tgStatus.TgID,
			SpecVersion:     tgStatus.SpecVersion,
			EndpointVersion: tgStatus.EndpointVersion,
		}
	)
	if tgCacheEntry.tg.SpecVersion > tgStatus.SpecVersion {
		updated = true
		change.Spec = &tgCacheEntry.tg.Spec
		change.SpecVersion = tgCacheEntry.tg.SpecVersion
	}

	if tgCacheEntry.tg.EndpointVersion > tgStatus.EndpointVersion {
		updated = true
		change.EndpointVersion = tgCacheEntry.tg.EndpointVersion

		if tgCacheEntry.tg.SnapshotLastVersion > tgStatus.EndpointVersion {
			for _, ep := range tgCacheEntry.tg.EndpointsSnapshot {
				change.Snapshot = append(change.Snapshot, ep)
			}
		}
		start := 0
		if tgStatus.EndpointVersion > 0 {
			start, _ = slices.BinarySearchFunc(
				tgCacheEntry.tg.EndpointsChangelog,
				tgStatus.EndpointVersion,
				func(a models.EndpointEvent, t uint64) int {
					return int(a.DesiredVersion - t)
				},
			)
			start++
		}
		for i := start; i < len(tgCacheEntry.tg.EndpointsChangelog); i++ {
			entry := tgCacheEntry.tg.EndpointsChangelog[i]
			change.Changelog = append(change.Changelog, entry)
		}
	}
	return change, updated
}

func (ar *ApiRuntime) fetchTargetGroupInfo(ctx context.Context, tgIDs []models.TargetGroupID) {
	ctx = context.WithoutCancel(ctx)

	log.Warn().Msgf("need refetch target group cache: %+v", tgIDs)

	for _, tgID := range tgIDs {
		_ = ar.coordinatorSema.Acquire(ctx, 1)
		go func() {
			defer ar.coordinatorSema.Release(1)

			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			err := ar.updateTargetGroup(ctx, tgID)
			if err != nil {
				log.Error().Err(err).Msgf("failed to fetch tg %s", tgID)
				return
			}
			log.Info().Msgf("fetched tg %s data", tgID)
		}()
	}
}

func (ar *ApiRuntime) fetchDataplaneInfo(ctx context.Context, nodeID models.DataPlaneID) {
	ctx = context.WithoutCancel(ctx)

	log.Warn().Msgf("need refetch data-plane node %s cache", nodeID)
}
