package apirpc

import (
	"context"
	"time"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/api/apiruntime"
	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
	"github.com/Sh00ty/cloud-nlb/control-plane/pkg/protobuf/api/proto/cplpbv1"
)

type Runtime interface {
	GetChangesForDataPlane(
		ctx context.Context,
		curDplState apiruntime.DataPlaneCurrentState,
	) (apiruntime.DataPlaneChanges, error)
	GetNotifier(nodeID models.DataPlaneID, deadline time.Time) apiruntime.Notifier

	UpsertEndpoint(ctx context.Context, epEvent models.EndpointEvent) error
	UpsertTargetGroupSpec(ctx context.Context, tgSpec models.TargetGroupSpec) error
}

type Server struct {
	cplpbv1.ControlPlaneServiceServer
	runtime Runtime
}

func NewServer(runtime Runtime) *Server {
	return &Server{
		runtime: runtime,
	}
}
