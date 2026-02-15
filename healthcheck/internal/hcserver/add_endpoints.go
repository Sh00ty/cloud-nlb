package hcserver

import (
	"context"
	"net"
	"net/netip"
	"strconv"

	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/healthcheck"
	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/protobuf/api/proto/hcpbv1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (srv *Server) AddEndpoints(ctx context.Context, req *hcpbv1.AddEndpointsRequest) (*hcpbv1.AddEndpointsResponse, error) {
	var (
		targets = make([]healthcheck.Target, 0, len(req.Endpoints))
		vshards = make([]string, 0, len(req.Endpoints))
	)
	for _, reqTarget := range req.Endpoints {
		addr, err := netip.ParseAddr(reqTarget.RealIp)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "failed to parse addr %s: %v", reqTarget.RealIp, err)
		}
		if !addr.Is4() {
			return nil, status.Errorf(codes.InvalidArgument, "addr %s is not ip v4: %v", reqTarget.RealIp, err)
		}
		if reqTarget.Port < 1 && reqTarget.Port > 1<<16 {
			return nil, status.Errorf(codes.InvalidArgument, "invalid port %d value, should be in (0, 2^16]", reqTarget.Port)
		}
		target := healthcheck.Target{
			TargetGroup: healthcheck.TargetGroupID(req.TargetGroup),
			RealIP:      net.ParseIP(reqTarget.RealIp),
			Port:        uint16(reqTarget.Port),
		}
		targets = append(targets, target)
		vshard := srv.targetToVshardCh.GetTargetVshard(target.ToAddr().String())
		vshards = append(vshards, strconv.Itoa(int(vshard)))
	}
	created, err := srv.repo.CreateTargets(ctx, targets, vshards)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create targets: %v", err)
	}
	return &hcpbv1.AddEndpointsResponse{AddedCount: uint32(created)}, nil
}
