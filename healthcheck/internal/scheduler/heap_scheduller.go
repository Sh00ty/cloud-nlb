package scheduler

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/Sh00ty/cloud-nlb/healthcheck/internal/models"
	"github.com/Sh00ty/cloud-nlb/healthcheck/pkg/healthcheck"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

const (
	emptyHcLoopInterval = 1 * time.Second
)

type TaskExecutor interface {
	ExecuteHealthCheck(hc models.HealthCheck) error
}

type Scheduler struct {
	invocationHeapGuard sync.Mutex
	invocationHeap      *hcInvokeHeap
	timer               *time.Timer
	executor            TaskExecutor

	log zerolog.Logger
}

func New(healthChecks []models.HealthCheck, executor TaskExecutor, log zerolog.Logger) *Scheduler {
	internalHealthChecks := make([]models.HealthCheck, 0, len(healthChecks))
	for _, hc := range healthChecks {
		hc.NextInvoke = addIntervalWithJitter(hc.Settings.Interval)
		internalHealthChecks = append(internalHealthChecks, hc)
	}
	return &Scheduler{
		invocationHeap: newHcInvokeHeap(internalHealthChecks),
		timer:          time.NewTimer(time.Until(invokeTimeOrDefault(nil))),
		executor:       executor,
		log:            log.With().Str("component", "healthcheck_scheduler").Logger(),
	}
}

func (p *Scheduler) Remove(check healthcheck.TargetAddr) bool {
	p.invocationHeapGuard.Lock()
	defer p.invocationHeapGuard.Unlock()

	removed := p.invocationHeap.remove(check)
	removesTotal.WithLabelValues(boolToStr(removed)).Add(1)

	nextHc := p.invocationHeap.getNextHc()
	p.timer.Reset(time.Until(invokeTimeOrDefault(nextHc)))

	return removed
}

func (p *Scheduler) Add(hc models.HealthCheck) error {
	p.invocationHeapGuard.Lock()
	defer p.invocationHeapGuard.Unlock()

	index := p.invocationHeap.find(hc.Target)
	if index >= 0 {
		return nil
	}
	hc.NextInvoke = addIntervalWithJitter(hc.Settings.Interval)
	p.invocationHeap.push(hc)

	nextHc := p.invocationHeap.getNextHc()
	p.timer.Reset(time.Until(invokeTimeOrDefault(nextHc)))

	addsTotal.Add(1)
	return nil
}

func addIntervalWithJitter(interval time.Duration) time.Time {
	return time.Now().Add(interval + jit(interval))
}

func invokeTimeOrDefault(hc *models.HealthCheck) time.Time {
	if hc == nil {
		return time.Now().Add(emptyHcLoopInterval)
	}
	return hc.NextInvoke
}

func (p *Scheduler) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case invokeTime := <-p.timer.C:
			p.runIteration(invokeTime)
		}
	}
}

func (p *Scheduler) runIteration(invokeTime time.Time) {
	scheduleTimer := prometheus.NewTimer(scheduleDuration)
	defer scheduleTimer.ObserveDuration()

	p.invocationHeapGuard.Lock()
	defer p.invocationHeapGuard.Unlock()

	wantExecute := p.invocationHeap.getNextHc()
	if wantExecute != nil && !wantExecute.NextInvoke.After(invokeTime) {
		executeDelay := time.Since(wantExecute.NextInvoke)
		invocationDelay.Observe(executeDelay.Seconds())

		p.log.Debug().
			Str("target", wantExecute.Target.String()).
			Time("next_invoke", wantExecute.NextInvoke).
			Time("invoke_time", invokeTime).
			Int("heap_size", p.invocationHeap.hcHeap.Len()).
			Msg("executing hc")

		// мы не боимся того, что забьется что-то в экзекуторе, так как
		// min time heap сам по себе будет работать нормально
		err := p.executor.ExecuteHealthCheck(*wantExecute)
		if err != nil {
			p.log.Error().Err(err).Msg("executing healthcheck task")
		}
		p.invocationHeap.updateTop()
	} else {
		emptyLoopIterationsTotal.Add(1)
	}
	nextHc := p.invocationHeap.getNextHc()
	p.timer.Reset(time.Until(invokeTimeOrDefault(nextHc)))
}

func jit(internal time.Duration) time.Duration {
	return time.Duration(rand.Uint64N(uint64(internal)))
}
