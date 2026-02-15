package hcserver

import (
	"context"
	"net"

	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/healthcheck"
	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/protobuf/api/proto/hcpbv1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (srv *Server) RemoveEndpoints(ctx context.Context, req *hcpbv1.RemoveEndpointsRequest) (*hcpbv1.RemoveEndpointsResponse, error) {
	if req.Endpoints == nil {
		return &hcpbv1.RemoveEndpointsResponse{RemovedCount: 0}, nil
	}
	targets := make([]healthcheck.Target, 0, len(req.Endpoints))
	for _, reqEp := range req.Endpoints {
		targets = append(targets, healthcheck.Target{
			TargetGroup: healthcheck.TargetGroupID(req.TargetGroup),
			RealIP:      net.ParseIP(reqEp.RealIp),
			Port:        uint16(reqEp.Port),
		})
	}
	removed, err := srv.repo.RemoveTargets(ctx, targets)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to remove targets: %v", err)
	}
	return &hcpbv1.RemoveEndpointsResponse{RemovedCount: uint32(removed)}, nil
}
