package main

import (
	"context"
	"os"
	"time"

	controlplane "github.com/Sh00ty/cloud-nlb/nlb-agent/internal/control-plane"
	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/coordinator/etcd"
	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/reconciler"
	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/scheduler"
	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/storage/inmemory"
	"github.com/rs/zerolog/log"
)

var nodeID = "dataplane-node-001"

func main() {
	ctx := context.Background()

	if len(os.Args) > 1 {
		nodeID = os.Args[1]
	}
	log.Warn().Msgf("run data-plane with node-id: %s", nodeID)

	coord, err := etcd.NewClient(ctx, "localhost:2379", nodeID, 5)
	if err != nil {
		panic(err)
	}
	err = coord.Register(ctx)
	if err != nil {
		panic(err)
	}
	defer coord.Close(ctx)

	cpl, err := controlplane.NewClient("localhost:9091", 10*time.Second)
	if err != nil {
		panic(err)
	}
	state := inmemory.NewInMemoryState()
	rec := reconciler.New(state)
	sched := scheduler.NewScheduler(nodeID, cpl, rec, state)
	go sched.Run(ctx)

	log.Info().Msg("start scheduler")
	<-ctx.Done()
}
