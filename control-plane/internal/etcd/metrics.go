package etcd

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	metricsNamespace = "nlb_control_plane"
	metricsSubsystem = "etcd"
)

var (
	watchEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "watch_events_total",
		Help:      "Total etcd watch events processed by prefix.",
	}, []string{"prefix"})

	watchErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "watch_errors_total",
		Help:      "Total etcd watch errors (canceled, unexpected).",
	}, []string{"prefix", "reason"}) // "canceled", "error", "handler_error"

	watchRestartsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "watch_restarts_total",
		Help:      "Total watcher restarts.",
	}, []string{"prefix", "reason"}) // "reset_revision", "canceled"

	watchLastRevision = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "watch_last_revision",
		Help:      "Last processed etcd revision per watcher prefix.",
	}, []string{"prefix"})

	kvOperationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "kv_operation_duration_seconds",
		Help:      "Time spent on etcd KV operations.",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	}, []string{"operation", "error"}) // operation: "update_placements", "get_tg_diff", "set_tg_spec", "add_endpoint", "remove_endpoint", "initial_sync"; error: "true"/"false"

	txRetriesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "tx_retries_total",
		Help:      "Total etcd transaction retries in ExecuteIncrementally.",
	}, []string{"operation"})

	isLeader = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "is_leader",
		Help:      "1 if this instance is the reconciler leader, 0 otherwise.",
	})
)

func errToBool(err error) string {
	errLabel := "false"
	if err != nil {
		errLabel = "true"
	}
	return errLabel
}
