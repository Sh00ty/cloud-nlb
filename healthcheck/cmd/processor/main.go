package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/vrischmann/envconfig"

	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/consistent"
	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/coordinator"
	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/coordinator/repository/postgres"
	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/coordinator/targetwatcher"
	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/executor"
	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/memberlist"
	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/models"
	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/notifyer"
	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/scheduler"
	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/sender"
	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/sharder"
)

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

type Config struct {
	NodeID      string `envconfig:"HC_WORKER_ID"`
	LoggerLevel string `envconfig:"LOGGER_LEVEL"`

	DatabaseHost     string `envconfig:"DATABASE_HOST"`
	DatabaseUser     string `envconfig:"DATABASE_USER"`
	DatabasePassword string `envconfig:"DATABASE_PASSWORD"`
	DatabasePort     uint16 `envconfig:"DATABASE_PORT"`

	QueueAddr  string `envconfig:"QUEUE_ADDR"`
	QueueTopic string `envconfig:"QUEUE_TARGET_UPDATES_TOPIC"`

	InitialNodeSyncTimeout time.Duration `envconfig:"INITIAL_NODE_SYNC_TIMEOUT"`
	NodeAddrsMask          string        `envconfig:"NODE_ADDR_MASK"`
	NodesCount             int           `envconfig:"HC_TOTAL_NODES"`
	VShardCount            int           `envconfig:"HC_VIRTUAL_SHARDS"`
	HcReplicationFactor    uint16        `envconfig:"HC_REPLICATION_FACTOR"`

	ExecutorConcurrency  uint16        `envconfig:"EXECUTOR_CONCURRENCY"`
	ExecutorBuffer       uint32        `envconfig:"EXECUTOR_BUFFER"`
	ResendStatusInterval time.Duration `envconfig:"RESEND_STATUS_INTERVAL"`
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	appCfg := Config{}
	err := envconfig.Init(&appCfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to read app config")
	}
	log.Logger = log.Level(loggerLevelFromString(appCfg.LoggerLevel))

	log.Warn().Msgf("running node %s", appCfg.NodeID)

	checksRepo, err := postgres.NewRepo(
		ctx,
		appCfg.DatabaseUser,
		appCfg.DatabasePassword,
		appCfg.DatabaseHost,
		appCfg.DatabasePort,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to init yugabyte database repository")
	}

	var seedNodes []string
	for nodeOrderedID := range appCfg.NodesCount {
		seedNodes = append(seedNodes, fmt.Sprintf(appCfg.NodeAddrsMask, nodeOrderedID))
	}
	memberListCfg := memberlist.Config{
		SeedNodes: seedNodes,
	}
	err = envconfig.Init(&memberListCfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to read memberlist config")
	}

	membershipEventsChan := make(chan models.MemberShipEvent, 256)
	memberList, err := memberlist.New(ctx, memberListCfg, membershipEventsChan)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to init memberlist: can't choose vshards")
	}

	// nodesCh = consistent.New(
	// 	2*int(maxNodesCount),
	// 	consistent.WithNewInitialState(consistent.Unhealthy),
	// 	consistent.WithSearchLimit(bigSearchLimmit),
	// )
	// vshardCh = consistent.New(
	// 	int(vshardCount),
	// 	consistent.WithSearchLimit(bigSearchLimmit),
	// )
	nodesCh := consistent.NewCirle()
	vshardCh := consistent.NewCirle()

	sharder, err := sharder.NewConsistentSharder(
		nodesCh,
		vshardCh,
		uint64(appCfg.VShardCount),
		appCfg.HcReplicationFactor,
		models.NodeID(appCfg.NodeID),
	)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create sharder")
	}

	notifyer := notifyer.NewNotifier(1024)
	sender := sender.NewSenderController(notifyer.GetEventChan(), checksRepo, appCfg.ResendStatusInterval)
	go sender.Run(ctx)

	checkExecutor := executor.NewExecutor(
		notifyer,
		appCfg.ExecutorConcurrency,
		appCfg.ExecutorBuffer,
	)
	go checkExecutor.Run()

	scheduler := scheduler.New(
		nil,
		checkExecutor,
	)
	defer checkExecutor.Close()

	go scheduler.Run(ctx)

	cord, err := coordinator.NewCoordinator(
		ctx,
		checksRepo,
		membershipEventsChan,
		scheduler,
		sharder,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create coordinator")
	}
	err = cord.FetchTargets(ctx, sharder.GetMyVshards())
	if err != nil {
		log.Fatal().Err(err).Msg("failed to init hcs")
	}
	go cord.StartHandleMembershipChanges(ctx)

	log.Info().Msg("start listening for memberhip changes")
	w := targetwatcher.NewCheckUpdateWatcher(
		ctx,
		appCfg.NodeID,
		appCfg.QueueAddr,
		appCfg.QueueTopic,
		cord,
	)
	go func() {
		err = w.RunTargetWatcher(ctx)
		if err != nil && !errors.Is(err, io.EOF) {
			log.Fatal().Err(err).Msg("failed to consume message")
		}
	}()
	select {
	case <-ctx.Done():
	case <-time.After(appCfg.InitialNodeSyncTimeout):
		err := memberList.Join(ctx)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to join gossip cluster")
		}
		log.Info().Msg("successfully joined gossip cluster")
	}

	serverClose := startProbeServer()
	defer serverClose()

	<-ctx.Done()
	memberList.GracefullyClose(time.Second)
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
