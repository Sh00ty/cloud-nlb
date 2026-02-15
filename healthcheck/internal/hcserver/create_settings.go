package hcserver

import (
	"context"
	"net"

	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/healthcheck"
	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/protobuf/api/proto/hcpbv1"
	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/strategies"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var testTarget = healthcheck.TargetAddr{
	RealIP: net.IPv4(1, 1, 1, 1),
	Port:   443,
}

func (srv *Server) CreateSettings(
	ctx context.Context,
	req *hcpbv1.CreateSettingsRequest,
) (*hcpbv1.CreateSettingsResponse, error) {
	if req.Settings == nil {
		return nil, status.Error(codes.InvalidArgument, "settings are required")
	}
	js, err := req.Settings.StrategySettings.MarshalJSON()
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to marshal settings to json: %v", err)
	}
	strategyType := healthcheck.MockStrategy
	switch req.Settings.Strategy {
	case hcpbv1.StrategyType_STRATEGY_TYPE_HTTP:
		strategyType = healthcheck.HTTPStrategy
	case hcpbv1.StrategyType_STRATEGY_TYPE_TCP:
		strategyType = healthcheck.TCPStrategy
	case hcpbv1.StrategyType_STRATEGY_TYPE_MOCK:
		strategyType = healthcheck.MockStrategy
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unknown strategy type %s", req.Settings.Strategy)
	}

	_, err = strategies.NewStrategy(strategyType, testTarget, js)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid settings type or strategy settings: %v", err)
	}

	st := healthcheck.Settings{
		TargetGroup:            healthcheck.TargetGroupID(req.Settings.TargetGroup),
		Strategy:               strategyType,
		StrategySettings:       js,
		SuccessBeforePassing:   uint8(req.Settings.SuccessBeforePassing),
		FailuresBeforeCritical: uint8(req.Settings.FailuresBeforeCritical),
		Interval:               req.Settings.Interval.AsDuration(),
		InitialState:           req.Settings.InitialState,
	}
	err = srv.repo.CreateHealthCheckSetting(ctx, st)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create hc settings: %v", err)
	}
	return &hcpbv1.CreateSettingsResponse{Success: true}, nil
}
