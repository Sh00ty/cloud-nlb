package reconciler

import (
	"encoding/csv"
	"fmt"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
)

// This file measures placement churn (replica movements) on topology changes —
// the axis where consistent hashing is traditionally strong. For each algorithm
// it builds a steady-state placement, applies a single event (node death or node
// join), recomputes and counts changed (tg, node) assignment pairs. Run with
// -tags placementfix to measure the corrected two-step variant.

// pairKey identifies one replica assignment.
func pairKey(tg, node string) string { return tg + "|" + node }

func diffPairs(before, after map[string]struct{}) (added, removed int) {
	for k := range after {
		if _, ok := before[k]; !ok {
			added++
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			removed++
		}
	}
	return added, removed
}

// snapshotAlivePairs collects assignment pairs on alive nodes, skipping `exclude`
// so a victim's own (inevitably lost) replicas do not count as churn.
func snapshotAlivePairs(r *Reconciler, exclude models.DataPlaneID) map[string]struct{} {
	s := make(map[string]struct{}, 4096)
	for id, pl := range r.placements {
		if id == exclude || r.dplStatuses[id].State != models.Alive {
			continue
		}
		for tg := range pl.TargetGroups {
			s[pairKey(string(tg), string(id))] = struct{}{}
		}
	}
	return s
}

type churnRow struct {
	algo, event string
	nodes       int
	added       int
	removed     int
	minNeeded   int
	timeMS      float64
}

func (c churnRow) ratio() float64 {
	if c.minNeeded == 0 {
		return 0
	}
	return float64(c.added+c.removed) / float64(c.minNeeded)
}

func twoStepDeathChurn(numTG, m, rf int) churnRow {
	r := buildReconciler(numTG, m, rf)
	runTwoStepPlacement(r)
	victim := models.DataPlaneID("dpl-0")
	victimLoad := len(r.placements[victim].TargetGroups)
	before := snapshotAlivePairs(r, victim)

	st := r.dplStatuses[victim]
	st.State = models.Dead
	r.dplStatuses[victim] = st
	dur := runTwoStepPlacement(r)

	added, removed := diffPairs(before, snapshotAlivePairs(r, victim))
	return churnRow{"two-step", "death", m, added, removed, victimLoad, msf(dur)}
}

func twoStepJoinChurn(numTG, m, rf int) churnRow {
	r := buildReconciler(numTG, m, rf)
	runTwoStepPlacement(r)
	before := snapshotAlivePairs(r, "")

	newID := models.DataPlaneID("dpl-new")
	r.dplStatuses[newID] = DataPlaneStatus{State: models.Alive}
	r.placements[newID] = models.Placement{TargetGroups: map[models.TargetGroupID]struct{}{}}
	dur := runTwoStepPlacement(r)

	added, removed := diffPairs(before, snapshotAlivePairs(r, ""))
	// ideal: fill the new node to its fair share, one remove + one add per move
	minNeeded := 2 * (numTG * rf / (m + 1))
	return churnRow{"two-step", "join", m, added, removed, minNeeded, msf(dur)}
}

// --- consistent-hashing baselines (same hash functions as compare_test.go) ---

type ringPoint struct {
	h    uint64
	node string
}

func buildRingFor(nodeIDs []string, vnodes int) []ringPoint {
	ring := make([]ringPoint, 0, len(nodeIDs)*vnodes)
	for _, id := range nodeIDs {
		for v := range vnodes {
			ring = append(ring, ringPoint{h: hash64(id + "#" + strconv.Itoa(v)), node: id})
		}
	}
	slices.SortFunc(ring, func(a, b ringPoint) int {
		switch {
		case a.h < b.h:
			return -1
		case a.h > b.h:
			return 1
		default:
			return 0
		}
	})
	return ring
}

func ringAssignPairs(ring []ringPoint, numTG, rf int) map[string]struct{} {
	pairs := make(map[string]struct{}, numTG*rf)
	seen := make(map[string]struct{}, rf)
	for tg := range numTG {
		tgs := "tg-" + strconv.Itoa(tg)
		h := hash64(tgs)
		idx, _ := slices.BinarySearchFunc(ring, h, func(p ringPoint, target uint64) int {
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
				pairs[pairKey(tgs, p.node)] = struct{}{}
			}
			idx++
		}
	}
	return pairs
}

func rendezvousAssignPairs(nodeIDs []string, numTG, rf int) map[string]struct{} {
	seeds := make([]uint64, len(nodeIDs))
	for i, id := range nodeIDs {
		seeds[i] = hash64(id)
	}
	pairs := make(map[string]struct{}, numTG*rf)
	type ns struct {
		score uint64
		node  int
	}
	top := make([]ns, 0, rf)
	for tg := range numTG {
		tgs := "tg-" + strconv.Itoa(tg)
		tgh := hash64(tgs)
		top = top[:0]
		for n := range nodeIDs {
			s := mix(seeds[n], tgh)
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
			pairs[pairKey(tgs, nodeIDs[t.node])] = struct{}{}
		}
	}
	return pairs
}

func nodeNames(m int) []string {
	ids := make([]string, m)
	for i := range m {
		ids[i] = "dpl-" + strconv.Itoa(i)
	}
	return ids
}

// pairsWithout filters out pairs assigned to the given node.
func pairsWithout(pairs map[string]struct{}, node string) (rest map[string]struct{}, onNode int) {
	rest = make(map[string]struct{}, len(pairs))
	suffix := "|" + node
	for k := range pairs {
		if len(k) >= len(suffix) && k[len(k)-len(suffix):] == suffix {
			onNode++
			continue
		}
		rest[k] = struct{}{}
	}
	return rest, onNode
}

func chChurn(algo string, assign func([]string) map[string]struct{}, numTG, m, rf int) []churnRow {
	ids := nodeNames(m)
	base := assign(ids)

	// death of dpl-0: recompute without it, diff against survivors' pairs
	victim := ids[0]
	beforeSurv, victimLoad := pairsWithout(base, victim)
	start := time.Now()
	afterDeath := assign(ids[1:])
	deathDur := time.Since(start)
	dAdd, dRem := diffPairs(beforeSurv, afterDeath)

	// join of dpl-new
	start = time.Now()
	afterJoin := assign(append(slices.Clone(ids), "dpl-new"))
	joinDur := time.Since(start)
	jAdd, jRem := diffPairs(base, afterJoin)
	joinMin := 2 * (numTG * rf / (m + 1))

	return []churnRow{
		{algo, "death", m, dAdd, dRem, victimLoad, msf(deathDur)},
		{algo, "join", m, jAdd, jRem, joinMin, msf(joinDur)},
	}
}

// TestPlacementChurn writes /tmp/placement_churn.csv with replica movement counts
// for a single node death and a single node join across node counts.
func TestPlacementChurn(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping churn comparison in short mode")
	}
	const numTG, rf = 10000, 3
	nodeCounts := []int{8, 16, 32, 64, 128, 256, 500}

	f, err := os.Create("/tmp/placement_churn.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"algo", "event", "nodes", "added", "removed", "min_needed", "churn_ratio", "time_ms"})

	emit := func(rows ...churnRow) {
		for _, c := range rows {
			_ = w.Write([]string{c.algo, c.event, strconv.Itoa(c.nodes),
				strconv.Itoa(c.added), strconv.Itoa(c.removed), strconv.Itoa(c.minNeeded),
				fmt.Sprintf("%.3f", c.ratio()), fmt.Sprintf("%.3f", c.timeMS)})
			fmt.Printf("%-14s %-6s nodes=%-4d added=%-6d removed=%-6d min=%-6d ratio=%.3f time=%7.3fms\n",
				c.algo, c.event, c.nodes, c.added, c.removed, c.minNeeded, c.ratio(), c.timeMS)
		}
	}

	for _, m := range nodeCounts {
		fmt.Printf("--- nodes=%d ---\n", m)
		emit(twoStepDeathChurn(numTG, m, rf), twoStepJoinChurn(numTG, m, rf))
		for _, v := range []int{1, 200} {
			vn := v
			emit(chChurn("ring-ch-v"+strconv.Itoa(vn), func(ids []string) map[string]struct{} {
				return ringAssignPairs(buildRingFor(ids, vn), numTG, rf)
			}, numTG, m, rf)...)
		}
		emit(chChurn("rendezvous", func(ids []string) map[string]struct{} {
			return rendezvousAssignPairs(ids, numTG, rf)
		}, numTG, m, rf)...)
	}
	t.Log("wrote /tmp/placement_churn.csv")
}
