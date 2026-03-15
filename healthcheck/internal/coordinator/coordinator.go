package coordinator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"

	"github.com/Sh00ty/cloud-nlb/healthcheck/internal/models"
	"github.com/Sh00ty/cloud-nlb/healthcheck/pkg/healthcheck"
)

type ChecksSourceRepo interface {
	GetTargets(ctx context.Context, vshards []uint) ([]healthcheck.Target, error)
	GetSettingsForTargetGroups(
		ctx context.Context,
		targetGroups []healthcheck.TargetGroupID,
	) (map[healthcheck.TargetGroupID]healthcheck.Settings, error)
}

type CheckScheduler interface {
	Add(hc models.HealthCheck) error
	Remove(target healthcheck.TargetAddr) bool
}

type CheckSharder interface {
	NeedHandle(addr healthcheck.TargetAddr) bool
	LinkTarget(target healthcheck.TargetAddr) bool
	RemoveTargetLink(target healthcheck.TargetAddr) bool
	AddNewMember(ctx context.Context, nodeID models.NodeID) ([]healthcheck.TargetAddr, error)
	RemoveMember(ctx context.Context, nodeID models.NodeID) ([]uint, error)
}

// TODO: hc settings deduplication
type Coordinator struct {
	mu                    *sync.Mutex
	sharderSchedulerGuard *sync.Mutex

	checksSource ChecksSourceRepo
	sched        CheckScheduler

	checkSharder     CheckSharder
	membershipEvents chan models.MemberShipEvent

	log zerolog.Logger
}

func NewCoordinator(ctx context.Context,
	checksSource ChecksSourceRepo,
	membershipEvents chan models.MemberShipEvent,
	sched CheckScheduler,
	sharder CheckSharder,
	log zerolog.Logger,
) (*Coordinator, error) {
	log = log.With().Str("component", "coordinator").Logger()
	c := &Coordinator{
		mu:                    &sync.Mutex{},
		sharderSchedulerGuard: &sync.Mutex{},
		membershipEvents:      membershipEvents,
		checksSource:          checksSource,
		sched:                 sched,
		checkSharder:          sharder,
		log:                   log,
	}
	return c, nil
}

func (c *Coordinator) FetchTargets(ctx context.Context, vshards []uint) error {
	durTimer := prometheus.NewTimer(fetchTargetsDuration)
	defer durTimer.ObserveDuration()

	// TODO: split fetch and sharding stages, to make waiter more efficient
	targets, err := c.checksSource.GetTargets(ctx, vshards)
	if err != nil {
		return fmt.Errorf("getting ranges for current node: %w", err)
	}
	fetchedTargetsTotal.Add(float64(len(targets)))

	targetGroupsToFetch := make([]healthcheck.TargetGroupID, 0, len(targets))
	for _, target := range targets {
		if !c.checkSharder.NeedHandle(target.ToAddr()) {
			continue
		}
		targetGroupsToFetch = append(targetGroupsToFetch, target.TargetGroup)
	}
	settingsByTg, err := c.checksSource.GetSettingsForTargetGroups(ctx, targetGroupsToFetch)
	if err != nil {
		return fmt.Errorf("getting healthcheck settings: %w", err)
	}

	// TODO: check if necessary
	c.sharderSchedulerGuard.Lock()
	defer c.sharderSchedulerGuard.Unlock()

	for _, target := range targets {
		log := c.log.With().Interface("target", target).Logger()

		if !c.checkSharder.NeedHandle(target.ToAddr()) {
			log.Info().Msgf("skip target")
			continue
		}
		settings, exists := settingsByTg[target.TargetGroup]
		if !exists {
			log.Error().Msgf("not found settings for target")
			continue
		}
		parsedHc, err := models.NewHealthCheck(target.ToAddr(), &settings)
		if err != nil {
			return fmt.Errorf("failed to create healthcheck: %w", err)
		}
		c.sched.Add(parsedHc)
		c.checkSharder.LinkTarget(target.ToAddr())

		log.Info().Msgf("added check into scheduler")
	}
	return nil
}

func (c *Coordinator) StartHandleMembershipChanges(ctx context.Context) {
	// TODO: make add events batch + jitter for cold going
	// TODO: make membership events freeze to wait some additional signal
	for {
		select {
		case <-ctx.Done():
			return
		case event, opened := <-c.membershipEvents:
			if !opened {
				return
			}
			switch event.Type {
			case models.MemberShipDead:
				membershipEventsTotal.WithLabelValues("dead").Add(1)
				c.processNodeDeath(ctx, event.From)
			case models.MemberShipNew:
				membershipEventsTotal.WithLabelValues("new").Add(1)
				c.processNewNode(ctx, event.From)
			case models.MemberShipUnknown, models.MemberShipSuspect:
				membershipEventsTotal.WithLabelValues("suspect").Add(1)
				continue
			}
		}
	}
}

func (c *Coordinator) processNodeDeath(ctx context.Context, nodeID models.NodeID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.log.Info().Msgf("processing node deletion: %s", nodeID)

	shardsToFetch, err := c.checkSharder.RemoveMember(ctx, nodeID)
	if err != nil {
		// here i think we can panic, probably it's not retriable
		c.log.Error().Err(err).Msg("sharder remove member error")
		return
	}
	// TODO: retry + don't lose membership events
	err = c.FetchTargets(ctx, shardsToFetch)
	if err != nil {
		c.log.Error().Err(err).Msg("failed to make cold start on member dead event")
	}
}

func (c *Coordinator) processNewNode(ctx context.Context, nodeID models.NodeID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.log.Info().Msgf("processing node addition: %s", nodeID)

	dropTargets, err := c.checkSharder.AddNewMember(ctx, nodeID)
	if err != nil {
		c.log.Error().Err(err).Msg("failed to add process member addition")
		return
	}
	for _, target := range dropTargets {
		if c.sched.Remove(target) {
			c.log.Info().Msgf("removed target from sched: %+v", target)
		}
	}
}

type EventOperationType int8

const (
	Unknown EventOperationType = iota
	Create
	Update
	Delete
)

type TargetEvent struct {
	Operation EventOperationType
	Timestamp time.Time
	Target    healthcheck.Target
}

func (c *Coordinator) HandleTargetEvents(ctx context.Context, targetEvents []TargetEvent) error {
	var (
		add          = make([]healthcheck.Target, 0, len(targetEvents))
		delete       = make([]healthcheck.Target, 0, len(targetEvents))
		targetGroups = make([]healthcheck.TargetGroupID, 0, len(targetEvents))
	)
	for _, event := range targetEvents {
		if !c.checkSharder.NeedHandle(event.Target.ToAddr()) {
			continue
		}
		switch event.Operation {
		case Create:
			targetEventsTotal.WithLabelValues("create").Add(1)
			add = append(add, event.Target)
		case Delete:
			targetEventsTotal.WithLabelValues("delete").Add(1)
			delete = append(delete, event.Target)
		default:
			return nil
		}
	}
	for _, needToAdd := range add {
		targetGroups = append(targetGroups, needToAdd.TargetGroup)
	}
	settingsByTg, err := c.checksSource.GetSettingsForTargetGroups(ctx, targetGroups)
	if err != nil {
		return fmt.Errorf("failed to get checks settings for targets: %w", err)
	}

	// TODO: check if necessary
	c.sharderSchedulerGuard.Lock()
	defer c.sharderSchedulerGuard.Unlock()

	for _, targetToAdd := range add {
		if !c.checkSharder.LinkTarget(targetToAdd.ToAddr()) {
			continue
		}
		settings, exists := settingsByTg[targetToAdd.TargetGroup]
		if !exists {
			return fmt.Errorf("not found settings for target %+v", targetToAdd)
		}

		parsedHc, err := models.NewHealthCheck(targetToAdd.ToAddr(), &settings)
		if err != nil {
			return fmt.Errorf("failed to create healthcheck: %w", err)
		}
		c.sched.Add(parsedHc)
		c.log.Info().Msgf("schedule hc from cdc: %v", targetToAdd)
	}
	for _, targetToDelete := range delete {
		c.sched.Remove(targetToDelete.ToAddr())
		c.checkSharder.RemoveTargetLink(targetToDelete.ToAddr())
		c.log.Info().Msgf("removed from hc via cdc: %v", targetToDelete)
	}
	return nil
}
