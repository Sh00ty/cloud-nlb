package healthcheck

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

type StrategyName string

const (
	MockStrategy StrategyName = "mock"
	HTTPStrategy StrategyName = "http"
	TCPStrategy  StrategyName = "tcp"
)

type Strategy interface {
	DoHealthCheck() (bool, error)
}

type TargetGroupID string

type Settings struct {
	TargetGroup            TargetGroupID
	Strategy               StrategyName
	StrategySettings       []byte
	Interval               time.Duration
	SuccessBeforePassing   uint8
	FailuresBeforeCritical uint8
	InitialState           bool
}

type Target struct {
	TargetGroup TargetGroupID
	RealIP      net.IP
	Port        uint16
}

func (t Target) ToAddr() TargetAddr {
	return TargetAddr{
		RealIP: t.RealIP,
		Port:   t.Port,
	}
}

type TargetAddr struct {
	RealIP net.IP
	Port   uint16
}

func (a TargetAddr) String() string {
	return fmt.Sprintf("%s:%d", a.RealIP.String(), a.Port)
}

func TargetAddrFromString(str string) (TargetAddr, error) {
	addr, portStr, ok := strings.Cut(str, ":")
	if !ok {
		return TargetAddr{}, fmt.Errorf("invalid format")
	}
	ip := net.ParseIP(addr)
	port, err := strconv.ParseUint(portStr, 10, 64)
	if err != nil {
		return TargetAddr{}, fmt.Errorf("failed to parse port: %w", err)
	}
	return TargetAddr{
		RealIP: ip,
		Port:   uint16(port),
	}, nil
}
