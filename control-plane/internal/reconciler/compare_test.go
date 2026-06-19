package reconciler

import (
	"encoding/csv"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"
)

// This file compares the two-step placement algorithm (section 5.7) against
// consistent-hashing baselines on load uniformity and build time. It emits CSV
// files consumed by the plotting script.

// hash64 hashes a string with FNV-1a and applies a murmur-style finalizer. Raw
// FNV-1a clusters badly on sequential keys like "tg-1", "tg-2" (verified: ring-CH
// load cv inflates from 0.14 to 1.2 at 500 nodes), which would unfairly handicap
// the consistent-hashing baselines.
func hash64(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	x := h.Sum64()
	x ^= x >> 33
	x *= 0xff51afd7ed558ccd
	x ^= x >> 33
	x *= 0xc4ceb9fe1a85ec53
	x ^= x >> 33
	return x
}

// mix combines a node seed with a target-group hash without re-hashing strings.
func mix(a, b uint64) uint64 {
	x := a ^ (b + 0x9e3779b97f4a7c15 + (a << 6) + (a >> 2))
	x ^= x >> 33
	x *= 0xff51afd7ed558ccd
	x ^= x >> 33
	return x
}

func msf(d time.Duration) float64 { return float64(d.Microseconds()) / 1000.0 }

// loadStats returns coefficient of variation and the hottest-node overhead
// (max-mean)/mean as a fraction.
func loadStats(load []int) (cv, maxOverhead float64) {
	n := len(load)
	if n == 0 {
		return 0, 0
	}
	sum, mx := 0, 0
	for _, l := range load {
		sum += l
		if l > mx {
			mx = l
		}
	}
	mean := float64(sum) / float64(n)
	if mean == 0 {
		return 0, 0
	}
	var ss float64
	for _, l := range load {
		d := float64(l) - mean
		ss += d * d
	}
	std := math.Sqrt(ss / float64(n))
	return std / mean, (float64(mx) - mean) / mean
}

// twoStepLoad runs the two-step reconciler (production single pass, or the
// corrected balanced variant under the placementfix tag — see runTwoStepPlacement)
// and returns per-node load and the time spent in placement generation
// (excluding etcd).
func twoStepLoad(numTG, numNodes, rf int) (load []int, dur time.Duration) {
	r := buildReconciler(numTG, numNodes, rf)
	dur = runTwoStepPlacement(r)
	load = make([]int, 0, numNodes)
	for _, pl := range r.placements {
		load = append(load, len(pl.TargetGroups))
	}
	return load, dur
}

// ringPlacement places rf replicas of each target group via classic ring-based
// consistent hashing with vnodes virtual nodes per physical node.
func ringPlacement(numTG, numNodes, rf, vnodes int) (load []int, dur time.Duration) {
	start := time.Now()
	type point struct {
		h    uint64
		node int
	}
	ring := make([]point, 0, numNodes*vnodes)
	for n := range numNodes {
		for v := range vnodes {
			ring = append(ring, point{h: hash64("dpl-" + strconv.Itoa(n) + "#" + strconv.Itoa(v)), node: n})
		}
	}
	slices.SortFunc(ring, func(a, b point) int {
		switch {
		case a.h < b.h:
			return -1
		case a.h > b.h:
			return 1
		default:
			return 0
		}
	})
	load = make([]int, numNodes)
	seen := make(map[int]struct{}, rf)
	for tg := range numTG {
		h := hash64("tg-" + strconv.Itoa(tg))
		idx, _ := slices.BinarySearchFunc(ring, h, func(p point, target uint64) int {
			switch {
			case p.h < target:
				return -1
			case p.h > target:
				return 1
			default:
				return 0
			}
		})
		clear(seen)
		for len(seen) < rf {
			p := ring[idx%len(ring)]
			if _, ok := seen[p.node]; !ok {
				seen[p.node] = struct{}{}
				load[p.node]++
			}
			idx++
		}
	}
	return load, time.Since(start)
}

// rendezvousPlacement places rf replicas via weighted rendezvous (HRW) hashing:
// for each target group the rf highest-scoring nodes are chosen.
func rendezvousPlacement(numTG, numNodes, rf int) (load []int, dur time.Duration) {
	seed := make([]uint64, numNodes)
	for n := range numNodes {
		seed[n] = hash64("dpl-" + strconv.Itoa(n))
	}
	load = make([]int, numNodes)
	type ns struct {
		score uint64
		node  int
	}
	top := make([]ns, 0, rf)
	start := time.Now()
	for tg := range numTG {
		tgh := hash64("tg-" + strconv.Itoa(tg))
		top = top[:0]
		for n := range numNodes {
			s := mix(seed[n], tgh)
			if len(top) < rf {
				top = append(top, ns{s, n})
				for i := len(top) - 1; i > 0 && top[i].score < top[i-1].score; i-- {
					top[i], top[i-1] = top[i-1], top[i]
				}
			} else if s > top[0].score {
				top[0] = ns{s, n}
				for i := 0; i+1 < len(top) && top[i].score > top[i+1].score; i++ {
					top[i], top[i+1] = top[i+1], top[i]
				}
			}
		}
		for _, t := range top {
			load[t.node]++
		}
	}
	return load, time.Since(start)
}

// TestPlacementComparison writes /tmp/placement_compare.csv with uniformity and
// build time for the two-step algorithm and consistent-hashing baselines across
// a range of node counts (fixed target-group count).
func TestPlacementComparison(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping comparison in short mode")
	}
	const numTG, rf = 10000, 3
	nodeCounts := []int{4, 5, 8, 16, 32, 64, 128, 256, 500}

	f, err := os.Create("/tmp/placement_compare.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"algo", "nodes", "time_ms", "cv", "max_overhead_pct"})

	emit := func(algo string, m int, dur time.Duration, load []int) {
		cv, mo := loadStats(load)
		_ = w.Write([]string{algo, strconv.Itoa(m), fmt.Sprintf("%.3f", msf(dur)),
			fmt.Sprintf("%.4f", cv), fmt.Sprintf("%.2f", mo*100)})
		fmt.Printf("%-14s nodes=%-4d time=%7.3fms cv=%.3f maxOver=%5.1f%%\n", algo, m, msf(dur), cv, mo*100)
	}

	for _, m := range nodeCounts {
		fmt.Printf("--- nodes=%d ---\n", m)
		load, dur := twoStepLoad(numTG, m, rf)
		emit("two-step", m, dur, load)
		for _, v := range []int{1, 200} {
			l, d := ringPlacement(numTG, m, rf, v)
			emit("ring-ch-v"+strconv.Itoa(v), m, d, l)
		}
		l, d := rendezvousPlacement(numTG, m, rf)
		emit("rendezvous", m, d, l)
	}
	t.Log("wrote /tmp/placement_compare.csv")
}

// TestReconcilerScaling writes /tmp/recon_scaling.csv with two-step placement
// build time as a function of target-group count at a fixed node count.
func TestReconcilerScaling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping scaling in short mode")
	}
	const nodes, rf = 500, 3
	tgCounts := []int{1000, 5000, 10000, 25000, 50000, 100000, 200000}

	f, err := os.Create("/tmp/recon_scaling.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"n", "time_ms"})

	for _, n := range tgCounts {
		// median of 5 runs
		samples := make([]float64, 5)
		for i := range samples {
			_, dur := twoStepLoad(n, nodes, rf)
			samples[i] = msf(dur)
		}
		slices.Sort(samples)
		med := samples[len(samples)/2]
		_ = w.Write([]string{strconv.Itoa(n), fmt.Sprintf("%.3f", med)})
		fmt.Printf("tg=%-7d time=%7.3fms\n", n, med)
	}
	t.Log("wrote /tmp/recon_scaling.csv")
}
