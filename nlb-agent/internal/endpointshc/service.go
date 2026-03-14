package endpointshc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/models"
	"github.com/avast/retry-go/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

type EndpointKey struct {
	IP   string
	Port uint16
}

type status struct {
	healthy   bool
	updatedAt time.Time
}

type targetGroupEndpoints struct {
	statuses   map[EndpointKey]status
	generation uint64
}

type TargetGroupStorage interface {
	GetAllTargetGroupIDs() []models.TargetGroupID
}

type HealthServiceClient interface {
	GetEndpointStatuses(ctx context.Context, targetGroup models.TargetGroupID) ([]models.EndpointStatus, error)
}

type EndpointsHealthService struct {
	hcSvcClient HealthServiceClient

	handlingTargetGroupsGuard sync.Mutex
	// tgID -> time of full resync
	handlingTargetGroups map[models.TargetGroupID]time.Time
	forceResyncInterval  time.Duration

	actualStatusesGuard sync.Mutex
	actualStatuses      map[models.TargetGroupID]*targetGroupEndpoints

	reconcilerTaskChan chan []models.TargetGroupID

	log zerolog.Logger
}

func NewEndpointHealthService(
	hcSvcClient HealthServiceClient,
	forceResyncInterval time.Duration,
	reconcilerTaskChan chan []models.TargetGroupID,
	log zerolog.Logger,
) *EndpointsHealthService {
	log = log.With().Str("component", "endpoint_statuses_service").Logger()

	return &EndpointsHealthService{
		hcSvcClient:         hcSvcClient,
		forceResyncInterval: forceResyncInterval,

		handlingTargetGroups: make(map[models.TargetGroupID]time.Time),
		actualStatuses:       make(map[models.TargetGroupID]*targetGroupEndpoints),

		reconcilerTaskChan: reconcilerTaskChan,

		log: log,
	}
}

func (s *EndpointsHealthService) Run(ctx context.Context) {
	go func() {
		var (
			runTimeout = max(s.forceResyncInterval, 5*time.Second)
			ticker     = time.NewTicker(runTimeout)
		)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.handlingTargetGroupsGuard.Lock()
				tgIDs := make([]models.TargetGroupID, 0, len(s.handlingTargetGroups))
				for k := range s.actualStatuses {
					tgIDs = append(tgIDs, k)
				}
				s.handlingTargetGroupsGuard.Unlock()

				s.SyncStatuses(ctx, tgIDs)
			}
		}
	}()
}

func (s *EndpointsHealthService) WatchForTargetGroup(ctx context.Context, tgID models.TargetGroupID) error {
	s.handlingTargetGroupsGuard.Lock()
	defer s.handlingTargetGroupsGuard.Unlock()

	_, exists := s.handlingTargetGroups[tgID]
	if !exists {
		s.handlingTargetGroups[tgID] = time.Time{}
	}

	watchedTargetGroups.Set(float64(len(s.handlingTargetGroups)))
	return nil
}

func (s *EndpointsHealthService) StopWatchForTargetGroup(ctx context.Context, tgID models.TargetGroupID) error {
	s.handlingTargetGroupsGuard.Lock()
	delete(s.handlingTargetGroups, tgID)
	watchedTargetGroups.Set(float64(len(s.handlingTargetGroups)))
	s.handlingTargetGroupsGuard.Unlock()

	s.actualStatusesGuard.Lock()
	delete(s.actualStatuses, tgID)
	s.actualStatusesGuard.Unlock()
	return nil
}

func (s *EndpointsHealthService) IsWatchFor(ctx context.Context, tgID models.TargetGroupID) bool {
	s.handlingTargetGroupsGuard.Lock()
	defer s.handlingTargetGroupsGuard.Unlock()

	_, exists := s.handlingTargetGroups[tgID]
	return exists
}

func (s *EndpointsHealthService) SyncStatuses(ctx context.Context, tgIDs []models.TargetGroupID) {
	defer prometheus.NewTimer(syncDuration).ObserveDuration()

	needFetch := make([]models.TargetGroupID, 0, len(tgIDs))

	s.handlingTargetGroupsGuard.Lock()
	for _, tgID := range tgIDs {
		lastResyncTime, exists := s.handlingTargetGroups[tgID]
		if !exists {
			s.handlingTargetGroups[tgID] = time.Time{}
		}

		if time.Since(lastResyncTime) < s.forceResyncInterval {
			continue
		}
		needFetch = append(needFetch, tgID)
	}
	s.handlingTargetGroupsGuard.Unlock()

	wg := sync.WaitGroup{}

	syncTargetGroupsTotal.Add(float64(len(needFetch)))
	// TODO: probably we can get here a data-race, i think that we need to make this function atomic
	for _, tgID := range needFetch {
		wg.Add(1)
		tgID := tgID

		go func() {
			defer wg.Done()

			err := retry.Do(
				func() error {
					fetchStart := time.Now()

					statuses, err := s.hcSvcClient.GetEndpointStatuses(ctx, tgID)
					if err != nil {
						fetchDuration.WithLabelValues("true").Observe(time.Since(fetchStart).Seconds())
						return fmt.Errorf("getting endpoint statuses: %w", err)
					}
					fetchDuration.WithLabelValues("false").Observe(time.Since(fetchStart).Seconds())

					updated := s.updateEndpointStatuses(ctx, tgID, statuses)
					s.log.Info().
						Int("updated", updated).
						Str("tg_id", string(tgID)).
						Msg("successfully updated target group endpoint statuses")

					return nil
				},
				retry.Attempts(3),
				retry.DelayType(retry.BackOffDelay),
				retry.Delay(30*time.Millisecond),
			)
			if err != nil {
				s.log.Error().
					Err(err).
					Str("tg_id", string(tgID)).
					Msg("failed to fetch endpoint statuses for target group: wait for resync")
			}
		}()
	}
	wg.Wait()
}

func (s *EndpointsHealthService) UpdateEndpointsStatuses(
	ctx context.Context,
	statuses map[models.TargetGroupID][]models.EndpointStatus,
) error {
	log := s.log.With().Str("action", "updating endpoint statuses").Logger()

	for tgID, stats := range statuses {
		updated := s.updateEndpointStatuses(ctx, tgID, stats)
		endpointStatusUpdatesTotal.Add(float64(updated))
		log.Info().
			Str("tg_id", string(tgID)).
			Int("updated", updated).
			Msg("updated endpoint statuses from watcher")
	}
	return nil
}

func (s *EndpointsHealthService) updateEndpointStatuses(
	ctx context.Context,
	tgID models.TargetGroupID,
	statuses []models.EndpointStatus,
) int {
	s.actualStatusesGuard.Lock()
	defer s.actualStatusesGuard.Unlock()

	log := s.log.With().Str("tg_id", string(tgID))

	knownStatuses, exists := s.actualStatuses[tgID]
	if !exists {
		knownStatuses = new(targetGroupEndpoints)
		knownStatuses.statuses = make(map[EndpointKey]status, len(statuses))
		s.actualStatuses[tgID] = knownStatuses
	}

	updated := 0

	for _, stat := range statuses {
		key := EpStatusKey(stat)

		log := log.Interface("endpoint", key).Interface("status", stat.Healthy).Logger()

		actual, exists := knownStatuses.statuses[key]
		if !exists {
			updated++
			knownStatuses.statuses[key] = status{
				healthy:   stat.Healthy,
				updatedAt: stat.UpdatedAt,
			}
			log.Info().Msg("[new]: updated endpoint status")
			continue
		}
		if actual.updatedAt.After(stat.UpdatedAt) {
			log.Warn().Msg("not updated due to older updated at")
			continue
		}
		knownStatuses.statuses[key] = status{
			healthy:   stat.Healthy,
			updatedAt: stat.UpdatedAt,
		}
		if stat.Healthy != actual.healthy {
			updated++
		}
	}

	if updated > 0 {
		knownStatuses.generation++
		s.triggerReconciliation(ctx, tgID)
	}
	return updated
}

func (s *EndpointsHealthService) triggerReconciliation(ctx context.Context, tgID models.TargetGroupID) {
	select {
	case <-ctx.Done():
	case s.reconcilerTaskChan <- []models.TargetGroupID{tgID}:
		reconciliationTriggersTotal.Inc()
		s.log.Debug().Str("tg_id", string(tgID)).Msg("triggered target group reconciliation")
	}
}

func (s *EndpointsHealthService) RemoveEndpoint(ctx context.Context, tgID models.TargetGroupID, ep EndpointKey) error {
	s.actualStatusesGuard.Lock()
	defer s.actualStatusesGuard.Unlock()

	statuses, exists := s.actualStatuses[tgID]
	if !exists {
		return nil
	}
	delete(statuses.statuses, ep)
	statuses.generation++

	s.triggerReconciliation(ctx, tgID)

	s.log.Info().
		Str("tg_id", string(tgID)).
		Interface("ep_key", ep).
		Msg("removed endpoint")
	return nil
}

func (s *EndpointsHealthService) GetEndpointsStatus(
	ctx context.Context,
	tgID models.TargetGroupID,
	ep models.EndpointHdr,
) bool {
	s.actualStatusesGuard.Lock()
	defer s.actualStatusesGuard.Unlock()

	tgStatuses, exists := s.actualStatuses[tgID]
	if !exists {
		return false
	}

	key := EpHdrKey(ep)
	stat, exists := tgStatuses.statuses[key]
	if !exists {
		return false
	}
	return stat.healthy
}

func (s *EndpointsHealthService) GetTgVersion(ctx context.Context, tg models.TargetGroupID) (uint64, bool) {
	s.actualStatusesGuard.Lock()
	defer s.actualStatusesGuard.Unlock()

	info, exists := s.actualStatuses[tg]
	if !exists {
		return 0, false
	}
	return info.generation, true
}

func EpStatusKey(stat models.EndpointStatus) EndpointKey {
	return EpHdrKey(stat.Header)
}

func EpHdrKey(hdr models.EndpointHdr) EndpointKey {
	return EndpointKey{
		IP:   hdr.IP.String(),
		Port: hdr.Port,
	}
}
