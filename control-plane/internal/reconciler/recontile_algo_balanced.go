//go:build placementfix

// This file is NOT part of the normal build. It is gated behind the `placementfix`
// build tag and contains a corrected balancing pass that fixes the remainder-
// concentration bug in fixDataplanePlacements: the original enforces only a lower
// bound (floor) per node, so the remainder (tgCount*RF) mod aliveNodes ends up
// stranded on a single node. This variant gives the +1 to the rem most-loaded
// nodes, yielding a max-min load difference of at most 1, and is stable at the
// balanced state (no movements once balanced).
//
// Build/run on demand:
//
//	go test -tags placementfix -run TestBalancedConvergence -v ./internal/reconciler/

package reconciler

import (
	"cmp"
	"context"
	"maps"
	"slices"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
)

func (r *Reconciler) reconcileAttemptBalanced(ctx context.Context) error {
	desired := r.calculateDesiredLazy()
	return r.fixDataplanePlacementsBalanced(ctx, desired)
}

func (r *Reconciler) fixDataplanePlacementsBalanced(
	ctx context.Context,
	desiredPlacements map[models.DataPlaneID]models.Placement,
) error {
	loads := make([]dplLoad, 0, len(desiredPlacements))
	total := 0
	for id, pl := range desiredPlacements {
		if r.dplStatuses[id].State != models.Alive {
			continue
		}
		loads = append(loads, dplLoad{id: id, load: len(pl.TargetGroups)})
		total += len(pl.TargetGroups)
	}
	m := len(loads)
	if m == 0 {
		return nil
	}

	slices.SortFunc(loads, func(a, b dplLoad) int {
		if a.load != b.load {
			return a.load - b.load
		}
		return cmp.Compare(a.id, b.id)
	})

	base := total / m
	rem := total % m
	// The rem most-loaded nodes (highest sorted indices) target base+1, the rest base.
	// Assigning the +1 to already-loaded nodes minimizes movement.
	target := func(i int) int {
		if i >= m-rem {
			return base + 1
		}
		return base
	}

	left, right := 0, m-1
	for left < right {
		deficit := target(left) - loads[left].load
		surplus := loads[right].load - target(right)
		if deficit <= 0 {
			left++
			continue
		}
		if surplus <= 0 {
			right--
			continue
		}
		moveCnt := min(deficit, surplus)

		toID, fromID := loads[left].id, loads[right].id
		toBefore, fromBefore := desiredPlacements[toID], desiredPlacements[fromID]
		toPl := models.Placement{Version: toBefore.Version, TargetGroups: maps.Clone(toBefore.TargetGroups)}
		fromPl := models.Placement{Version: fromBefore.Version, TargetGroups: maps.Clone(fromBefore.TargetGroups)}

		moved := 0
		for tgID := range fromBefore.TargetGroups {
			if moved == moveCnt {
				break
			}
			if _, exists := toPl.TargetGroups[tgID]; exists {
				continue
			}
			toPl.TargetGroups[tgID] = struct{}{}
			delete(fromPl.TargetGroups, tgID)
			moved++
			tgMovementsTotal.Inc()
		}
		if moved > 0 {
			toPl.Version++
			fromPl.Version++
			desiredPlacements[toID] = toPl
			desiredPlacements[fromID] = fromPl
			loads[left].load += moved
			loads[right].load -= moved
		}

		if loads[left].load >= target(left) {
			left++
		}
		if loads[right].load <= target(right) {
			right--
		}
		// All candidate TGs were duplicates on the destination; advance to avoid a stall.
		if moved == 0 {
			left++
		}
	}
	return r.applyDesiredState(ctx, desiredPlacements)
}
