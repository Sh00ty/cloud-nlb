package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/vrischmann/envconfig"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/api/apirpc"
	"github.com/Sh00ty/cloud-nlb/control-plane/internal/api/apiruntime"
	"github.com/Sh00ty/cloud-nlb/control-plane/internal/etcd"
	"github.com/Sh00ty/cloud-nlb/control-plane/pkg/protobuf/api/proto/cplpbv1"
	"github.com/rs/zerolog"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type Config struct {
	DataPlaneSubscribeGcInterval time.Duration `envconfig:"DATA_PLANE_SUBSCRIBE_GC_INTERVAL"`
	LoggerLevel                  string        `envconfig:"LOGGER_LEVEL"`
	EtcdRuntimeMaxConcurrency    int64         `envconfig:"ETCD_RUNTIME_MAC_CONCURRENCY"`
	EtcdEndpoint                 string        `envconfig:"ETCD_ENDPOINT"`
	ServerAddr                   string        `envconfig:"GRPC_SERVER_ADDR"`
	ServerPort                   uint16        `envconfig:"GRPC_SERVER_PORT"`
	IsDebug                      bool          `envconfig:"DEBUG"`
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

	etcdClnt, err := etcd.NewApiClient(ctx, appCfg.EtcdEndpoint)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create etcd client")
	}
	// TODO: runtime have to periodically validate cache: check spec version +
	// fetch changelog version + fetch last changelog event, it its differs with local
	// cache we need fetch only changed
	apiRuntime := apiruntime.NewApiRuntime(
		etcdClnt,
		appCfg.DataPlaneSubscribeGcInterval,
		appCfg.EtcdRuntimeMaxConcurrency,
	)
	go apiRuntime.RunGarbageCollection(ctx)

	placements, placementWatcher, err := etcd.GetDataPlanesPlacements(
		etcdClnt.Client(),
		ctx,
		etcd.NewEtcdPlacementChangeHandler(apiRuntime).Handle,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to fetch current data-plane placements")
	}
	// TODO: fetch all from etcd and then pass rev in watchers
	endpointWatcher := etcd.NewWatcher(
		etcd.EndpointsLogFolder(),
		etcd.NewEtcdEndpointChangelogHandler(apiRuntime).Handle,
		etcdClnt,
		0,
	)
	targetGroupWatcher := etcd.NewWatcher(
		etcd.TgSpecDesiredLatestFolder(),
		etcd.NewEtcdSpecChangelogHandler(apiRuntime).Handle,
		etcdClnt,
		0,
	)
	apiRuntime.Init(placements)

	go func() {
		err := placementWatcher.WatchEventlog(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Fatal().Err(err).Msg("failed to run placement watcher")
		}
	}()
	go func() {
		err = endpointWatcher.WatchEventlog(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Fatal().Err(err).Msg("failed to run endpoints watcher")
		}
	}()
	go func() {
		err = targetGroupWatcher.WatchEventlog(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Fatal().Err(err).Msg("failed to run target groups spec watcher")
		}
	}()

	apiSrv := apirpc.NewServer(apiRuntime)
	srv := grpc.NewServer()
	cplpbv1.RegisterControlPlaneServiceServer(srv, apiSrv)

	if appCfg.IsDebug {
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
	srv.GracefulStop()
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
