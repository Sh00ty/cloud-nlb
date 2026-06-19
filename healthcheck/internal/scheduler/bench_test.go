package scheduler

import (
	"fmt"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/Sh00ty/cloud-nlb/healthcheck/internal/models"
	"github.com/Sh00ty/cloud-nlb/healthcheck/pkg/healthcheck"
	"github.com/rs/zerolog"
)

// noopExecutor isolates the scheduling logic from the actual probing. The thesis
// argument is that the bottleneck under real load is the kernel network stack, not
// the scheduler; a no-op executor measures the scheduler in isolation.
type noopExecutor struct{}

func (noopExecutor) ExecuteHealthCheck(_ models.HealthCheck) error { return nil }

func buildChecks(n int, interval time.Duration) []models.HealthCheck {
	hcs := make([]models.HealthCheck, n)
	for i := range n {
		// IP fully encodes i, so every target is unique (heap deduplicates by addr).
		ip := net.IPv4(10, byte(i>>16), byte(i>>8), byte(i))
		hcs[i] = models.HealthCheck{
			Target:   healthcheck.TargetAddr{RealIP: ip, Port: 80},
			Settings: &healthcheck.Settings{Interval: interval},
		}
	}
	return hcs
}

var schedulerSizes = []int{1000, 10000, 100000, 1000000}

// BenchmarkScheduleIteration measures the per-task scheduling cost: each iteration
// pops the earliest-due check, dispatches it to the no-op executor and re-inserts it
// with the next invocation time (heap.Fix, O(log n)). Reported tasks/s is the pure
// scheduling throughput on a single node.
func BenchmarkScheduleIteration(b *testing.B) {
	for _, n := range schedulerSizes {
		b.Run(fmt.Sprintf("targets=%d", n), func(b *testing.B) {
			s := New(buildChecks(n, 3*time.Second), noopExecutor{}, zerolog.Nop())
			// far-future invoke time keeps the heap top always "due" so every
			// iteration performs a real dispatch.
			future := time.Now().Add(1000 * time.Hour)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.runIteration(future)
			}
			b.StopTimer()
			b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "tasks/s")
		})
	}
}

// TestScheduleLatencyDistribution reports the per-iteration scheduling latency
// distribution (p50/p90/p99/p99.9/max) for a large target set. Run explicitly:
//
//	go test -run TestScheduleLatencyDistribution -v ./internal/scheduler/
func TestScheduleLatencyDistribution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping latency distribution in short mode")
	}
	const (
		n      = 100000
		sample = 300000
	)
	s := New(buildChecks(n, 3*time.Second), noopExecutor{}, zerolog.Nop())
	future := time.Now().Add(1000 * time.Hour)

	// warm-up
	for range n {
		s.runIteration(future)
	}

	lat := make([]time.Duration, sample)
	for i := range sample {
		start := time.Now()
		s.runIteration(future)
		lat[i] = time.Since(start)
	}
	slices.Sort(lat)

	pct := func(p float64) time.Duration { return lat[int(float64(len(lat)-1)*p)] }
	var sum time.Duration
	for _, d := range lat {
		sum += d
	}
	mean := sum / time.Duration(len(lat))

	fmt.Printf("\n=== HC scheduler per-iteration latency (targets=%d, samples=%d) ===\n", n, sample)
	fmt.Printf("mean   = %v\n", mean)
	fmt.Printf("p50    = %v\n", pct(0.50))
	fmt.Printf("p90    = %v\n", pct(0.90))
	fmt.Printf("p99    = %v\n", pct(0.99))
	fmt.Printf("p99.9  = %v\n", pct(0.999))
	fmt.Printf("max    = %v\n", lat[len(lat)-1])
	fmt.Printf("throughput(mean) = %.0f tasks/s\n", float64(time.Second)/float64(mean))
}
