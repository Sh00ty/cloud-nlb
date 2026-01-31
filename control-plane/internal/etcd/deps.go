package etcd

import (
	"time"

	"github.com/Sh00ty/network-lb/control-plane/internal/models"
)

type event struct {
	Type           models.EventType `json:"event_type"`
	DesiredVersion uint             `json:"desired_version"`
	Time           time.Time        `json:"time"`
	Deadline       time.Time        `json:"deadline"`
	Payload        string           `json:"payload"`
}

type targetGroupSpec struct {
	Proto              models.Protocol `json:"protocol"`
	Port               uint16          `json:"port"`
	VIP                string          `json:"vip"`
	AssignedDataPlanes []string        `json:"assigned_data_planes"`
}

type endpointSpec struct {
	TargetGroupID models.TargetGroupID `json:"target_group_id"`
	IP            string               `json:"real_ip"`
	Port          uint16               `json:"port"`
	Weight        uint16               `json:"weight"`
}

const (
	maxTxRetryAttempts      = 5
	maxEventPendingDuration = time.Minute
)
