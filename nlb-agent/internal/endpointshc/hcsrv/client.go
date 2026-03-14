package hcsrv

import (
	"context"
	"fmt"
	"net"

	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/protobuf/api/proto/hcpbv1"
	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/models"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	clnt hcpbv1.HealthCheckServiceClient
}

func NewClient(hcSrvAddr string) (*Client, error) {
	conn, err := grpc.NewClient(hcSrvAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("creating grpc connection: %w", err)
	}
	return &Client{
		clnt: hcpbv1.NewHealthCheckServiceClient(conn),
	}, nil
}

func (c *Client) GetEndpointStatuses(ctx context.Context, targetGroup models.TargetGroupID) ([]models.EndpointStatus, error) {
	resp, err := c.clnt.GetEndpointStatuses(ctx, &hcpbv1.GetEndpointStatusesRequest{
		TargetGroup: string(targetGroup),
	})
	if err != nil {
		return nil, fmt.Errorf("getting endpoint statuses from healthcheck service: %w", err)
	}
	result := make([]models.EndpointStatus, 0, len(resp.Statuses))
	for _, stat := range resp.Statuses {
		result = append(result, models.EndpointStatus{
			Header: models.EndpointHdr{
				TargetGroupID: targetGroup,
				IP:            net.ParseIP(stat.RealIp),
				Port:          uint16(stat.Port),
			},
			UpdatedAt: stat.UpdatedAt.AsTime(),
			Healthy:   stat.IsHealthy,
		})
	}
	return result, nil
}
