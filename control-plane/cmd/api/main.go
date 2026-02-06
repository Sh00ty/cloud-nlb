package main

import (
	"context"
	"net"
	"time"

	"github.com/Sh00ty/network-lb/control-plane/internal/api/apirpc"
	"github.com/Sh00ty/network-lb/control-plane/internal/api/apiruntime"
	"github.com/Sh00ty/network-lb/control-plane/internal/etcd"
	"github.com/Sh00ty/network-lb/control-plane/pkg/protobuf/api/proto/cplpbv1"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
)

func main() {
	ctx := context.Background()
	clnt, err := etcd.NewApiClient(ctx, "localhost:2379")
	if err != nil {
		panic(err)
	}

	rt := apiruntime.NewApiRuntime(time.Second, clnt)
	apiSrv := apirpc.NewServer(rt)
	srv := grpc.NewServer()
	cplpbv1.RegisterControlPlaneServiceServer(srv, apiSrv)

	go func() {
		ls, err := net.Listen("tcp4", "127.0.0.1:9090")
		if err != nil {
			log.Fatal().Err(err).Msg("failed to bind server addr")
		}
		err = srv.Serve(ls)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to start serving grpc requests")
		}
	}()
	<-ctx.Done()
	srv.GracefulStop()
}
