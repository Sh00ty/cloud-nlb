package executor

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "health_check_node"
	subsystem = "executor"
)

var (
	channelBufferUsage = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "channel_buffer_usage",
		Help:      "Current number of tasks waiting in the input channel buffer.",
	})

	channelBufferCapacity = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "channel_buffer_capacity",
		Help:      "Total capacity of the input channel buffer.",
	})

	taskDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "task_duration_seconds",
		Help:      "Time spent executing a single health check task by a worker.",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"changed"})
)

func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
