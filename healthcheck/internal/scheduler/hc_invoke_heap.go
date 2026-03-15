package scheduler

import (
	"container/heap"
	"errors"
	"time"

	"github.com/Sh00ty/cloud-nlb/healthcheck/internal/models"
	"github.com/Sh00ty/cloud-nlb/healthcheck/pkg/healthcheck"
)

var ErrNoHealthCheckDefined = errors.New("no health check is defined")
var _ heap.Interface = (*timeBasedHeap)(nil)

type hcInvokeHeap struct {
	hcHeap timeBasedHeap
}

func newHcInvokeHeap(healthChecks []models.HealthCheck) *hcInvokeHeap {
	var (
		deduplicated = make([]models.HealthCheck, 0, len(healthChecks))
		indexMap     = make(map[string]int, len(healthChecks))
	)
	for _, hc := range healthChecks {
		key := hc.Target.String()
		if _, exists := indexMap[key]; exists {
			continue
		}
		indexMap[key] = len(deduplicated)
		deduplicated = append(deduplicated, hc)
	}
	hp := &hcInvokeHeap{
		hcHeap: timeBasedHeap{
			heap:     deduplicated,
			indexMap: indexMap,
		},
	}
	heap.Init(&hp.hcHeap)
	heapSize.Set(float64(hp.hcHeap.Len()))
	return hp
}

func (h *hcInvokeHeap) updateTop() {
	if h.hcHeap.Len() == 0 {
		return
	}
	h.hcHeap.heap[0].NextInvoke = time.Now().Add(h.hcHeap.heap[0].Settings.Interval)
	heap.Fix(&h.hcHeap, 0)
}

func (h *hcInvokeHeap) getNextHc() *models.HealthCheck {
	if h.hcHeap.Len() == 0 {
		return nil
	}
	return &h.hcHeap.heap[0]
}

func (h *hcInvokeHeap) find(target healthcheck.TargetAddr) int {
	index, exists := h.hcHeap.indexMap[target.String()]
	if !exists {
		return -1
	}
	return index
}

func (h *hcInvokeHeap) push(hc models.HealthCheck) {
	heap.Push(&h.hcHeap, hc)
	heapSize.Set(float64(h.hcHeap.Len()))
}

func (h *hcInvokeHeap) remove(target healthcheck.TargetAddr) bool {
	index, exists := h.hcHeap.indexMap[target.String()]
	if !exists {
		return false
	}

	heap.Remove(&h.hcHeap, index)
	heapSize.Set(float64(h.hcHeap.Len()))
	return true
}

type timeBasedHeap struct {
	heap     []models.HealthCheck
	indexMap map[string]int
}

func (t timeBasedHeap) Len() int {
	return len(t.heap)
}

func (t timeBasedHeap) Less(i int, j int) bool {
	return t.heap[i].NextInvoke.Before(t.heap[j].NextInvoke)
}

func (t timeBasedHeap) Swap(first int, second int) {
	var (
		firstKey  = t.heap[first].Target.String()
		secondKey = t.heap[second].Target.String()
	)
	firstIdx := t.indexMap[firstKey]
	t.indexMap[firstKey] = t.indexMap[secondKey]
	t.indexMap[secondKey] = firstIdx

	t.heap[first], t.heap[second] = t.heap[second], t.heap[first]
}

func (t *timeBasedHeap) Push(x any) {
	var (
		hc  = x.(models.HealthCheck)
		key = hc.Target.String()
	)
	if _, exists := t.indexMap[key]; exists {
		return
	}

	t.indexMap[key] = len(t.heap)
	t.heap = append(t.heap, hc)
}

func (t *timeBasedHeap) Pop() any {
	if t.Len() == 0 {
		return nil
	}
	topVal := t.heap[t.Len()-1]
	delete(t.indexMap, topVal.Target.String())

	t.heap = t.heap[:t.Len()-1]
	return topVal
}
