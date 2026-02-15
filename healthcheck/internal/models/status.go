package models

import (
	"time"

	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/healthcheck"
)

type HcEvent struct {
	TargetGroup healthcheck.TargetGroupID
	Target      healthcheck.TargetAddr
	HcInterval  time.Duration
	Error       error
	NewStatus   bool
}

type TargetStatus struct {
	TargetGroup healthcheck.TargetGroupID
	Target      healthcheck.TargetAddr
	Error       string
	Status      bool
}
