package httphc

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/healthcheck"
)

type HTTPStrategy struct {
	client *http.Client
	req    *http.Request
	body   []byte
}

type HTTPStrategySettings struct {
	Timeout       time.Duration `json:"timeout"`
	Path          string        `json:"path"`
	Scheme        string        `json:"scheme"`
	Method        string        `json:"method"`
	Headers       http.Header   `json:"-"`
	Body          []byte        `json:"-"`
	TLSServerName string        `json:"-"`
	TLSSkipVerify bool          `json:"-"`
}

func NewHTTPStrategy(settings *HTTPStrategySettings, target healthcheck.TargetAddr) (*HTTPStrategy, error) {
	transport := http.Transport{
		DisableKeepAlives: true,
	}
	targetUrl := url.URL{
		Scheme: settings.Scheme,
		Path:   settings.Path,
		Host:   fmt.Sprintf("%s:%d", target.RealIP, target.Port),
	}
	if settings.Timeout == 0 {
		settings.Timeout = time.Second
	}
	if targetUrl.Scheme == "https" {
		tlsConfig := new(tls.Config)
		tlsConfig.InsecureSkipVerify = settings.TLSSkipVerify
		tlsConfig.ServerName = settings.TLSServerName

		transport.TLSClientConfig = tlsConfig
		transport.TLSHandshakeTimeout = settings.Timeout
	}
	clnt := http.Client{
		Timeout:   settings.Timeout,
		Transport: &transport,
	}
	method := http.MethodGet
	if settings.Method != "" {
		method = settings.Method
	}
	req, err := http.NewRequest(
		method,
		targetUrl.String(),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to form http initial request for hc: %w", err)
	}
	// TODO: debug
	if settings.Headers == nil {
		settings.Headers = make(http.Header)
	}
	if settings.Headers.Get("User-Agent") == "" {
		settings.Headers.Add("User-Agent", os.Getenv("HC_WORKER_ID"))
	}
	req.Header = settings.Headers
	return &HTTPStrategy{
		body:   settings.Body,
		req:    req,
		client: &clnt,
	}, nil
}

func (tc *HTTPStrategy) DoHealthCheck() (bool, error) {
	req := tc.req.Clone(context.Background())
	req.Body = io.NopCloser(bytes.NewReader(tc.body))
	resp, err := tc.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("request do error: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode/100 == 2 {
		return true, nil
	}
	log.Debug().Msgf("[http-hc]: invalid status code = %d", resp.StatusCode)
	return false, nil
}
