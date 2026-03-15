package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	controlplane "github.com/Sh00ty/cloud-nlb/nlb-agent/internal/control-plane"
	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/coordinator/etcd"
	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/endpointshc"
	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/endpointshc/hcsrv"
	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/endpointshc/statecache"
	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/endpointshc/watcher/kafka"
	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/models"
	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/reconciler"
	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/scheduler"
	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/storage/persistent"
	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/vpp/stubvpp"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/vrischmann/envconfig"
)

type Config struct {
	NodeID        string        `envconfig:"NODE_ID"`
	TTLUntilDeath time.Duration `envconfig:"TTL_UNTIL_NODE_DEATH"`

	EtcdEndpoint string `envconfig:"ETCD_ENDPOINT"`

	PersistentStoragePathTemplate string `envconfig:"PERSISTENT_STORAGE_PATH_TEMPLATE"`

	MaxReconcileAttempts   uint8         `envconfig:"MAX_RECONCILE_ATTEMPTS"`
	ForceReconcileInterval time.Duration `envconfig:"FORCE_RECONCILE_INTERVAL"`
	ReconcilerConcurrency  uint8         `envconfig:"RECONCILER_CONCURRENCY"`

	ControlPlaneAddr             string        `envconfig:"CONTROL_PLANE_ADDR"`
	ControlPlaneLongPollDuration time.Duration `envconfig:"CONTROL_PLANE_LONG_POLL_DURATION"`

	EndpointStatusesServiceAddr    string        `envconfig:"ENDPOINTS_SERVICE_ADDR"`
	EndpointStatusesResyncInterval time.Duration `envconfig:"ENDPOINT_STATUSES_RESYNC_INTERVAL"`

	QueueAddr  string `envconfig:"QUEUE_ADDR"`
	QueueTopic string `envconfig:"QUEUE_ENDPOINT_STATUSES_TOPIC"`

	IsDebug bool `envconfig:"DEBUG"`

	LoggerLevel     string `envconfig:"LOGGER_LEVEL"`
	LoggerUsePretty bool   `envconfig:"LOGGER_USE_PRETTY"`
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
	if appCfg.LoggerUsePretty {
		log.Logger = log.Logger.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}
	go startMetrics()

	nodeID := appCfg.NodeID
	if len(os.Args) > 1 {
		nodeID = os.Args[1]
	}
	log.Logger = log.Logger.With().Str("node_id", nodeID).Logger()

	log.Warn().Msg("starting data-plane node")

	coord, err := etcd.NewClient(
		ctx,
		appCfg.EtcdEndpoint,
		nodeID,
		uint8(appCfg.TTLUntilDeath.Seconds()),
	)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to init etcd client")
	}
	err = coord.Register(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to register data-plane in coordinator")
	}
	defer coord.Close(ctx)

	cpl, err := controlplane.NewClient(
		appCfg.ControlPlaneAddr,
		appCfg.ControlPlaneLongPollDuration,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create control-plane client")
	}

	hcSrvClient, err := hcsrv.NewClient(appCfg.EndpointStatusesServiceAddr)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create endpoint statuses client")
	}

	storage, err := persistent.New(
		fmt.Sprintf(appCfg.PersistentStoragePathTemplate, nodeID),
		log.Logger,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to init persistent spec storage")
	}
	defer storage.Close()

	vpp := stubvpp.NewStubVPP(log.Logger)
	reconcileTaskChan := make(chan []models.TargetGroupID, 128)

	endpointsSvc := endpointshc.NewEndpointHealthService(
		hcSrvClient,
		appCfg.EndpointStatusesResyncInterval,
		reconcileTaskChan,
		log.Logger,
	)

	rec := reconciler.New(
		reconcileTaskChan,
		storage,
		endpointsSvc,
		statecache.New(),
		appCfg.MaxReconcileAttempts,
		appCfg.ForceReconcileInterval,
		appCfg.ReconcilerConcurrency,
		vpp,
		&log.Logger,
	)
	sched := scheduler.NewScheduler(nodeID, cpl, rec, storage, log.Logger)

	endpointsSvc.SyncStatuses(ctx, storage.GetAllTargetGroupIDs())

	epStatusWatcher := kafka.NewStatusesWatcher(
		ctx,
		nodeID,
		appCfg.QueueAddr,
		appCfg.QueueTopic,
		endpointsSvc,
		log.Logger,
	)
	defer epStatusWatcher.Close(ctx)

	go rec.Run(ctx)
	go func() {
		err := epStatusWatcher.RunEndpointStatusesWatcher(ctx)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to run endpoint status watcher")
		}
	}()
	go endpointsSvc.Run(ctx)
	go sched.Run(ctx)

	go startProbeServer()

	log.Info().Msg("agent started")
	<-ctx.Done()
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
		addr = os.Getenv("METRICS_ADDR")
		mux  = http.NewServeMux()
	)
	if addr == "" {
		return
	}
	mux.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(addr, mux)
}
