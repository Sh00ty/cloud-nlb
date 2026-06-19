package stubvpp

import (
	"context"

	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/models"
	"github.com/rs/zerolog"
)

type StubVPP struct {
	log zerolog.Logger
}

func NewStubVPP(log zerolog.Logger) *StubVPP {
	return &StubVPP{
		log: log.With().Str("component", "stub_vpp").Logger(),
	}
}

func (v StubVPP) ApplySpec(ctx context.Context, tgID models.TargetGroupID, spec models.TargetGroupSpec) error {
	v.log.Warn().
		Str("target_group_id", string(tgID)).
		Interface("spec", spec).
		Msg("applied target group spec")
	return nil
}
func (v StubVPP) RemoveSpec(ctx context.Context, tgID models.TargetGroupID, spec models.TargetGroupSpec) error {
	v.log.Warn().
		Str("target_group_id", string(tgID)).
		Interface("spec", spec).
		Msg("removed target group spec")
	return nil
}
func (v StubVPP) AddEndpoints(ctx context.Context, tgID models.TargetGroupID, endpoints []models.EndpointSpec) error {
	v.log.Warn().
		Str("target_group_id", string(tgID)).
		Interface("endpoints", endpoints).
		Msg("added endpoints")
	return nil
}
func (v StubVPP) RemoveEndpoints(ctx context.Context, tgID models.TargetGroupID, endpoints []models.EndpointSpec) error {
	v.log.Warn().
		Str("target_group_id", string(tgID)).
		Interface("endpoints", endpoints).
		Msg("removed endpoints")
	return nil
}
