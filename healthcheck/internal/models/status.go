package models

import (
	"time"

	"github.com/Sh00ty/network-lb/health-check-node/pkg/healthcheck"
)

type HcEvent struct {
	SettingID  int64
	Target     healthcheck.TargetAddr
	HcInverval time.Duration
	Error      error
	NewStatus  bool
}
