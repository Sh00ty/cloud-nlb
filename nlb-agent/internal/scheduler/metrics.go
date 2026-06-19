package scheduler

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "nlb_agent"
	subsystem = "scheduler"
)

var (
	iterationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "iteration_duration_seconds",
		Help:      "Total time spent in runIteration.",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	}, []string{"error"}) // "true" / "false"

	pollDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "poll_duration_seconds",
		Help:      "Time spent polling control plane for updates.",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	}, []string{"result"}) // "updated", "not_modified", "error"

	reconcileDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "reconcile_duration_seconds",
		Help:      "Time spent in reconciler.UpdateDesired.",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"error"}) // "true" / "false"
)

func hasError(err error) string {
	return boolToStr(err != nil)
}

func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
