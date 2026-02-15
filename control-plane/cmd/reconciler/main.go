package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/vrischmann/envconfig"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/etcd"
	"github.com/Sh00ty/cloud-nlb/control-plane/internal/reconciler"
)

type Config struct {
	PodID        string `envconfig:"POD_ID"`
	LoggerLevel  string `envconfig:"LOGGER_LEVEL"`
	EtcdEndpoint string `envconfig:"ETCD_ENDPOINT"`
}

func loggerLevelFromString(level string) zerolog.Level {
	level = strings.ToLower(level)
	switch level {
	case "error":
		return zerolog.ErrorLevel
	case "warn":
		return zerolog.WarnLevel
	case "info":
		return zerolog.InfoLevel
	case "debug":
		return zerolog.DebugLevel
	}
	return zerolog.WarnLevel
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	godotenv.Load()

	appCfg := Config{}
	err := envconfig.Init(&appCfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to read app config from envs")
	}

	log.Logger = log.Level(loggerLevelFromString(appCfg.LoggerLevel))

	reconcileRepo, err := etcd.NewReconcilerClient(ctx, []string{appCfg.EtcdEndpoint}, appCfg.PodID)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create reconciliation etcd repository")
	}

	leaderCtx, cancelLeader := context.WithCancel(ctx)
	defer cancelLeader()
	for {
		isLeader, lostLeadership, err := reconcileRepo.BecomeLeader(ctx)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to determine leadership")
		}
		if !isLeader {
			log.Warn().Msg("not a leader, will wait")
			continue
		}

		reconcilerSvc := reconciler.NewReconciler(reconcileRepo, 2, 15*time.Second, 5*time.Minute, log.Logger)

		dplStateWatchHandler := etcd.NewDataPlaneStateChangeHandler(reconcilerSvc.GetEventsChan())
		tgCreationWatchHandler := etcd.NewTargetGroupCreationHandler(reconcilerSvc.GetEventsChan())

		states, dplStateWatcher, err := reconcileRepo.DataPlaneStatusesInitialSync(leaderCtx, dplStateWatchHandler.Handle)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to make data-plane statuses initial sync")
		}
		targetGroups, tgWatcher, err := reconcileRepo.TargetGroupsInitialSync(leaderCtx, tgCreationWatchHandler.Handle)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to make target groups initial sync")
		}
		placements, _, err := etcd.GetDataPlanesPlacements(reconcileRepo.Client(), leaderCtx, nil)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to get current data-plane placements")
		}

		reconcilerSvc.Init(placements, states, targetGroups)
		go reconcilerSvc.RunReconciler(leaderCtx)
		go func() {
			err := dplStateWatcher.WatchEventlog(leaderCtx)
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Fatal().Err(err).Msg("failed to run data-plane state watcher")
			}
		}()
		go func() {
			err := tgWatcher.WatchEventlog(leaderCtx)
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Fatal().Err(err).Msg("failed to run target groups watcher")
			}
		}()

		select {
		case <-ctx.Done():
			log.Info().Msg("shutdown")
			return
		case <-lostLeadership:
			log.Warn().Msg("lost leadership, will wait until i will be elected")
			cancelLeader()
			leaderCtx, cancelLeader = context.WithCancel(ctx)
		}
	}
}
