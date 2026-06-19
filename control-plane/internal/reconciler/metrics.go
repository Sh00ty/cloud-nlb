package reconciler

import (
	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	metricsNamespace = "nlb_control_plane"
	metricsSubsystem = "reconciler"
)

var (
	dataPlanesByState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "data_planes_by_state",
		Help:      "Current number of data planes by state.",
	}, []string{"state"}) // "alive", "dead", "drained"

	targetGroupsTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "target_groups_total",
		Help:      "Current total number of target groups known to reconciler.",
	})

	dataPlanesTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "data_planes_total",
		Help:      "Current total number of data planes known to reconciler.",
	})

	underReplicatedTargetGroups = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "under_replicated_target_groups",
		Help:      "Current number of target groups with fewer assignments than replication factor.",
	})

	eventChannelUsage = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "event_channel_usage",
		Help:      "Current number of events in the event channel buffer.",
	})

	delayedEventsQueueSize = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "delayed_events_queue_size",
		Help:      "Current number of events in the delayed events queue.",
	})

	incomingEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "incoming_events_total",
		Help:      "Total incoming events by type.",
	}, []string{"type"}) // "target-group-created", "data-plane-alive", "data-plane-dead", "data-plane-drained", "run-reconcile"

	skippedEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "skipped_events_total",
		Help:      "Total events skipped (duplicate alive, stale death, etc).",
	}, []string{"type", "reason"}) // reason: "already_alive", "stale_death", "already_dead", "tg_exists"

	delayedEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "delayed_events_total",
		Help:      "Total events that were delayed for later processing.",
	}, []string{"type"})

	reconcileTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "reconcile_total",
		Help:      "Total reconciliation attempts by result.",
	}, []string{"result"}) // "success", "no_changes", "error", "no_alive_dpl"

	reconcileDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "reconcile_duration_seconds",
		Help:      "Time spent in a single reconciliation attempt.",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	})

	reconcileRetriesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "reconcile_retries_total",
		Help:      "Total number of reconciliation retries.",
	})

	placementUpdatesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "placement_updates_total",
		Help:      "Total number of individual DPL placement updates sent to coordinator.",
	})

	placementUpdateDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "placement_update_duration_seconds",
		Help:      "Time spent in coordinator.UpdatePlacements call.",
		Buckets:   []float64{0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	})

	tgMovementsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "tg_movements_total",
		Help:      "Total target group movements between data planes during rebalance.",
	})

	tgReplacementsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "tg_replacements_total",
		Help:      "Total target groups replaced from dead nodes to alive ones.",
	})

	dplLoadDistribution = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "dpl_load_distribution",
		Help:      "Distribution of target group count per alive data plane after reconciliation.",
		Buckets:   prometheus.LinearBuckets(0, 5, 20), // 0, 5, 10, ..., 95
	})
)

func (r *Reconciler) updateStateGauges() {
	counts := map[string]float64{
		"alive":   0,
		"dead":    0,
		"drained": 0,
	}
	for _, status := range r.dplStatuses {
		switch status.State {
		case models.Alive:
			counts["alive"]++
		case models.Dead:
			counts["dead"]++
		case models.Drained:
			counts["drained"]++
		}
	}
	for state, count := range counts {
		dataPlanesByState.WithLabelValues(state).Set(count)
	}
	dataPlanesTotal.Set(float64(len(r.dplStatuses)))
	targetGroupsTotal.Set(float64(len(r.targetGroups)))

	underReplicated := 0
	for _, tg := range r.targetGroups {
		if len(tg.Assignments) < r.targetGroupsReplicationFactor {
			underReplicated++
		}
	}
	underReplicatedTargetGroups.Set(float64(underReplicated))
}

func (r *Reconciler) recordLoadDistribution() {
	for dplID, pl := range r.placements {
		if r.dplStatuses[dplID].State != models.Alive {
			continue
		}
		dplLoadDistribution.Observe(float64(len(pl.TargetGroups)))
	}
}
