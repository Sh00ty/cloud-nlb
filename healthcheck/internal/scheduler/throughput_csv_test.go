package scheduler

import (
	"encoding/csv"
	"fmt"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// TestSchedulerThroughputCSV writes /tmp/scheduler_throughput.csv with the pure
// scheduling throughput and per-iteration latency percentiles as a function of the
// number of scheduled targets. Throughput is measured in a tight loop; latency
// percentiles in a separate timed pass.
func TestSchedulerThroughputCSV(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping throughput CSV in short mode")
	}
	sizes := []int{1000, 5000, 10000, 50000, 100000, 500000, 1000000}

	f, err := os.Create("/tmp/scheduler_throughput.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"targets", "ns_per_iter", "tasks_per_s", "p50_us", "p99_us"})

	future := time.Now().Add(1000 * time.Hour)
	const iters = 200000

	for _, n := range sizes {
		s := New(buildChecks(n, 3*time.Second), noopExecutor{}, zerolog.Nop())
		for range n { // warm-up so every entry has been rescheduled once
			s.runIteration(future)
		}

		// pass 1: throughput (no per-iteration timing overhead)
		start := time.Now()
		for range iters {
			s.runIteration(future)
		}
		elapsed := time.Since(start)
		tps := float64(iters) / elapsed.Seconds()
		nsPerIter := float64(elapsed.Nanoseconds()) / float64(iters)

		// pass 2: latency distribution
		lat := make([]time.Duration, iters)
		for i := range iters {
			t0 := time.Now()
			s.runIteration(future)
			lat[i] = time.Since(t0)
		}
		slices.Sort(lat)
		p50 := float64(lat[iters*50/100].Nanoseconds()) / 1000.0
		p99 := float64(lat[iters*99/100].Nanoseconds()) / 1000.0

		_ = w.Write([]string{strconv.Itoa(n), fmt.Sprintf("%.0f", nsPerIter),
			fmt.Sprintf("%.0f", tps), fmt.Sprintf("%.3f", p50), fmt.Sprintf("%.3f", p99)})
		fmt.Printf("targets=%-8d ns/iter=%-6.0f tasks/s=%-8.0f p50=%.2fus p99=%.2fus\n",
			n, nsPerIter, tps, p50, p99)
	}
	t.Log("wrote /tmp/scheduler_throughput.csv")
}
