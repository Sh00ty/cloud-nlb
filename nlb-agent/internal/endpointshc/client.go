package endpointshc

import (
	"fmt"

	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/protobuf/api/proto/hcpbv1"
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
