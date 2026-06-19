#!/usr/bin/env python3
"""Backfill the raw cumulative client counters into ALREADY-FROZEN runs while
Prometheus retention (7d) still covers their windows, so the v2 aggregation
can use honest ~scrape-step (~10 s) recovery for the old runs too WITHOUT
re-running the 95 min suite.

ADDITIVE and idempotent: writes only metrics/avail__{succ,resp,err}_ctr.json
(the same keys build_evidence freezes for new runs, the same PromQL). Existing
metrics/*.json, recovery.json, graphs and the old aggregate are never touched.
Each run's time grid (start/end/step) is taken from its own frozen
avail__succ_rps.json so the counters align exactly with everything else.

Usage: kubectl context must point at the chaos stand (colima).
  backfill_counters.py [runs_dir]    (default chaos/evidence/runs)
"""
import glob, json, os, statistics, sys

import build_evidence as be   # reuse prom_query_range + the counter PromQL

HERE = os.path.dirname(os.path.abspath(__file__))
RUNS = sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, "runs")
CTR_KEYS = ("succ_ctr", "resp_ctr", "err_ctr")


def grid(run_dir):
    """(start, end, step) from this run's frozen avail__succ_rps.json."""
    f = os.path.join(run_dir, "metrics", "avail__succ_rps.json")
    if not os.path.exists(f):
        return None
    s = be.matrix_to_series(json.load(open(f)))
    pts = s[0][1] if s else []
    if len(pts) < 2:
        return None
    ts = sorted(t for t, _ in pts)
    step = int(statistics.median(round(b - a) for a, b in zip(ts, ts[1:]))) or 10
    return int(ts[0]), int(ts[-1]), step


def main():
    runs = sorted(glob.glob(os.path.join(RUNS, "run-*")))
    if not runs:
        print(f"no run dirs under {RUNS}", file=sys.stderr)
        sys.exit(1)
    missing = [k for k in CTR_KEYS if k not in be.AVAIL_SIGNALS]
    if missing:
        print(f"build_evidence.AVAIL_SIGNALS lacks {missing}; update the "
              "collector first", file=sys.stderr)
        sys.exit(1)
    ok = skipped = 0
    for rd in runs:
        name = os.path.basename(rd)
        g = grid(rd)
        if not g:
            print(f"[skip] {name}: no usable avail__succ_rps.json grid")
            skipped += 1
            continue
        start, end, step = g
        try:
            for key in CTR_KEYS:
                resp = be.prom_query_range(be.AVAIL_SIGNALS[key],
                                           start, end, step)
                s = be.matrix_to_series(resp)
                npts = len(s[0][1]) if s else 0
                if npts == 0:
                    raise RuntimeError(f"{key}: 0 points (retention gap?)")
                with open(os.path.join(rd, "metrics",
                                       f"avail__{key}.json"), "w") as fh:
                    json.dump(resp, fh)
        except Exception as e:
            print(f"[FAIL] {name}: {e} — left untouched, no partial freeze "
                  "trusted")
            for key in CTR_KEYS:                 # avoid a half-written set
                p = os.path.join(rd, "metrics", f"avail__{key}.json")
                if os.path.exists(p):
                    os.remove(p)
            skipped += 1
            continue
        print(f"[ok]   {name}: counters frozen "
              f"[{start}..{end}] step={step}s ({npts} pts)")
        ok += 1
    print(f"\nbackfilled {ok}/{len(runs)} runs, skipped {skipped}. "
          f"Now run: make -C {HERE} resilience")


if __name__ == "__main__":
    main()
