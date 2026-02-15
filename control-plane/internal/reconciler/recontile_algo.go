package reconciler

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"time"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
	"github.com/avast/retry-go/v4"
)

func (r *Reconciler) reconcile() {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err := retry.Do(
		func() error {
			return r.reconcileAttempt(ctx)
		},
		retry.Context(ctx),
		retry.Attempts(3),
		retry.DelayType(retry.BackOffDelay),
		retry.OnRetry(func(attempt uint, err error) {
			r.log.Warn().Err(err).Msgf("failed to complete reconciliation, attempt: %d", attempt)
		}),
	)
	if err != nil {
		r.log.Error().Err(err).Msg("failed to complete reconciliation event, will try later")
		r.delayEvent(Event{Type: RunReconcile}, 30*time.Second)
	}
}

type dplLoad struct {
	id   models.DataPlaneID
	load int
}

func (r *Reconciler) reconcileAttempt(ctx context.Context) error {
	r.log.Info().Msg("start reconciliation")

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
		needReplace       = make(map[models.TargetGroupID]int, 128)
	)

	r.log.Debug().Msgf("reconciliation: current data-planes states: %+v", r.dplStatuses)
	r.log.Debug().Msgf("reconciliation: current placements: %+v", r.placements)
	r.log.Debug().Msgf("reconciliation: current target groups: %+v", r.targetGroups)

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
		underReplicated := r.targetGroupsReplicationFactor - len(as.Assignments)
		needReplace[tgID] = min(needReplace[tgID]+underReplicated, r.targetGroupsReplicationFactor)
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
		r.log.Error().Msg("can't reconciliate target groups: all data-planes dead")
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
		r.log.Info().Msg("reconciliation: no changes")
		return nil
	}

	err := r.coordinator.UpdatePlacements(ctx, dataPlaneIDs, newPlacements)
	if err != nil {
		return err
	}
	r.applyLocal(dataPlaneIDs, newPlacements)
	r.log.Warn().Msgf("reconciliation: set desired state: %+v", desiredPlacements)
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
