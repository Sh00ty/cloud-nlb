package apirpc

import (
	"context"
	"time"

	"github.com/Sh00ty/network-lb/control-plane/internal/api/apiruntime"
	"github.com/Sh00ty/network-lb/control-plane/internal/models"
	"github.com/Sh00ty/network-lb/control-plane/pkg/protobuf/api/proto/cplpbv1"
)

type Runtime interface {
	HandleDataPlaneRequest(
		ctx context.Context,
		req apiruntime.DataPlaneRequest,
	) (apiruntime.DataPlaneResponse, error)
	GetNotifier(nodeID models.DataPlaneID, deadline time.Time) apiruntime.Notifier
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
