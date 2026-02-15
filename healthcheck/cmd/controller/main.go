package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"

	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/consistent"
	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/coordinator/repository/postgres"
	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/hcserver"
	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/sharder"
	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/protobuf/api/proto/hcpbv1"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/vrischmann/envconfig"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
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
	LoggerLevel string `envconfig:"LOGGER_LEVEL"`

	DatabaseHost     string `envconfig:"DATABASE_HOST"`
	DatabaseUser     string `envconfig:"DATABASE_USER"`
	DatabasePassword string `envconfig:"DATABASE_PASSWORD"`
	DatabasePort     uint16 `envconfig:"DATABASE_PORT"`
	ServerAddr       string `envconfig:"GRPC_SERVER_ADDR"`
	ServerPort       uint16 `envconfig:"GRPC_SERVER_PORT"`
	GrpcDebug        bool   `envconfig:"GRPC_DEBUG"`
	VShardCount      int    `envconfig:"HC_VIRTUAL_SHARDS"`
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
		0,
		"hc-worker-0",
	)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create sharder")
	}

	hcSrv := hcserver.NewServer(sharder, checksRepo)
	srv := grpc.NewServer()
	hcpbv1.RegisterHealthCheckServiceServer(srv, hcSrv)

	if appCfg.GrpcDebug {
		reflection.Register(srv)
	}

	go func() {
		listenAddr := fmt.Sprintf("%s:%d", appCfg.ServerAddr, appCfg.ServerPort)

		log.Info().Msgf("running grpc server on %s", listenAddr)

		ls, err := net.Listen("tcp4", listenAddr)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to bind server addr")
		}
		err = srv.Serve(ls)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to start serving grpc requests")
		}
	}()

	serverClose := startProbeServer()
	defer serverClose()

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
