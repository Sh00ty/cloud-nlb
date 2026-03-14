package apiruntime

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	metricsNamespace = "nlb_control_plane"
	metricsSubsystem = "api_runtime"
)

var (
	targetGroupCacheSize = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "target_group_cache_size",
		Help:      "Current number of target groups in cache.",
	})

	dataplaneCacheSize = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "dataplane_cache_size",
		Help:      "Current number of data planes in cache.",
	})

	getChangesDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "get_changes_duration_seconds",
		Help:      "Time spent in GetChangesForDataPlane (excluding long-poll wait).",
		Buckets:   []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1},
	}, []string{"result"}) // "changes", "wait", "error"

	changesNewTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "changes_new_tg_total",
		Help:      "Total new target groups returned in changes.",
	})

	changesRemovedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "changes_removed_tg_total",
		Help:      "Total removed target groups returned in changes.",
	})

	changesUpdatedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "changes_updated_tg_total",
		Help:      "Total updated target groups returned in changes.",
	})

	incomingEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "incoming_events_total",
		Help:      "Total incoming cache mutation events by type.",
	}, []string{"type"}) // "endpoint_change", "spec_change", "placement_change"

	incomingEventsSkippedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "incoming_events_skipped_total",
		Help:      "Total incoming events skipped (stale version, already applied).",
	}, []string{"type", "reason"}) // reason: "stale_version", "already_in_snapshot", "already_in_changelog", "version_gap", "unknown_tg"

	notificationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "notifications_total",
		Help:      "Total notifier notifications triggered by event type.",
	}, []string{"type"}) // "endpoint_change", "spec_change", "placement_change", "tg_update"

	coordinatorFetchDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "coordinator_fetch_duration_seconds",
		Help:      "Time spent fetching data from coordinator.",
		Buckets:   []float64{0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"target", "error"}) // target: "target_group", "dataplane"; error: "true"/"false"

	coordinatorSemaWaiters = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "coordinator_sema_waiters",
		Help:      "Current number of goroutines waiting on coordinator semaphore.",
	})

	gcDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "gc_duration_seconds",
		Help:      "Time spent in garbage collection cycle.",
		Buckets:   []float64{0.0001, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5},
	})

	newNodeProcessedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "new_node_processed_total",
		Help:      "Total new data plane nodes discovered via GetChangesForDataPlane.",
	})
)

func errorToBool(err error) string {
	errLabel := "false"
	if err != nil {
		errLabel = "true"
	}
	return errLabel
}
