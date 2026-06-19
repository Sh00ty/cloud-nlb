package vppmetrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "nlb_agent"
	subsystem = "vpp"
)

var (
	operationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "operation_duration_seconds",
		Help:      "Time spent on VPP operations.",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
	}, []string{"operation", "error"}) // operation: "apply_spec", "remove_spec", "add_endpoints", "remove_endpoints"

	endpointsAffected = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "endpoints_affected_total",
		Help:      "Total number of endpoints added or removed.",
	}, []string{"operation"}) // "add", "remove"
)

func errToBoolStr(err error) string {
	if err == nil {
		return "false"
	}
	return "true"
}
