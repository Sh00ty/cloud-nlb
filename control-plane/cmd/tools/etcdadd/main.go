package main

import (
	"context"
	"net"

	"github.com/Sh00ty/network-lb/control-plane/internal/etcd"
	"github.com/Sh00ty/network-lb/control-plane/internal/models"
)

func main() {
	ctx := context.Background()
	clnt, err := etcd.NewApiClient(ctx, "localhost:2379")
	if err != nil {
		panic(err)
	}
	err = clnt.SetTargetGroupSpec(ctx, models.TargetGroupSpec{
		ID:        "mws-k8s-kubeproxy",
		Proto:     models.TCP,
		Port:      8080,
		VirtualIP: net.IPv4(175, 10, 22, 2),
	})
	if err != nil {
		panic(err)
	}

	clnt.AddEndpoint(ctx, "mws-k8s-kubeproxy", models.EndpointSpec{
		IP:     net.IPv4(10, 10, 10, 10),
		Port:   9090,
		Weight: 100,
	})

}
