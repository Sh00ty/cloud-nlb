package reconciler

import (
	"context"
	"fmt"

	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/models"
	"github.com/rs/zerolog/log"
)

type Storage interface {
	SetAssignment(ctx context.Context, ver uint64) error
	SetTargetGroupSpecVersion(ctx context.Context, tgID models.TargetGroupID, ver uint64) error
	SetTargetGroupEndpointVersion(ctx context.Context, tgID models.TargetGroupID, ver uint64) error
	RemoveTargetGroup(ctx context.Context, tgID models.TargetGroupID) error
}

type Reconciler struct {
	stor Storage
}

func New(stateStor Storage) *Reconciler {
	return &Reconciler{stor: stateStor}
}

func (r *Reconciler) Reconcile(ctx context.Context, recUint *models.ReconciliationUnit) error {
	for _, added := range recUint.Added {
		err := r.stor.SetTargetGroupSpecVersion(ctx, added.ID, added.SpecVersion)
		if err != nil {
			log.Error().Err(err).Msgf("failed to update spec version for tg %s on %d", added.ID, added.SpecVersion)
		} else {
			log.Info().Msgf("successfully updated spec %s version to %d", added.ID, added.SpecVersion)
		}
		err = r.stor.SetTargetGroupEndpointVersion(ctx, added.ID, added.EndpointsVersion)
		if err != nil {
			log.Error().Err(err).Msgf("failed to endpoints spec version for tg %s on %d", added.ID, added.EndpointsVersion)
		} else {
			log.Info().Msgf("successfully updated endpoint %s version to %d", added.ID, added.EndpointsVersion)
		}
	}
	for _, updated := range recUint.Updated {
		if updated.SpecVersion != 0 {
			err := r.stor.SetTargetGroupSpecVersion(ctx, updated.ID, updated.SpecVersion)
			if err != nil {
				log.Error().Err(err).Msgf("failed to update spec version for tg %s on %d", updated.ID, updated.SpecVersion)
			} else {
				log.Info().Msgf("successfully updated spec %s version to %d", updated.ID, updated.SpecVersion)
			}
		}
		if updated.EndpointsVersion != 0 {
			err := r.stor.SetTargetGroupEndpointVersion(ctx, updated.ID, updated.EndpointsVersion)
			if err != nil {
				log.Error().Err(err).Msgf("failed to endpoints spec version for tg %s on %d", updated.ID, updated.EndpointsVersion)
			} else {
				log.Info().Msgf("successfully updated endpoint %s version to %d", updated.ID, updated.EndpointsVersion)
			}
		}
	}
	for _, toDelete := range recUint.Removed {
		err := r.stor.RemoveTargetGroup(ctx, toDelete)
		if err != nil {
			log.Error().Err(err).Msgf("failed to remove target group %s", toDelete)
		}
	}
	err := r.stor.SetAssignment(ctx, recUint.PlacementVersion)
	if err != nil {
		return fmt.Errorf("failed to set new assignment version to %d", recUint.PlacementVersion)
	}
	return nil
}
