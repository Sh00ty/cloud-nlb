package models

import (
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/Sh00ty/network-lb/health-check-node/pkg/healthcheck"
	"github.com/Sh00ty/network-lb/health-check-node/pkg/strategies"
)

func NewHealthCheck(targetAddr healthcheck.TargetAddr, settings *healthcheck.Settings) (HealthCheck, error) {
	executable, err := newHcExecutable(targetAddr, settings)
	if err != nil {
		return HealthCheck{}, err
	}
	return HealthCheck{
		Settings:   settings,
		Target:     targetAddr,
		Executable: executable,
	}, nil
}

type HealthCheck struct {
	Target     healthcheck.TargetAddr
	Settings   *healthcheck.Settings
	NextInvoke time.Time
	Executable *Executable
}

func newHcExecutable(target healthcheck.TargetAddr, settings *healthcheck.Settings) (*Executable, error) {
	strategy, err := strategies.NewStrategy(settings.Strategy, target, settings.StrategySettings)
	if err != nil {
		return nil, fmt.Errorf("failed to create hc executable: %w", err)
	}
	return &Executable{
		guard:                  sync.RWMutex{},
		strategy:               strategy,
		successBeforePassing:   settings.SuccessBeforePassing,
		failuresBeforeCritical: settings.FailuresBeforeCritical,
		firstRun:               true,
	}, nil
}

type Executable struct {
	guard    sync.RWMutex
	strategy healthcheck.Strategy

	successBeforePassing uint8
	curSuccess           uint8

	failuresBeforeCritical uint8
	curFailures            uint8

	lastError error

	firstRun bool
	status   bool
}

func (t *Executable) DoHealthCheckIteration() bool {
	health, err := t.strategy.DoHealthCheck()

	t.guard.Lock()
	defer func() {
		t.firstRun = false
		t.guard.Unlock()
	}()

	prevStatus := t.status
	if err == nil && health {
		t.curSuccess++
		t.curFailures = 0
		if !t.status {
			t.status = t.successBeforePassing < t.curSuccess+1
		}
		t.lastError = nil
		return prevStatus != t.status || t.firstRun
	}

	log.Debug().Err(err).Msg("got error from check %+v")

	t.curFailures++
	if t.status {
		t.status = !(t.failuresBeforeCritical < t.curFailures+1)
	}
	t.lastError = err
	t.curSuccess = 0
	return prevStatus != t.status || t.firstRun
}

func (t *Executable) Info() (status bool, lastError error) {
	t.guard.RLock()
	defer t.guard.RUnlock()
	return t.status, t.lastError
}
