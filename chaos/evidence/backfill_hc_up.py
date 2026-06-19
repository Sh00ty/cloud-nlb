#!/usr/bin/env python3
"""Backfill the live hc-worker count into ALREADY-FROZEN runs while Prometheus
retention (7d) still covers their windows, so the HC coverage-vs-health figure
can correlate the probe-coverage interval with how many HC nodes were actually
down — for the OLD runs too, WITHOUT re-running the 95 min suite.

ADDITIVE and idempotent: writes only metrics/avail__live_hc.json (the same key
build_evidence freezes for new runs, the same PromQL). Existing metrics/*.json,
recovery.json, graphs and aggregates are never touched. Each run's time grid is
taken from its own frozen hc_health p90 file, so the hc-up samples land on
EXACTLY the same timestamps as the coverage-interval series.

Usage: kubectl context must point at the chaos stand (colima).
  backfill_hc_up.py [runs_dir]    (default chaos/evidence/runs)
"""
import glob, json, os, statistics, sys

import build_evidence as be   # reuse prom_query_range + the live_hc PromQL

HERE = os.path.dirname(os.path.abspath(__file__))
RUNS = sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, "runs")
KEY = "live_hc"
GRID_FILE = "hc_health__p90 интервала покрытия.json"


def grid(run_dir):
    """(start, end, step) from this run's frozen coverage-interval file."""
    f = os.path.join(run_dir, "metrics", GRID_FILE)
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
    if KEY not in be.AVAIL_SIGNALS:
        print(f"build_evidence.AVAIL_SIGNALS lacks '{KEY}'; update the "
              "collector first", file=sys.stderr)
        sys.exit(1)
    ok = skipped = 0
    for rd in runs:
        name = os.path.basename(rd)
        g = grid(rd)
        if not g:
            print(f"[skip] {name}: no usable {GRID_FILE} grid")
            skipped += 1
            continue
        start, end, step = g
        out = os.path.join(rd, "metrics", f"avail__{KEY}.json")
        try:
            resp = be.prom_query_range(be.AVAIL_SIGNALS[KEY], start, end, step)
            s = be.matrix_to_series(resp)
            npts = len(s[0][1]) if s else 0
            if npts == 0:
                raise RuntimeError(f"{KEY}: 0 points (retention gap?)")
            vmax = max(v for _, v in s[0][1])
            with open(out, "w") as fh:
                json.dump(resp, fh)
        except Exception as e:
            print(f"[FAIL] {name}: {e} — left untouched, no partial freeze "
                  "trusted")
            if os.path.exists(out):
                os.remove(out)
            skipped += 1
            continue
        print(f"[ok]   {name}: live_hc frozen [{start}..{end}] step={step}s "
              f"({npts} pts, max replicas seen = {vmax:g})")
        ok += 1
    print(f"\nbackfilled {ok}/{len(runs)} runs, skipped {skipped}.")


if __name__ == "__main__":
    main()
