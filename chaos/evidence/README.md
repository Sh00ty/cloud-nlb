# Chaos evidence pipeline

Turns a chaos-mesh run into committable, thesis-grade evidence: graphs with the
fault timeline overlaid + a per-phase downtime table. Built because chaos-mesh
itself draws no graphs and its phase boundaries (k8s events, pod logs) expire
within ~1 h, while the metrics live in Prometheus.

## Design

- **Metrics are not live-watched.** Prometheus retains 7 d / 9 GB; every run is
  additionally *frozen* into `runs/run-<UTC>/metrics/*.json` right after it
  finishes, so evidence survives TSDB rotation regardless.
- **The fault timeline is the only ephemeral piece**, so `poll_timeline.sh`
  records chaos-mesh CR state every 5 s into `timeline.jsonl` (durable). Phase
  bands are derived offline from that raw sample stream.
- Prometheus is queried via `kubectl exec` (no port-forward).

## Use

```bash
make setup     # once: venv + pinned matplotlib/numpy
make dryrun    # ~2-3 min: validates the whole pipeline on one short fault
make run       # full 95 min suite; launch in background for the real runs
```

Each run produces `runs/run-<UTC>/`:

| file | kept in git | what |
|---|---|---|
| `graphs/*.png` | yes | 5 must-have graphs, phase bands overlaid |
| `summary.md` | yes | per-phase downtime table + methodology caveat |
| `phases.json` | yes | derived `{label,kind,start,end}` |
| `run.json` | yes | run window metadata |
| `metrics/*.json` | no (large) | raw Prometheus `query_range` responses |
| `timeline.jsonl` | no (large) | raw 5 s poll samples |

## Must-have graphs

`throughput` (success vs non-2xx vs transport errors), `latency`
(p50/p95/p99), `errors_by_kind` (stacked), `hc_health` (coverage p90 + max
staleness), `backends` (live backends serving traffic).

Edit the `CATALOG` in `build_evidence.py` to add the nice-to-have graphs
(scheduler scaling, end-to-end propagation CDF, placement balance).

## Caveat (state it in the НИР)

Time resolution ≈ scrape interval (5 s) + rate window (30 s–2 m). Downtime
shorter than that is not distinguishable; reported numbers are conservative
upper bounds, not sub-second measurements.
