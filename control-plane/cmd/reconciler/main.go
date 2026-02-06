package main

import (
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/Sh00ty/network-lb/control-plane/internal/api/apiruntime"
	"github.com/Sh00ty/network-lb/control-plane/internal/etcd"
	"github.com/Sh00ty/network-lb/control-plane/internal/reconciler"
)

func main() {
	ctx := context.Background()

	reconcileRepo, err := etcd.NewReconcilerClient(ctx, []string{"localhost:2379"}, "node-1")
	if err != nil {
		panic(err)
	}

	recon := reconciler.NewReconciler(reconcileRepo)
	_ = recon

	isLeader, lostLeadership, err := reconcileRepo.BecomeLeader(ctx)
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

	apiRuntime := apiruntime.NewApiRuntime(time.Second, reconcileRepo)

	epwatcher := etcd.NewWatcher(
		"/nlb-registry/target-groups/endpoints/changelog",
		etcd.NewEtcdEndpointChangelogHandler(apiRuntime).Handle,
		reconcileRepo,
		0,
	)
	go func() {

		err = epwatcher.WatchEventlog(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			panic(err)
		}
	}()

	tgwatcher := etcd.NewWatcher(
		"/nlb-registry/target-groups/spec/desired/latest",
		etcd.NewEtcdSpecChangelogHandler(apiRuntime).Handle,
		reconcileRepo,
		0,
	)
	go func() {

		err = tgwatcher.WatchEventlog(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			panic(err)
		}
	}()

	log.Info().Msg("done all operations")
	<-ctx.Done()
}
