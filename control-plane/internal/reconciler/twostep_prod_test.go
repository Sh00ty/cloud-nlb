//go:build !placementfix

package reconciler

import (
	"context"
	"time"
)

// runTwoStepPlacement runs the production two-step reconcile once (the production
// balancing pass is single-shot) and returns the elapsed time.
func runTwoStepPlacement(r *Reconciler) time.Duration {
	start := time.Now()
	_ = r.reconcileAttempt(context.Background())
	return time.Since(start)
}
