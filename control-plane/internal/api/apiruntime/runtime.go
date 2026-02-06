package apiruntime

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Sh00ty/network-lb/control-plane/internal/models"
	"github.com/rs/zerolog/log"
)

type Coordinator interface {
}

type Notifier interface {
	ID() uint64
	Notify(ctx context.Context)
	Wait(ctx context.Context) bool
	Expire()
	IsExpired() bool
}

type dataPlaneCacheEntry struct {
	dplInfo       models.DataPlanePlacementInfo
	subscriptions map[uint64]Notifier
	mu            sync.Mutex
}

func (e *dataPlaneCacheEntry) notifyAll(ctx context.Context) {
	for _, sub := range e.subscriptions {
		sub.Notify(ctx)
		delete(e.subscriptions, sub.ID())
	}
}

type targetGroupCacheEntry struct {
	tg            models.TargetGroup
	subscriptions map[uint64]Notifier
	mu            sync.Mutex
}

func (e *targetGroupCacheEntry) notifyAll(ctx context.Context) {
	for _, sub := range e.subscriptions {
		sub.Notify(ctx)
		delete(e.subscriptions, sub.ID())
	}
}

type ApiRuntime struct {
	targetGroupCache map[models.TargetGroupID]*targetGroupCacheEntry
	tgGuard          *sync.RWMutex

	dataPlaneCache map[models.DataPlaneID]*dataPlaneCacheEntry
	dplGuard       *sync.RWMutex

	notifierIdSaltGen atomic.Uint64
	gcTicker          *time.Ticker
	crd               Coordinator
}

func NewApiRuntime(gcInterval time.Duration, crd Coordinator) *ApiRuntime {
	ticker := time.NewTicker(gcInterval)
	return &ApiRuntime{
		targetGroupCache: make(map[models.TargetGroupID]*targetGroupCacheEntry, 128),
		tgGuard:          &sync.RWMutex{},

		dataPlaneCache: make(map[models.DataPlaneID]*dataPlaneCacheEntry, 128),
		dplGuard:       &sync.RWMutex{},

		notifierIdSaltGen: atomic.Uint64{},
		gcTicker:          ticker,
		crd:               crd,
	}
}

func (ar *ApiRuntime) RunGarbageCollection(ctx context.Context) {
	// TODO: make self removing after successful request
	// second iteration must be with clean flag
	// or we can store all entries in notifier and later make self cleaning
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ar.gcTicker.C:
			if !ok {
				return
			}
			deleted := ar.collectGarbage()
			log.Debug().Msgf("end garbage collection cycle, deleted %d subs", deleted)
		}
	}
}

func (ar *ApiRuntime) collectGarbage() int {
	ar.tgGuard.RLock()
	defer ar.tgGuard.RUnlock()

	deleted := 0
	for _, tgCacheEntry := range ar.targetGroupCache {
		tgCacheEntry.mu.Lock()
		for _, sub := range tgCacheEntry.subscriptions {
			if sub.IsExpired() {
				deleted++
				delete(tgCacheEntry.subscriptions, sub.ID())
			}
		}
		tgCacheEntry.mu.Unlock()
	}
	return deleted
}

func (ar *ApiRuntime) HandleEndpointChange(
	ctx context.Context,
	ev models.EndpointEvent,
	rev uint64,
) {
	ar.tgGuard.RLock()
	defer ar.tgGuard.RUnlock()

	tgCacheEntry, exists := ar.targetGroupCache[ev.TargetGroupID]
	if !exists {
		log.Error().Msgf("try to add endpoint to unknown target group: %s", ev.TargetGroupID)
		return
	}
	tgCacheEntry.mu.Lock()
	defer tgCacheEntry.mu.Unlock()

	if tgCacheEntry.tg.ChangelogStartVersion > ev.DesiredVersion {
		log.Info().Msgf("endpoint event with timestamp %d already in snapshot", ev.DesiredVersion)
		return
	}
	if tgCacheEntry.tg.ChangelogStartVersion == 0 {
		// TODO: fetch snapshot from coordinator, if we add
		// here too old version we can break all change line because
		// all previous messages will automaticly become outdated
		tgCacheEntry.tg.ChangelogStartVersion = ev.DesiredVersion
		log.Info().Msgf(
			"updated changelog start version for tg %s on %d",
			ev.TargetGroupID, ev.DesiredVersion,
		)
	}
	if len(tgCacheEntry.tg.EndpointsChangelog) == 0 {
		tgCacheEntry.tg.EndpointsChangelog = append(tgCacheEntry.tg.EndpointsChangelog, ev)
		tgCacheEntry.notifyAll(ctx)
		return
	}

	last := tgCacheEntry.tg.EndpointsChangelog[len(tgCacheEntry.tg.EndpointsChangelog)-1]
	if last.DesiredVersion >= ev.DesiredVersion {
		log.Info().Msgf("endpoint event with timestamp %d already in changelog", ev.DesiredVersion)
		return
	}
	if last.DesiredVersion+1 != ev.DesiredVersion {
		log.Warn().Msgf("income version is too big; must drop all tg %s cache", ev.TargetGroupID)
		// TODO:
		return
	}
	tgCacheEntry.tg.EndpointsChangelog = append(tgCacheEntry.tg.EndpointsChangelog, ev)
	tgCacheEntry.notifyAll(ctx)
}

func (ar *ApiRuntime) HandleTgSpecChange(
	ctx context.Context,
	spec models.TargetGroupSpec,
	desiredVersion uint64,
	rev uint64,
) {

	ar.tgGuard.Lock()
	defer ar.tgGuard.Unlock()

	tgCacheEntry, exists := ar.targetGroupCache[spec.ID]
	if !exists {
		ar.targetGroupCache[spec.ID] = &targetGroupCacheEntry{
			tg: models.TargetGroup{
				Spec:        spec,
				SpecVersion: desiredVersion,
			},
			subscriptions: make(map[uint64]Notifier, 128),
		}
		log.Info().Msgf(
			"added new target group into cache with desired version: %d",
			desiredVersion,
		)
		return
	}
	tgCacheEntry.mu.Lock()
	defer tgCacheEntry.mu.Unlock()

	if tgCacheEntry.tg.SpecVersion >= desiredVersion {
		log.Warn().Msgf("got outdated tg %s version %d", spec.ID, desiredVersion)
		return
	}
	tgCacheEntry.tg.Spec = spec
	tgCacheEntry.tg.SpecVersion = desiredVersion
	tgCacheEntry.notifyAll(ctx)

	log.Info().Msgf("updated tg %s spec, set desired version %d", spec.ID, desiredVersion)
}
