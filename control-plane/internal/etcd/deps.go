package etcd

import (
	"context"
	"time"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type targetGroupSpec struct {
	Version uint64          `json:"version"`
	VIP     string          `json:"vip"`
	Port    uint16          `json:"port"`
	Proto   models.Protocol `json:"protocol"`
	Time    time.Time       `json:"time"`
}

type endpointSpec struct {
	IP     string `json:"real_ip"`
	Port   uint16 `json:"port"`
	Weight uint16 `json:"weight"`
}

type endpointLogEntry struct {
	Type          models.EventType     `json:"type"`
	TargetGroupID models.TargetGroupID `json:"target_group_id"`
	Timestamp     uint64               `json:"timestamp"`
	Endpoint      endpointSpec         `json:"endpoint_spec"`
	Time          time.Time            `json:"time"`
}

type WatchHandler func(ctx context.Context, events *clientv3.Event) error

const (
	maxTxRetryAttempts      = 5
	leaderLeaseTTLInSeconds = 15
	maxEventPendingDuration = time.Minute
)
