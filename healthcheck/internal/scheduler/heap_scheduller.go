package scheduler

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/Sh00ty/network-lb/health-check-node/internal/models"
	"github.com/Sh00ty/network-lb/health-check-node/pkg/healthcheck"
)

const (
	emptyHcLoopInterval = 1 * time.Second
)

type TaskExecutor interface {
	ExecuteHealthCheck(hc *models.HealthCheck) error
}

type Scheduller struct {
	invokationHeap *hcInvokeHeap
	executor       TaskExecutor
}

func New(healthChecks []models.HealthCheck, executor TaskExecutor) *Scheduller {
	internalHealthChecks := make([]models.HealthCheck, 0, len(healthChecks))
	for _, hc := range healthChecks {
		hc.NextInvoke = addInvernalWithJitter(hc.Settings.Interval)
		internalHealthChecks = append(internalHealthChecks, hc)
	}
	return &Scheduller{
		invokationHeap: newHcInvokeHeap(internalHealthChecks),
		executor:       executor,
	}
}

func (p *Scheduller) Remove(id healthcheck.TargetAddr) bool {
	return p.invokationHeap.remove(id)
}

func (p *Scheduller) Add(hc models.HealthCheck) error {
	index := p.invokationHeap.find(hc.Target)
	if index >= 0 {
		return nil
	}
	hc.NextInvoke = addInvernalWithJitter(hc.Settings.Interval)
	p.invokationHeap.push(hc)
	return nil
}

func addInvernalWithJitter(interval time.Duration) time.Time {
	return time.Now().Add(interval + jit(interval))
}

func invokeTimeOrDefault(hc *models.HealthCheck) time.Time {
	if hc == nil {
		return time.Now().Add(emptyHcLoopInterval)
	}
	return hc.NextInvoke
}

func (p *Scheduller) Run(ctx context.Context) error {
	nextHc := p.invokationHeap.getNextHc()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Until(invokeTimeOrDefault(nextHc))):
		}
		if nextHc != nil {
			// мы не боимся того, что забьется что-то в экзекуторе, так как
			// min time heap сам по себе будет работать нормально
			err := p.executor.ExecuteHealthCheck(nextHc)
			if err != nil {
				return fmt.Errorf("failed to execute task: %v", err)
			}
			nextHc = p.invokationHeap.updateAndGetTop()
		} else {
			nextHc = p.invokationHeap.getNextHc()
		}
	}
}

func jit(internal time.Duration) time.Duration {
	return time.Duration(rand.Uint64N(uint64(internal)))
}
