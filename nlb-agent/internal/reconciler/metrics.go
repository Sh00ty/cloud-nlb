package reconciler

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "nlb_agent"
	subsystem = "reconciler"
)

var (
	reconcileDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "reconcile_duration_seconds",
		Help:      "Time spent reconciling a single target group.",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	}, []string{"result"}) // "changed", "in_sync", "error"

	workerQueueSize = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "worker_queue_size",
		Help:      "Current number of tasks in worker queue.",
	}, []string{"worker_id"})

	endpointsDiffTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "endpoints_diff_total",
		Help:      "Total endpoints added/removed during reconciliation.",
	}, []string{"action"}) // "add", "delete"

	desiredChangesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "desired_changes_total",
		Help:      "Total target group changes applied to desired state.",
	}, []string{"action"}) // "added", "updated", "removed"

	targetGroupsTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "target_groups_total",
		Help:      "Current total number of target groups in desired state. Can be significantly delayed.",
	})

	placementVersion = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "placement_version",
		Help:      "Current placement version on this node.",
	})

	placementVersionUpdatesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "placement_version_updates_total",
		Help:      "Total number of placement version updates.",
	})
)
