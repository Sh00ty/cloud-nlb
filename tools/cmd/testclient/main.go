package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	reqTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "testclient",
		Name:      "requests_total",
		Help:      "Total number of requests made.",
	}, []string{"method", "code", "served_by"})

	reqErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "testclient",
		Name:      "request_errors_total",
		Help:      "Total request errors (transport/timeouts/etc).",
	}, []string{"kind"})

	inflight = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "testclient",
		Name:      "inflight",
		Help:      "Number of in-flight requests.",
	})

	latency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "testclient",
		Name:      "request_duration_seconds",
		Help:      "Request duration.",
		Buckets:   []float64{0.001, 0.002, 0.005, 0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5},
	}, []string{"method", "code"})

	servedBySeen = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "testclient",
		Name:      "served_by_last_seen_unixtime",
		Help:      "Last time (unix) a backend with X-Served-By was observed.",
	}, []string{"served_by"})
)

type cfg struct {
	url        string
	method     string
	body       string
	concur     int
	qps        float64
	timeout    time.Duration
	duration   time.Duration
	keepAlive  bool
	metricsAdr string

	// jitter helps desync workers (prevents burst alignment)
	jitter time.Duration
}

func main() {
	var c cfg
	flag.StringVar(&c.url, "url", "http://nlb-vip-test-server.cloud-nlb:10000/ping", "Target URL")
	flag.StringVar(&c.method, "method", "GET", "HTTP method")
	flag.StringVar(&c.body, "body", "", "Request body (for POST/PUT)")
	flag.IntVar(&c.concur, "c", 10, "Concurrency (workers)")
	flag.Float64Var(&c.qps, "qps", 20, "Target overall QPS (0 = max)")
	flag.DurationVar(&c.timeout, "timeout", 2*time.Second, "Per-request timeout")
	flag.DurationVar(&c.duration, "duration", 0, "How long to run (0 = forever)")
	flag.BoolVar(&c.keepAlive, "keepalive", true, "HTTP keep-alive")
	flag.StringVar(&c.metricsAdr, "metrics-addr", "0.0.0.0:8081", "Prometheus metrics listen address")
	flag.DurationVar(&c.jitter, "jitter", 250*time.Millisecond, "Random start jitter per worker")
	flag.Parse()

	go serveMetrics(c.metricsAdr)

	client := newHTTPClient(c.keepAlive, c.timeout)

	ctx := context.Background()
	if c.duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.duration)
		defer cancel()
	}

	log.Printf("testclient starting: url=%s method=%s c=%d qps=%.2f timeout=%s keepalive=%v duration=%s metrics=%s",
		c.url, c.method, c.concur, c.qps, c.timeout, c.keepAlive, c.duration, c.metricsAdr)

	var wg sync.WaitGroup
	wg.Add(c.concur)

	// per-worker pacing; if qps == 0, workers run as fast as possible
	var perWorkerInterval time.Duration
	if c.qps > 0 {
		perWorkerInterval = time.Duration(float64(time.Second) * float64(c.concur) / c.qps)
		if perWorkerInterval < 0 {
			perWorkerInterval = 0
		}
	}

	for i := 0; i < c.concur; i++ {
		workerID := i
		go func() {
			defer wg.Done()

			// jitter
			if c.jitter > 0 {
				time.Sleep(time.Duration(rand.Int64N(int64(c.jitter))))
			}

			t := time.NewTicker(perWorkerInterval)
			defer t.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				if c.qps > 0 && perWorkerInterval > 0 {
					select {
					case <-ctx.Done():
						return
					case <-t.C:
					}
				}

				doOne(ctx, client, workerID, c)
			}
		}()
	}

	wg.Wait()
}

func doOne(parent context.Context, client *http.Client, workerID int, c cfg) {
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()

	var body io.Reader
	if c.body != "" {
		body = strings.NewReader(c.body)
	}

	req, err := http.NewRequestWithContext(ctx, c.method, c.url, body)
	if err != nil {
		reqErrorsTotal.WithLabelValues("new_request").Inc()
		return
	}

	// Make UA unique per worker (useful if you later hit /health/ on servers)
	req.Header.Set("User-Agent", fmt.Sprintf("testclient/%s worker-%d", hostname(), workerID))

	inflight.Inc()
	start := time.Now()
	resp, err := client.Do(req)
	d := time.Since(start)
	inflight.Dec()

	if err != nil {
		kind := classifyErr(err)
		reqErrorsTotal.WithLabelValues(kind).Inc()
		latency.WithLabelValues(c.method, "error").Observe(d.Seconds())
		return
	}
	defer resp.Body.Close()

	// drain body to reuse keepalive connections
	_, _ = io.Copy(io.Discard, resp.Body)

	code := fmt.Sprintf("%d", resp.StatusCode)
	servedBy := resp.Header.Get("X-Served-By")
	if servedBy == "" {
		servedBy = "unknown"
	}

	reqTotal.WithLabelValues(c.method, code, servedBy).Inc()
	latency.WithLabelValues(c.method, code).Observe(d.Seconds())
	servedBySeen.WithLabelValues(servedBy).Set(float64(time.Now().Unix()))
}

func classifyErr(err error) string {
	// Very rough but good enough for error-rate dashboards
	s := err.Error()
	switch {
	case strings.Contains(s, "context deadline exceeded"):
		return "timeout"
	case strings.Contains(s, "connection refused"):
		return "refused"
	case strings.Contains(s, "no such host"):
		return "dns"
	case strings.Contains(s, "EOF"):
		return "eof"
	default:
		return "other"
	}
}

func newHTTPClient(keepAlive bool, timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
	}

	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
		ForceAttemptHTTP2:     false,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, // for dev only
		MaxIdleConns:          1000,
		MaxIdleConnsPerHost:   1000,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ExpectContinueTimeout: 1 * time.Second,
		DisableKeepAlives:     !keepAlive,
	}

	return &http.Client{
		Transport: tr,
		Timeout:   0, // rely on per-request context timeout
	}
}

func serveMetrics(addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	log.Printf("metrics listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("metrics server failed: %v", err)
	}
}

func hostname() string {
	h, _ := os.Hostname()
	if h == "" {
		return "unknown"
	}
	return h
}
