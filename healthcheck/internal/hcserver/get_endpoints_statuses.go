package hcserver

import (
	"context"

	"github.com/Sh00ty/cloud-nlb/healthcheck/pkg/healthcheck"
	"github.com/Sh00ty/cloud-nlb/healthcheck/pkg/protobuf/api/proto/hcpbv1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (srv *Server) GetEndpointStatuses(
	ctx context.Context,
	req *hcpbv1.GetEndpointStatusesRequest,
) (*hcpbv1.GetEndpointStatusesResponse, error) {
	statuses, err := srv.repo.GetTargetStatuses(ctx, healthcheck.TargetGroupID(req.TargetGroup))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get endpoint statuses: %v", err)
	}
	pbStatuses := make([]*hcpbv1.EndpointStatus, 0, len(statuses))
	for _, epStat := range statuses {
		pbStatuses = append(pbStatuses, &hcpbv1.EndpointStatus{
			RealIp:      epStat.Target.RealIP.String(),
			Port:        uint32(epStat.Target.Port),
			TargetGroup: string(epStat.TargetGroup),
			IsHealthy:   epStat.Status,
			LastError:   epStat.Error,
			UpdatedAt:   timestamppb.New(epStat.UpdatedAt),
		})
	}
	return &hcpbv1.GetEndpointStatusesResponse{Statuses: pbStatuses}, nil
}
