package reconciler

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
	"github.com/rs/zerolog"
)

// noopCoordinator replaces the etcd-backed coordinator so the benchmark measures
// only in-memory placement generation (calculateDesiredLazy + fixDataplanePlacements
// + local apply), excluding network round-trips to etcd.
type noopCoordinator struct{}

func (noopCoordinator) UpdatePlacements(_ context.Context, _ []models.DataPlaneID, _ []models.Placement) error {
	return nil
}

// buildReconciler constructs a reconciler with numNodes alive data-plane nodes and
// numTG target groups, all initially unplaced (cold placement scenario).
func buildReconciler(numTG, numNodes, rf int) *Reconciler {
	r := NewReconciler(noopCoordinator{}, rf, time.Minute, time.Hour, zerolog.Nop())
	// these background timers are never driven by the benchmark; stop them so they
	// do not accumulate across iterations.
	r.forceReconcileTicker.Stop()
	r.delayEventTimer.Stop()

	nodes := make([]models.DataPlaneState, numNodes)
	for i := range numNodes {
		nodes[i] = models.DataPlaneState{
			ID:     models.DataPlaneID("dpl-" + strconv.Itoa(i)),
			Status: models.Alive,
		}
	}
	tgs := make([]models.TargetGroupID, numTG)
	for i := range numTG {
		tgs[i] = models.TargetGroupID("tg-" + strconv.Itoa(i))
	}
	r.Init(map[models.DataPlaneID]models.Placement{}, nodes, tgs)
	return r
}

var placementCases = []struct{ tg, nodes, rf int }{
	{1000, 50, 3},
	{10000, 500, 3},
	{50000, 500, 3},
	{100000, 1000, 3},
}

// BenchmarkPlacementCold measures full placement generation from an empty initial
// state: every target group must be replicated rf times and balanced across nodes.
func BenchmarkPlacementCold(b *testing.B) {
	ctx := context.Background()
	for _, c := range placementCases {
		b.Run(fmt.Sprintf("tg=%d/nodes=%d/rf=%d", c.tg, c.nodes, c.rf), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				r := buildReconciler(c.tg, c.nodes, c.rf)
				b.StartTimer()
				if err := r.reconcileAttempt(ctx); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkCalculateDesiredLazy isolates step 1 of the algorithm (replacement of
// under-replicated target groups). It is read-only with respect to reconciler state,
// so it can be looped without rebuilding.
func BenchmarkCalculateDesiredLazy(b *testing.B) {
	for _, c := range placementCases {
		b.Run(fmt.Sprintf("tg=%d/nodes=%d/rf=%d", c.tg, c.nodes, c.rf), func(b *testing.B) {
			r := buildReconciler(c.tg, c.nodes, c.rf)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = r.calculateDesiredLazy()
			}
		})
	}
}

// BenchmarkPlacementNodeDeath measures the cost of re-reconciling after a single
// node fails in an already balanced cluster (the steady-state churn case).
func BenchmarkPlacementNodeDeath(b *testing.B) {
	ctx := context.Background()
	tg, nodes, rf := 10000, 500, 3
	b.Run(fmt.Sprintf("tg=%d/nodes=%d/rf=%d", tg, nodes, rf), func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			r := buildReconciler(tg, nodes, rf)
			if err := r.reconcileAttempt(ctx); err != nil { // reach balanced steady state
				b.Fatal(err)
			}
			victim := models.DataPlaneID("dpl-0")
			st := r.dplStatuses[victim]
			st.State = models.Dead
			r.dplStatuses[victim] = st
			b.StartTimer()
			if err := r.reconcileAttempt(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
}
