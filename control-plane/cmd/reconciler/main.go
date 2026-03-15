package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/vrischmann/envconfig"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/etcd"
	"github.com/Sh00ty/cloud-nlb/control-plane/internal/reconciler"
)

type Config struct {
	PodID                        string        `envconfig:"POD_ID"`
	LoggerLevel                  string        `envconfig:"LOGGER_LEVEL"`
	EtcdEndpoint                 string        `envconfig:"ETCD_ENDPOINT"`
	TargetGroupReplicationFactor uint8         `envconfig:"TARGET_GROUP_REPLICATION_FACTOR"`
	DataPlaneDeathEventDelay     time.Duration `envconfig:"DATA_PLANE_NODE_DEATH_EVENT_DELAY"`
	ForceReconcileInterval       time.Duration `envconfig:"FORCE_RECONCILE_INTERVAL"`
	NeedProbes                   bool          `envconfig:"NEED_PROBES"`
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
	go startMetrics()

	reconcileRepo, err := etcd.NewReconcilerClient(ctx, []string{appCfg.EtcdEndpoint}, appCfg.PodID)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create reconciliation etcd repository")
	}

	go startProbeServer()

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

		reconcilerSvc := reconciler.NewReconciler(
			reconcileRepo,
			int(appCfg.TargetGroupReplicationFactor),
			appCfg.DataPlaneDeathEventDelay,
			appCfg.ForceReconcileInterval,
			log.Logger,
		)

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

func startProbeServer() func() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		w.WriteHeader(http.StatusOK)
	})
	srv := http.Server{
		Handler: mux,
		Addr:    "0.0.0.0:8080",
	}
	go func() {
		err := srv.ListenAndServe()
		if err != nil {
			log.Fatal().Err(err).Msg("failed to start http server")
		}
	}()
	return func() {
		_ = srv.Close()
	}
}

func startMetrics() {
	var (
		addr = os.Getenv("RECONCILER_METRICS_ADDR")
		mux  = http.NewServeMux()
	)
	if addr == "" {
		return
	}
	mux.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(addr, mux)
}
