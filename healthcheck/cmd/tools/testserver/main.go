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
	hcRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "testserver",
		Name:      "hc_request_interval_seconds",
		Help:      "Interval between consecutive health check requests.",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 3, 5, 10},
	}, []string{"user_agent"})

	hcRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "testserver",
		Name:      "hc_requests_total",
		Help:      "Total number of health check requests received.",
	}, []string{"user_agent"})
)

func main() {
	mux := http.NewServeMux()

	mu := sync.Mutex{}
	lastHcReq := time.Now()

	mux.HandleFunc("/health/", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		ua := r.UserAgent()
		hcRequestsTotal.WithLabelValues(ua).Inc()

		mu.Lock()
		interval := time.Since(lastHcReq)
		lastHcReq = time.Now()
		mu.Unlock()

		hcRequestDuration.WithLabelValues(ua).Observe(interval.Seconds())

		log.Printf(
			"got hc request %s from %s interval=%s",
			r.RequestURI,
			ua,
			interval,
		)
		w.WriteHeader(http.StatusOK)
	})

	mux.Handle("/metrics", promhttp.Handler())

	err := http.ListenAndServe(fmt.Sprintf("0.0.0.0:%s", os.Args[1]), mux)
	if err != nil {
		fmt.Println(err)
	}
}
