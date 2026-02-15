package hcserver

import (
	"context"

	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/models"
	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/sharder"
	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/healthcheck"
	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/protobuf/api/proto/hcpbv1"
)

type Sharder interface {
	GetTargetVshard(targetKey string) sharder.Vshard
}

type HealthRepository interface {
	CreateHealthCheckSetting(ctx context.Context, check healthcheck.Settings) error
	CreateTargets(ctx context.Context, targets []healthcheck.Target, vshards []string) (uint, error)
	RemoveTargets(ctx context.Context, targets []healthcheck.Target) (uint, error)

	GetTargetStatuses(
		ctx context.Context,
		targetGroup healthcheck.TargetGroupID,
	) ([]models.TargetStatus, error)
}

func NewServer(targetToVshardCh Sharder, repo HealthRepository) *Server {
	return &Server{
		targetToVshardCh: targetToVshardCh,
		repo:             repo,
	}
}

type Server struct {
	hcpbv1.HealthCheckServiceServer

	targetToVshardCh Sharder
	repo             HealthRepository
}
