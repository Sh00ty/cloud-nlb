package sender

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "health_check_node"
	subsystem = "sender"
)

var (
	sendDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "send_duration_seconds",
		Help:      "Time spent sending a single event (including retries).",
		Buckets:   []float64{0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	}, []string{"success"}) // "true" / "false"

	// Очередь неотправленных
	unsentQueueSize = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "unsent_queue_size",
		Help:      "Current number of events in the unsent queue.",
	})

	// Ресинк неотправленных
	unsentResyncDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "unsent_resync_duration_seconds",
		Help:      "Time spent resending unsent events batch.",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	}, []string{"success"}) // "true" / "false"

	unsentResyncEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "unsent_resync_events_total",
		Help:      "Total events processed during unsent resync.",
	}, []string{"result"}) // "sent" / "remained"
)

func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
