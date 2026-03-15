package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	cplpb "github.com/Sh00ty/cloud-nlb/control-plane/pkg/protobuf/api/proto/cplpbv1"
	hcpb "github.com/Sh00ty/cloud-nlb/healthcheck/pkg/protobuf/api/proto/hcpbv1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type config struct {
	kubeconfig    string
	namespace     string
	labelSelector string

	cplAddr string
	hcAddr  string

	targetGroup string
	tgPort      uint
	tgProtocol  string
	tgVIP       string

	hcPort uint

	dryRun bool
}

func parseFlags() config {
	c := config{}

	flag.StringVar(&c.kubeconfig, "kubeconfig", "", "path to kubeconfig (empty = in-cluster)")
	flag.StringVar(&c.namespace, "namespace", "cloud-nlb", "k8s namespace")
	flag.StringVar(&c.labelSelector, "selector", "app=test-server", "pod label selector")

	flag.StringVar(&c.cplAddr, "cpl-addr", "control-plane-apiserver.local:30080", "control-plane gRPC address")
	flag.StringVar(&c.hcAddr, "hc-addr", "hc-server.local:30080", "health-check gRPC address")

	flag.StringVar(&c.targetGroup, "target-group", "test-server", "target group name")
	flag.UintVar(&c.tgPort, "tg-port", 8081, "target group port (traffic port)")
	flag.StringVar(&c.tgProtocol, "tg-protocol", "TCP", "protocol: TCP or UDP")
	flag.StringVar(&c.tgVIP, "tg-vip", "10.96.0.100", "virtual IP for target group")

	flag.UintVar(&c.hcPort, "hc-port", 8090, "health-check port on pods")

	flag.BoolVar(&c.dryRun, "dry-run", false, "only print diff, don't apply")

	flag.Parse()
	return c
}

// ---------- k8s ----------

func buildKubeClient(kubeconfigPath string) (kubernetes.Interface, error) {
	var cfg *rest.Config
	var err error

	if kubeconfigPath != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	} else {
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("kube config: %w", err)
	}
	return kubernetes.NewForConfig(cfg)
}

type endpointKey = string

func epKey(ip string, port uint32) endpointKey {
	return fmt.Sprintf("%s:%d", ip, port)
}

func fetchK8sPodIPs(ctx context.Context, client kubernetes.Interface, ns, selector string) (map[string]struct{}, error) {
	pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}

	result := make(map[string]struct{}, len(pods.Items))
	for _, pod := range pods.Items {
		if pod.Status.PodIP == "" {
			continue
		}
		ready := false
		for _, cond := range pod.Status.Conditions {
			if cond.Type == "Ready" && cond.Status == "True" {
				ready = true
				break
			}
		}
		if !ready {
			continue
		}
		result[pod.Status.PodIP] = struct{}{}
	}
	return result, nil
}

// ---------- gRPC helpers ----------

func dialGRPC(ctx context.Context, addr string) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	//nolint:staticcheck
	return grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}

func mapProtocol(s string) cplpb.Protocol {
	switch s {
	case "UDP":
		return cplpb.Protocol_PROTOCOL_UDP
	default:
		return cplpb.Protocol_PROTOCOL_TCP
	}
}

func ensureTargetGroup(ctx context.Context, cpl cplpb.ControlPlaneServiceClient, cfg config) error {
	resp, err := cpl.UpsertTargetGroupSpec(ctx, &cplpb.UpsertTargetGroupSpecRequest{
		Id: &cplpb.TargetGroupID{Value: cfg.targetGroup},
		Spec: &cplpb.TargetGroupSpec{
			Protocol:  mapProtocol(cfg.tgProtocol),
			Port:      uint32(cfg.tgPort),
			VirtualIp: cfg.tgVIP,
		},
		IdempotencyKey: fmt.Sprintf("sync-tg-%s", cfg.targetGroup),
	})
	if err != nil {
		return fmt.Errorf("upsert target group spec: %w", err)
	}
	slog.Info("cpl: target group ensured",
		"id", resp.GetId().GetValue(),
		"version", resp.GetVersion(),
		"created", resp.GetCreated(),
	)
	return nil
}

func syncCPLEndpoints(
	ctx context.Context,
	cpl cplpb.ControlPlaneServiceClient,
	cfg config,
	podIPs map[string]struct{},
	dryRun bool,
) error {
	tgID := cfg.targetGroup
	port := uint32(cfg.tgPort)

	for ip := range podIPs {
		slog.Info("cpl: ensure endpoint", "ip", ip, "port", port, "dry_run", dryRun)
		if dryRun {
			continue
		}
		_, err := cpl.UpsertEndpoint(ctx, &cplpb.UpsertEndpointRequest{
			TargetGroupId:  &cplpb.TargetGroupID{Value: tgID},
			Endpoint:       &cplpb.Endpoint{Ip: ip, Port: port, Weight: 1},
			Operation:      cplpb.EndpointOperationType_ENDPOINT_OPERATION_TYPE_ADD,
			IdempotencyKey: fmt.Sprintf("sync-add-%s-%s-%d", tgID, ip, port),
		})
		if err != nil {
			return fmt.Errorf("cpl upsert %s:%d: %w", ip, port, err)
		}
	}
	return nil
}

func ensureHCSettings(ctx context.Context, hc hcpb.HealthCheckServiceClient, cfg config) {
	strategySettings, err := structpb.NewStruct(map[string]interface{}{
		"timeout": float64(3 * time.Second),
		"path":    "/health/",
		"scheme":  "http",
		"method":  "GET",
	})
	if err != nil {
		slog.Error("hc: failed to build strategy settings", "err", err)
		return
	}

	_, err = hc.CreateSettings(ctx, &hcpb.CreateSettingsRequest{
		Settings: &hcpb.Settings{
			TargetGroup:            cfg.targetGroup,
			Interval:               durationpb.New(3 * time.Second),
			SuccessBeforePassing:   2,
			FailuresBeforeCritical: 3,
			InitialState:           true,
			Strategy:               hcpb.StrategyType_STRATEGY_TYPE_HTTP,
			StrategySettings:       strategySettings,
		},
	})
	if err != nil {
		slog.Warn("hc: create settings (may already exist)", "err", err)
	} else {
		slog.Info("hc: settings ensured", "target_group", cfg.targetGroup)
	}
}

func fetchHCEndpoints(ctx context.Context, hc hcpb.HealthCheckServiceClient, tg string) (map[endpointKey]*hcpb.Endpoint, error) {
	resp, err := hc.GetEndpointStatuses(ctx, &hcpb.GetEndpointStatusesRequest{
		TargetGroup: tg,
	})
	if err != nil {
		return nil, fmt.Errorf("hc get statuses: %w", err)
	}

	result := make(map[endpointKey]*hcpb.Endpoint, len(resp.GetStatuses()))
	for _, s := range resp.GetStatuses() {
		result[epKey(s.RealIp, s.Port)] = &hcpb.Endpoint{
			RealIp: s.RealIp,
			Port:   s.Port,
		}
	}
	return result, nil
}

func syncHCEndpoints(
	ctx context.Context,
	hc hcpb.HealthCheckServiceClient,
	cfg config,
	podIPs map[string]struct{},
	dryRun bool,
) error {
	tg := cfg.targetGroup
	hcPort := uint32(cfg.hcPort)

	// Desired: все ready pod IPs с hcPort
	desired := make(map[endpointKey]*hcpb.Endpoint, len(podIPs))
	for ip := range podIPs {
		desired[epKey(ip, hcPort)] = &hcpb.Endpoint{RealIp: ip, Port: hcPort}
	}

	// Current: то что сейчас в HC
	current, err := fetchHCEndpoints(ctx, hc, tg)
	if err != nil {
		return fmt.Errorf("fetch hc endpoints: %w", err)
	}

	slog.Info("hc: state",
		"desired", len(desired),
		"current", len(current),
	)

	// --- add missing ---
	var toAdd []*hcpb.Endpoint
	for k, ep := range desired {
		if _, ok := current[k]; ok {
			continue
		}
		slog.Info("hc: will add", "ip", ep.RealIp, "port", ep.Port, "dry_run", dryRun)
		toAdd = append(toAdd, ep)
	}
	if len(toAdd) > 0 && !dryRun {
		resp, err := hc.AddEndpoints(ctx, &hcpb.AddEndpointsRequest{
			TargetGroup: tg,
			Endpoints:   toAdd,
		})
		if err != nil {
			return fmt.Errorf("hc add endpoints: %w", err)
		}
		slog.Info("hc: added", "count", resp.GetAddedCount())
	}

	// --- remove stale ---
	var toRemove []*hcpb.Endpoint
	for k, ep := range current {
		if _, ok := desired[k]; ok {
			continue
		}
		slog.Info("hc: will remove", "ip", ep.RealIp, "port", ep.Port, "dry_run", dryRun)
		toRemove = append(toRemove, ep)
	}
	if len(toRemove) > 0 && !dryRun {
		resp, err := hc.RemoveEndpoints(ctx, &hcpb.RemoveEndpointsRequest{
			TargetGroup: tg,
			Endpoints:   toRemove,
		})
		if err != nil {
			return fmt.Errorf("hc remove endpoints: %w", err)
		}
		slog.Info("hc: removed", "count", resp.GetRemovedCount())
	}

	if len(toAdd) == 0 && len(toRemove) == 0 {
		slog.Info("hc: already in sync")
	}

	return nil
}

// Собираем IP-шники stale эндпоинтов из HC для удаления из CPL
func findStaleCPLEndpoints(
	ctx context.Context,
	hc hcpb.HealthCheckServiceClient,
	tg string,
	podIPs map[string]struct{},
) ([]string, error) {
	current, err := fetchHCEndpoints(ctx, hc, tg)
	if err != nil {
		return nil, err
	}

	var staleIPs []string
	seen := make(map[string]struct{})
	for _, ep := range current {
		if _, ok := podIPs[ep.RealIp]; ok {
			continue
		}
		if _, ok := seen[ep.RealIp]; ok {
			continue
		}
		seen[ep.RealIp] = struct{}{}
		staleIPs = append(staleIPs, ep.RealIp)
	}
	return staleIPs, nil
}

func removeStaleCPLEndpoints(
	ctx context.Context,
	cpl cplpb.ControlPlaneServiceClient,
	cfg config,
	staleIPs []string,
	dryRun bool,
) error {
	tgID := cfg.targetGroup
	port := uint32(cfg.tgPort)

	for _, ip := range staleIPs {
		slog.Info("cpl: will remove stale", "ip", ip, "port", port, "dry_run", dryRun)
		if dryRun {
			continue
		}
		_, err := cpl.UpsertEndpoint(ctx, &cplpb.UpsertEndpointRequest{
			TargetGroupId:  &cplpb.TargetGroupID{Value: tgID},
			Endpoint:       &cplpb.Endpoint{Ip: ip, Port: port},
			Operation:      cplpb.EndpointOperationType_ENDPOINT_OPERATION_TYPE_REMOVE,
			IdempotencyKey: fmt.Sprintf("sync-rm-%s-%s-%d", tgID, ip, port),
		})
		if err != nil {
			return fmt.Errorf("cpl remove %s:%d: %w", ip, port, err)
		}
	}
	return nil
}

func run(ctx context.Context) error {
	cfg := parseFlags()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	slog.Info("starting sync",
		"target_group", cfg.targetGroup,
		"namespace", cfg.namespace,
		"selector", cfg.labelSelector,
		"tg_port", cfg.tgPort,
		"hc_port", cfg.hcPort,
		"dry_run", cfg.dryRun,
	)

	// 1. K8s client
	kubeClient, err := buildKubeClient(cfg.kubeconfig)
	if err != nil {
		return fmt.Errorf("build kube client: %w", err)
	}
	slog.Info("k8s client ready")

	// 2. gRPC connections
	cplConn, err := dialGRPC(ctx, cfg.cplAddr)
	if err != nil {
		return fmt.Errorf("dial cpl %s: %w", cfg.cplAddr, err)
	}
	defer cplConn.Close()
	cplClient := cplpb.NewControlPlaneServiceClient(cplConn)
	slog.Info("cpl connected", "addr", cfg.cplAddr)

	hcConn, err := dialGRPC(ctx, cfg.hcAddr)
	if err != nil {
		return fmt.Errorf("dial hc %s: %w", cfg.hcAddr, err)
	}
	defer hcConn.Close()

	hcClient := hcpb.NewHealthCheckServiceClient(hcConn)
	slog.Info("hc connected", "addr", cfg.hcAddr)

	// 3. Ensure target group in CPL
	if !cfg.dryRun {
		if err := ensureTargetGroup(ctx, cplClient, cfg); err != nil {
			return fmt.Errorf("ensure target group: %w", err)
		}
	} else {
		slog.Info("cpl: skip ensure target group (dry-run)")
	}

	// 4. Ensure HC settings
	if !cfg.dryRun {
		ensureHCSettings(ctx, hcClient, cfg)
	} else {
		slog.Info("hc: skip ensure settings (dry-run)")
	}

	// 5. Fetch desired state from k8s
	podIPs, err := fetchK8sPodIPs(ctx, kubeClient, cfg.namespace, cfg.labelSelector)
	if err != nil {
		return fmt.Errorf("fetch k8s pods: %w", err)
	}
	slog.Info("k8s: fetched ready pods", "count", len(podIPs))

	for ip := range podIPs {
		slog.Info("k8s: ready pod", "ip", ip)
	}

	// 6. Sync HC endpoints (source of truth для текущего состояния)
	if err := syncHCEndpoints(ctx, hcClient, cfg, podIPs, cfg.dryRun); err != nil {
		return fmt.Errorf("sync hc endpoints: %w", err)
	}

	// 7. Найти stale эндпоинты через HC и удалить из CPL
	staleIPs, err := findStaleCPLEndpoints(ctx, hcClient, cfg.targetGroup, podIPs)
	if err != nil {
		return fmt.Errorf("find stale cpl endpoints: %w", err)
	}
	if len(staleIPs) > 0 {
		if err := removeStaleCPLEndpoints(ctx, cplClient, cfg, staleIPs, cfg.dryRun); err != nil {
			return fmt.Errorf("remove stale cpl endpoints: %w", err)
		}
	}

	// 8. Добавить все desired в CPL (идемпотентно)
	if err := syncCPLEndpoints(ctx, cplClient, cfg, podIPs, cfg.dryRun); err != nil {
		return fmt.Errorf("sync cpl endpoints: %w", err)
	}

	slog.Info("sync complete",
		"target_group", cfg.targetGroup,
		"desired_pods", len(podIPs),
		"stale_removed", len(staleIPs),
	)

	return nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}
