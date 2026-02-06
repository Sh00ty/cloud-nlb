package apiruntime

import (
	"context"
	"slices"
	"sync"

	"github.com/Sh00ty/network-lb/control-plane/internal/models"
	"github.com/rs/zerolog/log"
)

type DataPlaneRequest struct {
	NodeID             models.DataPlaneID
	Notifier           Notifier
	Placement          models.Placement
	TargetGroupsStatus map[models.TargetGroupID]models.TargetGroupPlacement
}

type TargetGroupChange struct {
	ID          models.TargetGroupID
	SpecVersion uint64
	Spec        *models.TargetGroupSpec

	EndpointVersion  uint64
	Snapshot         []models.EndpointSpec
	AddedEndpoints   []models.EndpointSpec
	RemovedEndpoints []models.EndpointSpec
}

type DataPlaneResponse struct {
	PlacementVersion uint64
	New              []TargetGroupChange
	Removed          []models.TargetGroupID
	Update           []TargetGroupChange
	NeedWait         bool
}

func (ar *ApiRuntime) HandleDataPlaneRequest(
	ctx context.Context,
	req DataPlaneRequest,
) (DataPlaneResponse, error) {
	placementDiff, needWait := ar.getDataplanePlacement(ctx, req)
	if needWait {
		return DataPlaneResponse{
			NeedWait: true,
		}, nil
	}
	changes, needWait := ar.getTargetGroupChanges(ctx, req, placementDiff)
	if needWait {
		return DataPlaneResponse{
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
	req DataPlaneRequest,
) (placementDiff, bool) {
	ar.dplGuard.RLock()
	dpl, exists := ar.dataPlaneCache[req.NodeID]
	ar.dplGuard.RUnlock()

	if !exists {
		addedDpl := ar.processNewNode(ctx, req)
		if addedDpl == nil {
			return placementDiff{placement: req.Placement.Version}, true
		}
		dpl = addedDpl
	}

	dpl.mu.Lock()
	defer dpl.mu.Unlock()

	if dpl.dplInfo.Desired == nil ||
		dpl.dplInfo.Desired.Version < req.Placement.Version {

		dpl.subscriptions[req.Notifier.ID()] = req.Notifier
		return placementDiff{placement: req.Placement.Version}, true
	}
	return ar.getPlacementDiff(dpl.dplInfo.Desired, &req.Placement), false
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
	req DataPlaneRequest,
) *dataPlaneCacheEntry {
	ar.dplGuard.Lock()
	defer ar.dplGuard.Unlock()

	dpl, exists := ar.dataPlaneCache[req.NodeID]
	if exists {
		return dpl
	}
	dpl = &dataPlaneCacheEntry{
		subscriptions: map[uint64]Notifier{req.Notifier.ID(): req.Notifier},
		mu:            sync.Mutex{},
		dplInfo: models.DataPlanePlacementInfo{
			NodeID:  req.NodeID,
			Desired: nil,
		},
	}
	ar.dataPlaneCache[req.NodeID] = dpl

	go func() {
		ar.fetchDataplaneInfo(ctx, req.NodeID)
	}()
	return nil
}

func (ar *ApiRuntime) getTargetGroupChanges(
	ctx context.Context,
	req DataPlaneRequest,
	placementDiff placementDiff,
) (DataPlaneResponse, bool) {
	var (
		needWait  = len(placementDiff.added) == 0 && len(placementDiff.deleted) == 0
		response  = DataPlaneResponse{}
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
				subscriptions: map[uint64]Notifier{req.Notifier.ID(): req.Notifier},
				tg:            models.TargetGroup{},
			}
		}
		ar.tgGuard.Unlock()

		go ar.fetchTargetGroupInfo(ctx, needFetch)
	}()

	for tgID := range placementDiff.deleted {
		response.Removed = append(response.Removed, tgID)
	}
	for _, tgID := range placementDiff.added {
		req.TargetGroupsStatus[tgID] = models.TargetGroupPlacement{TgID: tgID}
	}
	for tgID, tgStatus := range req.TargetGroupsStatus {
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
			tgCacheEntry.subscriptions[req.Notifier.ID()] = req.Notifier
		} else if ok {
			needWait = false
			response.Update = append(response.Update, change)
		}
		tgCacheEntry.mu.Unlock()
	}
	return response, needWait
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

		if tgCacheEntry.tg.ChangelogStartVersion < tgStatus.EndpointVersion {
			for _, ep := range tgCacheEntry.tg.EndpointsSnapshot {
				change.Snapshot = append(change.Snapshot, ep)
			}
		}
		changelogStart := max(tgStatus.EndpointVersion, tgCacheEntry.tg.ChangelogStartVersion)
		start, _ := slices.BinarySearchFunc(
			tgCacheEntry.tg.EndpointsChangelog,
			changelogStart,
			func(a models.EndpointEvent, t uint64) int {
				return int(a.DesiredVersion - t)
			},
		)
		for i := start; i < len(tgCacheEntry.tg.EndpointsChangelog); i++ {
			entry := tgCacheEntry.tg.EndpointsChangelog[i]
			switch entry.Type {
			case models.EventTypeAddEndpoint:
				change.AddedEndpoints = append(change.AddedEndpoints, entry.Spec)
			case models.EventTypeRemoveEndpoint:
				change.RemovedEndpoints = append(change.RemovedEndpoints, entry.Spec)
			}
		}
	}
	return change, updated
}

func (ar *ApiRuntime) fetchTargetGroupInfo(ctx context.Context, tgIDs []models.TargetGroupID) {
	ctx = context.WithoutCancel(ctx)

	log.Warn().Msgf("need refetch target group cache: %+v", tgIDs)
}

func (ar *ApiRuntime) fetchDataplaneInfo(ctx context.Context, nodeID models.DataPlaneID) {
	ctx = context.WithoutCancel(ctx)

	log.Warn().Msgf("need refetch data-plane node %s cache", nodeID)
}
