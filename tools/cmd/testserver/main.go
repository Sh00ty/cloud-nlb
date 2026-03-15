package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
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

	// Интервал покрытия проверками целиком (между любыми двумя последовательными HC от любого агента)
	hcCoverageInterval = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "testserver",
		Name:      "hc_coverage_interval_seconds",
		Help:      "Interval between any two consecutive health check requests (overall coverage).",
		Buckets:   []float64{0.1, 0.2, 0.5, 0.8, 1.0, 1.3, 1.5, 2, 2.2, 2.5, 3, 3.5, 5, 10},
	})

	// Общее количество HC запросов
	hcRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "testserver",
		Name:      "hc_requests_total",
		Help:      "Total number of health check requests received.",
	}, []string{"user_agent"})

	// Время с последнего HC запроса (для алертов: если растёт — проверки не приходят)
	hcTimeSinceLastRequest = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "testserver",
		Name:      "hc_time_since_last_request_seconds",
		Help:      "Time elapsed since the last health check request from any agent. Updated on each scrape.",
	})

	// Количество уникальных user-agent-ов, приславших хотя бы один HC
	hcActiveAgents = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "testserver",
		Name:      "hc_active_agents",
		Help:      "Number of unique user-agents that have sent at least one health check.",
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
	mux := http.NewServeMux()
	tracker := newHcTracker()

	mux.HandleFunc("/health/", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		ua := r.UserAgent()
		iv := tracker.recordRequest(ua)
		if iv.isFirst {
			log.Printf(
				"got hc request %s from %s (first request, global_interval=%s)",
				r.RequestURI,
				ua,
				iv.global,
			)
		} else {
			log.Printf(
				"got hc request %s from %s (agent_interval=%s, global_interval=%s)",
				r.RequestURI,
				ua,
				iv.perAgent,
				iv.global,
			)
		}

		w.WriteHeader(http.StatusOK)
	})

	go startProbeServer()
	go startMetrics(tracker)

	hcPort := os.Getenv("HC_PORT")
	if len(os.Args) > 1 {
		hcPort = os.Args[1]
	}
	addr := fmt.Sprintf("0.0.0.0:%s", hcPort)
	log.Printf("starting server on %s", addr)

	err := http.ListenAndServe(addr, mux)
	if err != nil {
		log.Fatal(err)
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
			log.Fatalf("failed to start http server: %v", err)
		}
	}()
	return func() {
		_ = srv.Close()
	}
}

func startMetrics(tracker *hcTracker) {
	var (
		addr = os.Getenv("METRICS_ADDR")
		mux  = http.NewServeMux()
	)
	if addr == "" {
		return
	}

	prometheus.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Namespace: "testserver",
			Name:      "hc_time_since_last_request_seconds_live",
			Help:      "Time since last HC request, computed at scrape time.",
		},
		tracker.timeSinceLastRequest,
	))

	mux.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(addr, mux)
}
