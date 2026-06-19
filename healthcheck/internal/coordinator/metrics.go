package coordinator

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "health_check_node"
	subsystem = "coordinator"
)

var (
	fetchTargetsDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "fetch_targets_duration_seconds",
		Help:      "Time spent in FetchTargets (DB calls + scheduling).",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	})

	fetchedTargetsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "fetched_targets_total",
		Help:      "Total number of targets fetched from source.",
	})

	// Membership events
	membershipEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "membership_events_total",
		Help:      "Total membership events received by type.",
	}, []string{"type"}) // "dead", "new", "suspect", "unknown"

	// CDC / HandleTargetEvents
	targetEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "target_events_total",
		Help:      "Total target events processed from CDC.",
	}, []string{"operation"}) // "create", "delete", "unknown"
)
