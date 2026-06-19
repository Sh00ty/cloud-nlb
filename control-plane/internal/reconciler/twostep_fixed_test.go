//go:build placementfix

package reconciler

import (
	"context"
	"time"
)

// versionSum totals placement versions; used to detect convergence (a pass that
// changes nothing leaves the sum unchanged).
func versionSum(r *Reconciler) uint64 {
	var s uint64
	for _, pl := range r.placements {
		s += pl.Version
	}
	return s
}

// runTwoStepPlacement runs the corrected balanced reconcile to convergence (until a
// pass changes nothing) and returns the total elapsed time to reach a balanced
// placement.
func runTwoStepPlacement(r *Reconciler) time.Duration {
	ctx := context.Background()
	start := time.Now()
	for range 50 {
		prev := versionSum(r)
		_ = r.reconcileAttemptBalanced(ctx)
		if versionSum(r) == prev {
			break
		}
	}
	return time.Since(start)
}
