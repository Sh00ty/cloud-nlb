package scheduler

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "health_check_node"
	subsystem = "scheduler"
)

var (
	heapSize = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "heap_size",
		Help:      "Current number of health checks in the invocation heap.",
	})

	scheduleDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "schedule_duration_seconds",
		Help:      "Time spent scheduling a single health check.",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	})

	invocationDelay = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "invocation_delay_seconds",
		Help:      "Delay between scheduled invoke time and actual execution start.",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	})

	addsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "adds_total",
		Help:      "Total number of health checks added to the scheduler.",
	})

	removesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "removes_total",
		Help:      "Total number of health check removal attempts.",
	}, []string{"found"})

	emptyLoopIterationsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "empty_loop_iterations_total",
		Help:      "Total number of loop iterations where heap was empty.",
	})
)

func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
