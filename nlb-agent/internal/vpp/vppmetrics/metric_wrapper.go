package vppmetrics

import (
	"context"
	"time"

	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/models"
	"github.com/rs/zerolog"
)

type RealVpp interface {
	ApplySpec(ctx context.Context, tgID models.TargetGroupID, spec models.TargetGroupSpec) error
	RemoveSpec(ctx context.Context, tgID models.TargetGroupID, spec models.TargetGroupSpec) error
	AddEndpoints(ctx context.Context, tgID models.TargetGroupID, desired []models.EndpointSpec) error
	RemoveEndpoints(ctx context.Context, tgID models.TargetGroupID, endpoints []models.EndpointSpec) error
}

type VPPMetricsWrapper struct {
	log zerolog.Logger
	vpp RealVpp
}

func NewVPPMetricsWrapper(log zerolog.Logger, vpp RealVpp) *VPPMetricsWrapper {
	return &VPPMetricsWrapper{
		log: log.With().Str("component", "vpp_metrics_wrapper").Logger(),
		vpp: vpp,
	}
}

func (v VPPMetricsWrapper) ApplySpec(ctx context.Context, tgID models.TargetGroupID, spec models.TargetGroupSpec) (err error) {
	ts := time.Now()

	defer func() {
		operationDuration.WithLabelValues(
			"add_spec",
			errToBoolStr(err),
		).Observe(time.Since(ts).Seconds())

		if err == nil {
			v.log.Info().
				Str("target_group_id", string(tgID)).
				Interface("spec", spec).
				Msg("applied target group spec")
		}
	}()

	return v.vpp.ApplySpec(ctx, tgID, spec)
}
func (v VPPMetricsWrapper) RemoveSpec(ctx context.Context, tgID models.TargetGroupID, spec models.TargetGroupSpec) (err error) {
	ts := time.Now()
	defer func() {
		operationDuration.WithLabelValues(
			"remove_spec",
			errToBoolStr(err),
		).Observe(time.Since(ts).Seconds())

		if err == nil {
			v.log.Info().
				Str("target_group_id", string(tgID)).
				Interface("spec", spec).
				Msg("removed target group spec")
		}
	}()
	return v.vpp.RemoveSpec(ctx, tgID, spec)
}

func (v VPPMetricsWrapper) AddEndpoints(ctx context.Context, tgID models.TargetGroupID, endpoints []models.EndpointSpec) (err error) {
	ts := time.Now()
	defer func() {
		operationDuration.WithLabelValues(
			"add_endpoints",
			errToBoolStr(err),
		).Observe(time.Since(ts).Seconds())

		if err == nil {
			endpointsAffected.WithLabelValues("add").Add(float64(len(endpoints)))
			v.log.Info().
				Str("target_group_id", string(tgID)).
				Interface("endpoints", endpoints).
				Msg("added endpoints")
		}
	}()

	return v.vpp.AddEndpoints(ctx, tgID, endpoints)
}

func (v VPPMetricsWrapper) RemoveEndpoints(ctx context.Context, tgID models.TargetGroupID, endpoints []models.EndpointSpec) (err error) {
	ts := time.Now()

	defer func() {
		operationDuration.WithLabelValues(
			"remove_endpoints",
			errToBoolStr(err),
		).Observe(time.Since(ts).Seconds())

		if err == nil {
			v.log.Info().
				Str("target_group_id", string(tgID)).
				Interface("endpoints", endpoints).
				Msg("removed endpoints")

			endpointsAffected.WithLabelValues("remove").Add(float64(len(endpoints)))
		}
	}()

	return v.vpp.RemoveEndpoints(ctx, tgID, endpoints)
}
