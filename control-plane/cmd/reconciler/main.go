package main

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/Sh00ty/network-lb/control-plane/internal/etcd"
	"github.com/Sh00ty/network-lb/control-plane/internal/models"
	"github.com/Sh00ty/network-lb/control-plane/internal/reconciler"
)

func main() {
	ctx := context.Background()

	evChan := make(chan *models.Event)
	reconcileRepo, err := etcd.NewReconcilerClient(ctx, "localhost:2379", "node-1", evChan)
	if err != nil {
		panic(err)
	}
	events, err := reconcileRepo.GetAllPendingOperations(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Println(events)

	recon := reconciler.NewReconciler(reconcileRepo, evChan)

	isLeader, lostLeadership, err := reconcileRepo.BecomeLeaderReconciller(ctx)
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
					log.Warn().Msg("lost leadership")
				}
			}
		}()
	}
	go recon.RunEventWatcher(ctx)

	err = reconcileRepo.WatchEventlog(ctx, "/nlb-registry/eventlog/pending-status", reconcileRepo.EventlogWatchHandler, 0)
	if err != nil {
		panic(err)
	}

	log.Info().Msg("done all operations")
	<-ctx.Done()
}
