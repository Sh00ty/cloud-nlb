package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/models"
	"github.com/hashicorp/go-uuid"
	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"
)

type ControlPlane interface {
	PollUpdatesIfExists(ctx context.Context, state models.NodeState, reqID string) (*models.ReconciliationUnit, error)
}

type Reconciler interface {
	UpdateDesired(ctx context.Context, recUint *models.ReconciliationUnit) error
}

type PersistentState interface {
	GetPlacement(ctx context.Context) (models.NodeState, error)
}

func NewScheduler(
	nodeID string,
	controlPlane ControlPlane,
	reconciler Reconciler,
	state PersistentState,
) *Scheduler {
	return &Scheduler{
		nodeID:               nodeID,
		controlPlane:         controlPlane,
		reconciler:           reconciler,
		state:                state,
		limiter:              rate.NewLimiter(rate.Every(4*time.Second), 4),
		afterErrorTokenUsage: 2,
		afterOkTokenUsage:    1,
	}
}

type Scheduler struct {
	nodeID               string
	controlPlane         ControlPlane
	reconciler           Reconciler
	state                PersistentState
	limiter              *rate.Limiter
	afterErrorTokenUsage int
	afterOkTokenUsage    int
	wasError             bool
}

func (s *Scheduler) Run(ctx context.Context) error {
	for {
		reqTokenUsage := s.afterOkTokenUsage
		if s.wasError {
			reqTokenUsage = s.afterErrorTokenUsage
		}
		err := s.limiter.WaitN(ctx, reqTokenUsage)
		if errors.Is(err, context.Canceled) {
			return nil
		}
		if err != nil {
			log.Error().Err(err).Msg("unexpected limiter error, sleep and set next token count to zero")
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Second):
				continue
			}
		}
		requestID, err := uuid.GenerateUUID()
		if err != nil {
			return fmt.Errorf("failed to generate uuid for request, probably need restart: %w", err)
		}
		err = s.runIteration(ctx, requestID)
		if err == nil {
			s.wasError = false
			continue
		}
		log.Error().Err(err).Msgf("scheduler: got iteration run error")
		s.wasError = true
	}
}

func (s *Scheduler) runIteration(ctx context.Context, reqID string) error {
	placement, err := s.state.GetPlacement(ctx)
	if err != nil {
		return fmt.Errorf("failed to get target groups current state from persisted state: %w", err)
	}
	placement.NodeName = s.nodeID

	reconciliationUnit, err := s.controlPlane.PollUpdatesIfExists(ctx, placement, reqID)
	if err != nil {
		return fmt.Errorf("failed to poll updates from control plane: %w", err)
	}
	if reconciliationUnit == nil {
		log.Debug().Msgf("request %s: not modified", reqID)
		return nil
	}
	log.Info().Msgf("got new state from control-plane with placement version: %d", reconciliationUnit.PlacementVersion)

	ts := time.Now()
	err = s.reconciler.UpdateDesired(ctx, reconciliationUnit)
	if err != nil {
		return fmt.Errorf("failed to reconcile incoming uint: %+v: %w", *reconciliationUnit, err)
	}
	duration := time.Since(ts)
	log.Info().Msgf("successfully reconciled uint with request id %s: duration %d ms", reqID, duration.Milliseconds())
	return nil
}
