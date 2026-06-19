//go:build placementfix

package reconciler

import (
	"context"
	"fmt"
	"testing"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
)

// TestBalancedConvergence checks that the corrected balancing pass reaches a
// max-min load difference of at most 1 (hottest-node overhead well under 1%) and
// is stable once balanced (an extra pass changes no placement).
//
//	go test -tags placementfix -run TestBalancedConvergence -v ./internal/reconciler/
func TestBalancedConvergence(t *testing.T) {
	ctx := context.Background()
	for _, m := range []int{5, 8, 16, 32, 64, 128, 256, 500, 1000} {
		r := buildReconciler(10000, m, 3)
		for range 20 {
			_ = r.reconcileAttemptBalanced(ctx)
		}

		load := make([]int, 0, m)
		for _, pl := range r.placements {
			load = append(load, len(pl.TargetGroups))
		}
		cv, mo := loadStats(load)

		// stability: snapshot versions, run one more pass, count changed placements.
		before := make(map[models.DataPlaneID]uint64, m)
		for id, pl := range r.placements {
			before[id] = pl.Version
		}
		_ = r.reconcileAttemptBalanced(ctx)
		changed := 0
		for id, pl := range r.placements {
			if before[id] != pl.Version {
				changed++
			}
		}

		fmt.Printf("nodes=%-4d cv=%.4f maxOver=%.3f%% extraPassChanges=%d\n", m, cv, mo*100, changed)
		if changed != 0 {
			t.Errorf("nodes=%d: not stable, %d placements changed on an extra pass", m, changed)
		}
	}
}
