package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// Интервал между последовательными HC запросами от одного user-agent
	hcPerAgentInterval = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "testserver",
		Name:      "hc_per_agent_interval_seconds",
		Help:      "Interval between consecutive health check requests from the same user-agent.",
		Buckets:   []float64{1, 1.2, 1.4, 1.6, 1.8, 2.0, 2.2, 2.4, 2.6, 2.8, 3, 3.2, 3.4, 3.6, 3.8, 4},
	}, []string{"user_agent"})

	// Интервал покрытия проверками целиком
	hcCoverageInterval = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "testserver",
		Name:      "hc_coverage_interval_seconds",
		Help:      "Interval between any two consecutive health check requests (overall coverage).",
		Buckets:   []float64{0.1, 0.2, 0.5, 0.8, 1.0, 1.3, 1.5, 2, 2.2, 2.5, 3, 3.5, 5, 10},
	})

	hcRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "testserver",
		Name:      "hc_requests_total",
		Help:      "Total number of health check requests received.",
	}, []string{"user_agent"})

	hcTimeSinceLastRequest = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "testserver",
		Name:      "hc_time_since_last_request_seconds",
		Help:      "Time elapsed since the last health check request from any agent.",
	})

	hcActiveAgents = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "testserver",
		Name:      "hc_active_agents",
		Help:      "Number of unique user-agents that have sent at least one health check.",
	})

	// Метрики трафика
	trafficRequestsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "testserver",
		Name:      "traffic_requests_total",
		Help:      "Total number of traffic requests received.",
	})

	trafficRequestDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "testserver",
		Name:      "traffic_request_duration_seconds",
		Help:      "Duration of traffic request handling.",
		Buckets:   prometheus.DefBuckets,
	})
)

type hcTracker struct {
	mu           sync.Mutex
	lastGlobal   time.Time
	lastPerAgent map[string]time.Time
}

func newHcTracker() *hcTracker {
	return &hcTracker{
		lastGlobal:   time.Now(),
		lastPerAgent: make(map[string]time.Time, 16),
	}
}

type intervals struct {
	global   time.Duration
	perAgent time.Duration
	isFirst  bool
}

func (t *hcTracker) recordRequest(ua string) intervals {
	now := time.Now()

	t.mu.Lock()
	defer t.mu.Unlock()

	result := intervals{}

	globalInterval := now.Sub(t.lastGlobal)
	result.global = globalInterval

	t.lastGlobal = now
	hcCoverageInterval.Observe(globalInterval.Seconds())

	if lastTime, exists := t.lastPerAgent[ua]; exists {
		agentInterval := now.Sub(lastTime)
		result.perAgent = agentInterval
		hcPerAgentInterval.WithLabelValues(ua).Observe(agentInterval.Seconds())
	} else {
		result.isFirst = true
	}
	t.lastPerAgent[ua] = now

	hcActiveAgents.Set(float64(len(t.lastPerAgent)))
	hcRequestsTotal.WithLabelValues(ua).Inc()

	return result
}

func (t *hcTracker) timeSinceLastRequest() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	return time.Since(t.lastGlobal).Seconds()
}

func main() {
	hostname, _ := os.Hostname()
	podIP := os.Getenv("POD_IP")

	go startProbeServer()
	go startHCServer(hostname)
	go startTrafficServer(hostname, podIP)
	startMetrics(newHcTracker())
}

func startHCServer(hostname string) {
	tracker := newHcTracker()
	mux := http.NewServeMux()

	mux.HandleFunc("/health/", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		ua := r.UserAgent()
		iv := tracker.recordRequest(ua)
		if iv.isFirst {
			log.Printf(
				"got hc request %s from %s (first request, global_interval=%s)",
				r.RequestURI, ua, iv.global,
			)
		} else {
			log.Printf(
				"got hc request %s from %s (agent_interval=%s, global_interval=%s)",
				r.RequestURI, ua, iv.perAgent, iv.global,
			)
		}

		w.WriteHeader(http.StatusOK)
	})

	hcPort := os.Getenv("HC_PORT")
	if hcPort == "" {
		hcPort = "8090"
	}
	addr := fmt.Sprintf("0.0.0.0:%s", hcPort)
	log.Printf("HC server starting on %s", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("HC server failed: %v", err)
	}
}

var requestCounter atomic.Uint64

func startTrafficServer(hostname, podIP string) {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() {
			trafficRequestDuration.Observe(time.Since(start).Seconds())
		}()

		trafficRequestsTotal.Inc()
		reqNum := requestCounter.Add(1)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Served-By", hostname)

		resp := map[string]interface{}{
			"hostname":    hostname,
			"pod_ip":      podIP,
			"request_num": reqNum,
			"timestamp":   time.Now().Format(time.RFC3339Nano),
			"path":        r.URL.Path,
			"remote_addr": r.RemoteAddr,
		}

		json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		trafficRequestsTotal.Inc()
		w.Header().Set("X-Served-By", hostname)
		fmt.Fprintf(w, "pong from %s (%s)\n", hostname, podIP)
	})

	trafficPort := os.Getenv("TRAFFIC_PORT")
	if trafficPort == "" {
		trafficPort = "10000"
	}
	addr := fmt.Sprintf("0.0.0.0:%s", trafficPort)
	log.Printf("Traffic server starting on %s", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Traffic server failed: %v", err)
	}
}

// ──────────────── Probe server ────────────────

func startProbeServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("Probe server starting on 0.0.0.0:8080")
	if err := http.ListenAndServe("0.0.0.0:8080", mux); err != nil {
		log.Fatalf("Probe server failed: %v", err)
	}
}

// ──────────────── Metrics server ────────────────

func startMetrics(tracker *hcTracker) {
	addr := os.Getenv("METRICS_ADDR")
	if addr == "" {
		addr = "0.0.0.0:8081"
	}

	prometheus.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Namespace: "testserver",
			Name:      "hc_time_since_last_request_seconds_live",
			Help:      "Time since last HC request, computed at scrape time.",
		},
		tracker.timeSinceLastRequest,
	))

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	log.Printf("Metrics server starting on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Metrics server failed: %v", err)
	}
}
