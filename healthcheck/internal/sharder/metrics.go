package sharder

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "health_check_node"
	subsystem = "sharder"
)

var (
	addMemberDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "add_member_duration_seconds",
		Help:      "Time spent processing AddNewMember.",
		Buckets:   []float64{0.0001, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
	})

	removeMemberDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "remove_member_duration_seconds",
		Help:      "Time spent processing RemoveMember.",
		Buckets:   []float64{0.0001, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
	})

	needHandleDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "need_handle_duration_seconds",
		Help:      "Time spent in NeedHandle lookup.",
		Buckets:   []float64{0.000001, 0.00001, 0.0001, 0.001, 0.01},
	})

	myVshardsCount = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "my_vshards_count",
		Help:      "Current number of vshards owned by this node.",
	})

	droppedTargetsOnRebalance = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "dropped_targets_on_rebalance_total",
		Help:      "Total targets dropped due to rebalance on new member.",
	})

	acquiredVshardsOnRebalance = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "acquired_vshards_on_rebalance_total",
		Help:      "Total vshards acquired due to rebalance on member death.",
	})
)
