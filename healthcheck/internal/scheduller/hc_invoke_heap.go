package scheduller

import (
	"container/heap"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/Sh00ty/network-lb/health-check-node/internal/models"
	"github.com/Sh00ty/network-lb/health-check-node/pkg/healthcheck"
)

var ErrNoHealthCheckDefined = errors.New("no health check is defined")
var _ heap.Interface = (*timeBasedHeap)(nil)

type hcInvokeHeap struct {
	// TODO: add target to index map
	// make index movements in heap.swap
	// it will make remove and add from O(n) to O(1)
	hcHeap timeBasedHeap
	guard  sync.Mutex
}

func newHcInvokeHeap(healthChecks []models.HealthCheck) *hcInvokeHeap {
	hp := &hcInvokeHeap{
		hcHeap: healthChecks,
	}
	heap.Init(&hp.hcHeap)
	return hp
}

func (h *hcInvokeHeap) updateAndGetTop() *models.HealthCheck {
	h.guard.Lock()
	defer h.guard.Unlock()

	if len(h.hcHeap) == 0 {
		return nil
	}
	h.hcHeap[0].NextInvoke = time.Now().Add(h.hcHeap[0].Settings.Interval)
	heap.Fix(&h.hcHeap, 0)
	return &h.hcHeap[0]
}

func (h *hcInvokeHeap) getNextHc() *models.HealthCheck {
	h.guard.Lock()
	defer h.guard.Unlock()

	if len(h.hcHeap) == 0 {
		return nil
	}
	return &h.hcHeap[0]
}

func (h *hcInvokeHeap) find(target healthcheck.TargetAddr) int {
	h.guard.Lock()
	defer h.guard.Unlock()

	index := slices.IndexFunc(h.hcHeap, func(hc models.HealthCheck) bool {
		return hc.Target.RealIP.Equal(target.RealIP) && hc.Target.Port == target.Port
	})
	return index
}

func (h *hcInvokeHeap) remove(target healthcheck.TargetAddr) bool {
	index := h.find(target)
	if index < 0 {
		return false
	}
	h.guard.Lock()
	defer h.guard.Unlock()
	heap.Remove(&h.hcHeap, index)
	return true
}

type timeBasedHeap []models.HealthCheck

func (t timeBasedHeap) Len() int {
	return len(t)
}

func (t timeBasedHeap) Less(i int, j int) bool {
	return t[i].NextInvoke.Before(t[j].NextInvoke)
}

func (t timeBasedHeap) Swap(i int, j int) {
	t[i], t[j] = t[j], t[i]
}

func (t *timeBasedHeap) Push(x any) {
	*t = append(*t, x.(models.HealthCheck))
}

func (t *timeBasedHeap) Pop() any {
	if t.Len() == 0 {
		return nil
	}
	topVal := (*t)[t.Len()-1]
	*t = (*t)[:t.Len()-1]
	return topVal
}

func (h *hcInvokeHeap) push(hc models.HealthCheck) {
	heap.Push(&h.hcHeap, hc)
}
