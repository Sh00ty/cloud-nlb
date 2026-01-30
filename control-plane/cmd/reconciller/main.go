package main

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/Sh00ty/network-lb/control-plane/internal/etcd"
	"github.com/Sh00ty/network-lb/control-plane/internal/models"
	"github.com/Sh00ty/network-lb/control-plane/internal/reconciller"
)

func main() {
	ctx := context.Background()

	evChan := make(chan *models.Event)
	recontileRepo, err := etcd.NewReconcillerClient(ctx, "localhost:2379", "node-1", evChan)
	if err != nil {
		panic(err)
	}
	recon := reconciller.NewReconciller(recontileRepo, evChan)

	isLeader, lostLeadership, err := recontileRepo.BecomeLeaderReconciller(ctx)
	if err != nil {
		panic(err)
	}
	if !isLeader {
		log.Info().Msg("not a leader")
	} else {
		go func() {
			for {
				select {
				case <-ctx.Done():
					log.Info().Msg("go off as a leader")
				case <-lostLeadership:
					log.Warn().Msg("lost leaderhip")
				}
			}
		}()
	}
	err = recon.RunEventWatcher(ctx)
	if err != nil {
		panic(err)
	}

	err = recontileRepo.WatchEventlog(ctx, "/nlb-registry/eventlog/pending-status", recontileRepo.EventlogWatchHandler, 0)
	if err != nil {
		panic(err)
	}

	log.Info().Msg("done all operations")
	<-ctx.Done()
}
