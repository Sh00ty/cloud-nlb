#!/usr/bin/env python3
"""Backfill the SHARP probe-coverage p90 (rate[15s] and rate[10s]) into
ALREADY-FROZEN runs while Prometheus retention (7d) still covers their
windows, so coverage recovery is resolvable near scrape resolution instead
of buried under the rate[2m] smoothing of the hc_health graph.

ADDITIVE and idempotent: writes only metrics/avail__cov_p90_{15s,10s}.json
(the same keys build_evidence freezes for new runs, the same PromQL). The
[2m] hc_health series, every other metrics/*.json, graphs and aggregates are
never touched. Each run's time grid is taken from its own frozen
hc_health p90 file, so the sharp samples land on EXACTLY the same timestamps
as the [2m] coverage series and live_hc.

Usage: kubectl context must point at the chaos stand (colima).
  backfill_cov_sharp.py [runs_dir]    (default chaos/evidence/runs)
"""
import glob, json, os, statistics, sys

import build_evidence as be

HERE = os.path.dirname(os.path.abspath(__file__))
RUNS = sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, "runs")
KEYS = ("cov_p90_15s", "cov_p90_10s")
GRID_FILE = "hc_health__p90 интервала покрытия.json"


def grid(run_dir):
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
    missing = [k for k in KEYS if k not in be.AVAIL_SIGNALS]
    if missing:
        print(f"build_evidence.AVAIL_SIGNALS lacks {missing}; update the "
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
        try:
            npts = {}
            for key in KEYS:
                resp = be.prom_query_range(be.AVAIL_SIGNALS[key],
                                           start, end, step)
                s = be.matrix_to_series(resp)
                n = len(s[0][1]) if s else 0
                if n == 0:
                    raise RuntimeError(f"{key}: 0 points (retention gap?)")
                with open(os.path.join(rd, "metrics",
                                       f"avail__{key}.json"), "w") as fh:
                    json.dump(resp, fh)
                npts[key] = n
        except Exception as e:
            print(f"[FAIL] {name}: {e} — left untouched, no partial freeze "
                  "trusted")
            for key in KEYS:
                p = os.path.join(rd, "metrics", f"avail__{key}.json")
                if os.path.exists(p):
                    os.remove(p)
            skipped += 1
            continue
        print(f"[ok]   {name}: sharp p90 frozen [{start}..{end}] "
              f"step={step}s ({npts})")
        ok += 1
    print(f"\nbackfilled {ok}/{len(runs)} runs, skipped {skipped}.")


if __name__ == "__main__":
    main()
