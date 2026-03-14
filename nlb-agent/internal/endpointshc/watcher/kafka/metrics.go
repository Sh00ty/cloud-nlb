package kafka

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "nlb_agent"
	subsystem = "endpoints_hc_watcher"
)

var (
	watcherEventsCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "endpoint_status_watcher_messages_total",
		Help:      "Total number of messages handled by endpoint watcher",
	}, []string{"action"})
)
