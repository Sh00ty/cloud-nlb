package tcpconnhc

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/healthcheck"
)

const (
	tcpNetwork    = "TCP"
	tcpTLSNetwork = "TCP+TLS"
)

type TcpHealthCheckSettings struct {
	Timeout       time.Duration
	TLSServerName string
	TLSSkipVerify bool
}

type TcpConnStrategy struct {
	network    string
	targetAddr string
	tlsConfig  *tls.Config
	dialer     net.Dialer
}

func NewTcpConnStrategy(settings *TcpHealthCheckSettings, target healthcheck.TargetAddr) (*TcpConnStrategy, error) {
	if len(target.RealIP.String()) == 0 {
		return nil, fmt.Errorf("invalid real ip format: zero lenght")
	}
	var (
		network   = tcpNetwork
		tlsConfig *tls.Config
	)
	if settings.TLSServerName != "" || !settings.TLSSkipVerify {
		network = tcpTLSNetwork
		tlsConfig = new(tls.Config)
		tlsConfig.InsecureSkipVerify = settings.TLSSkipVerify
		tlsConfig.ServerName = settings.TLSServerName
	}
	return &TcpConnStrategy{
		network:    network,
		targetAddr: fmt.Sprintf("%s:%d", target.RealIP.String(), target.Port),
		tlsConfig:  tlsConfig,
		dialer: net.Dialer{
			Timeout:   settings.Timeout,
			KeepAlive: -1,
		},
	}, nil
}

func (tc *TcpConnStrategy) DoHealthCheck() (bool, error) {
	var (
		conn net.Conn
		err  error
	)
	if tc.tlsConfig == nil {
		conn, err = tc.dialer.Dial(tc.network, tc.targetAddr)
	} else {
		conn, err = tls.DialWithDialer(&tc.dialer, tc.network, tc.targetAddr, tc.tlsConfig)
	}
	if err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return false, err
		}
		return false, err
	}
	_ = conn.Close()
	return true, nil
}
