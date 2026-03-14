package endpointshc

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "nlb_agent"
	subsystem = "endpoints_hc"
)

var (
	watchedTargetGroups = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "watched_target_groups",
		Help:      "Current number of target groups being watched.",
	})

	syncDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "sync_duration_seconds",
		Help:      "Total time spent in SyncStatuses (all TGs, including retries).",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	})

	syncTargetGroupsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "sync_target_groups_total",
		Help:      "Total number of target groups fetched during syncs.",
	})

	fetchDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "fetch_duration_seconds",
		Help:      "Time spent fetching endpoint statuses for a single target group (including retries).",
		Buckets:   []float64{0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	}, []string{"error"}) // "true" / "false"

	endpointStatusUpdatesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "endpoint_status_updates_total",
		Help:      "Total number of endpoint statuses that actually changed.",
	})

	reconciliationTriggersTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "reconciliation_triggers_total",
		Help:      "Total number of reconciliation triggers sent.",
	})
)
