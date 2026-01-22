package coordinator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/Sh00ty/network-lb/health-check-node/internal/models"
	"github.com/Sh00ty/network-lb/health-check-node/pkg/healthcheck"
)

type ChecksSourceRepo interface {
	GetTargets(ctx context.Context, vshards []uint) ([]healthcheck.Target, error)
	GetSettingsForTargets(ctx context.Context, checkIDs []int64) (map[int64]healthcheck.Settings, error)
}

type CheckScheduller interface {
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
	mu               *sync.Mutex
	checksSource     ChecksSourceRepo
	sched            CheckScheduller
	checkSharder     CheckSharder
	membershipEvents chan models.MemberShipEvent
}

func NewCoordinator(ctx context.Context,
	checksSource ChecksSourceRepo,
	membershipEvents chan models.MemberShipEvent,
	sched CheckScheduller,
	sharder CheckSharder,
) (*Coordinator, error) {
	c := &Coordinator{
		mu:               &sync.Mutex{},
		membershipEvents: membershipEvents,
		checksSource:     checksSource,
		sched:            sched,
		checkSharder:     sharder,
	}
	return c, nil
}

func (c *Coordinator) FetchTargets(ctx context.Context, vshards []uint) error {
	// TODO: split fetch and sharding stages, to make wainter more eficcient
	targets, err := c.checksSource.GetTargets(ctx, vshards)
	if err != nil {
		return fmt.Errorf("failed to get ranges for current node: %w", err)
	}
	settingsToFetch := make([]int64, 0, len(targets))
	for _, target := range targets {
		if !c.checkSharder.NeedHandle(target.ToAddr()) {
			continue
		}
		settingsToFetch = append(settingsToFetch, target.SettingID)
	}
	settingsByID, err := c.checksSource.GetSettingsForTargets(ctx, settingsToFetch)
	if err != nil {
		return fmt.Errorf("failed to get check settings: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, target := range targets {
		if !c.checkSharder.NeedHandle(target.ToAddr()) {
			log.Info().Msgf("skip target %v", target)
			continue
		}
		settings, exists := settingsByID[target.SettingID]
		if !exists {
			log.Error().Msgf("not found settings for target: %+v", target)
			continue
		}
		parcedHc, err := models.NewHealthCheck(target.ToAddr(), &settings)
		if err != nil {
			return fmt.Errorf("failed to create healthcheck: %w", err)
		}
		c.sched.Add(parcedHc)
		c.checkSharder.LinkTarget(target.ToAddr())

		log.Info().Msgf("added check into sheduller: %v", target)
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
				c.processNodeDeath(ctx, event.From)
			case models.MemberShipNew:
				c.processNewNode(ctx, event.From)
			case models.MemberShipUnknown, models.MemberShipSuspect:
				continue
			}
		}
	}
}

func (c *Coordinator) processNodeDeath(ctx context.Context, nodeID models.NodeID) {
	c.mu.Lock()
	log.Info().Msgf("processing node deletion: %s", nodeID)
	c.mu.Unlock()

	shardsToFetch, err := c.checkSharder.RemoveMember(ctx, nodeID)
	if err != nil {
		// here i think we can panic, probably it's not retriable
		log.Error().Err(err).Msg("sharder remove member error")
		return
	}
	// TODO: retrys + don't lose membership events
	err = c.FetchTargets(ctx, shardsToFetch)
	if err != nil {
		log.Error().Err(err).Msg("failed to make cold start on member dead event")
	}
}

func (c *Coordinator) processNewNode(ctx context.Context, nodeID models.NodeID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	log.Info().Msgf("processing node addition: %s", nodeID)

	dropTargets, err := c.checkSharder.AddNewMember(ctx, nodeID)
	if err != nil {
		log.Error().Err(err).Msg("failed to add process member addition")
		return
	}
	for _, target := range dropTargets {
		if c.sched.Remove(target) {
			log.Info().Msgf("removed target from sched: %+v", target)
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
		add      = make([]healthcheck.Target, 0, len(targetEvents))
		delete   = make([]healthcheck.Target, 0, len(targetEvents))
		checkIDs = make([]int64, 0, len(targetEvents))
	)
	for _, event := range targetEvents {
		if !c.checkSharder.NeedHandle(event.Target.ToAddr()) {
			continue
		}
		switch event.Operation {
		case Create:
			add = append(add, event.Target)
		case Delete:
			delete = append(delete, event.Target)
		default:
			return nil
		}
	}
	for _, needToAdd := range add {
		checkIDs = append(checkIDs, needToAdd.SettingID)
	}
	settingsByID, err := c.checksSource.GetSettingsForTargets(ctx, checkIDs)
	if err != nil {
		return fmt.Errorf("failed to get checks settings for targers: %w", err)
	}
	for _, targetToAdd := range add {
		if !c.checkSharder.LinkTarget(targetToAdd.ToAddr()) {
			continue
		}
		settings, exists := settingsByID[targetToAdd.SettingID]
		if !exists {
			return fmt.Errorf("not found settings for target %+v", targetToAdd)
		}

		parcedHc, err := models.NewHealthCheck(targetToAdd.ToAddr(), &settings)
		if err != nil {
			// TODO:
			return fmt.Errorf("failed to create healthcheck: %w", err)
		}
		c.sched.Add(parcedHc)
		log.Info().Msgf("schedule hc from cdc: %v", targetToAdd)
	}
	for _, targetToDelete := range delete {
		c.sched.Remove(targetToDelete.ToAddr())
		c.checkSharder.RemoveTargetLink(targetToDelete.ToAddr())
		log.Info().Msgf("removed from hc via cdc: %v", targetToDelete)
	}
	return nil
}
