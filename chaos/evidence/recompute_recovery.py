#!/usr/bin/env python3
"""Recompute recovery.json OFFLINE from each run's frozen metrics/avail__*.json
using the corrected build_evidence.recovery_metrics (steady-baseline
attributability gate). No Prometheus, no port-forward — pure re-derivation
from frozen evidence, so it is reproducible and safe to re-run.

Usage:  recompute_recovery.py [runs_dir]   (default chaos/evidence/runs)

Each run's old recovery.json is backed up to recovery.json.pre-gate-<UTC>.
"""
import glob, json, os, shutil, sys, datetime

import build_evidence as be

HERE = os.path.dirname(os.path.abspath(__file__))
RUNS = sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, "runs")


def avail_from_frozen(run_dir):
    """Rebuild build_evidence.fetch_avail's `av` dict from frozen JSON."""
    mdir = os.path.join(run_dir, "metrics")
    raw = {}
    for name in ("live_backends", "live_agents", "succ_rps", "err_rps"):
        f = os.path.join(mdir, f"avail__{name}.json")
        if not os.path.exists(f):
            raw[name] = []
            continue
        with open(f) as fh:
            resp = json.load(fh)
        s = be.matrix_to_series(resp)
        raw[name] = s[0][1] if s else []
    d = {k: {round(t): v for t, v in pts} for k, pts in raw.items()}
    exp_b = max(d["live_backends"].values()) if d.get("live_backends") else 0
    exp_a = max(d["live_agents"].values()) if d.get("live_agents") else 0
    ts = sorted(set().union(*[set(v) for v in d.values()])) if any(d.values()) else []
    return dict(
        ts=ts, exp_b=exp_b, exp_a=exp_a,
        un_b={t: max(0.0, exp_b - d["live_backends"].get(t, exp_b)) for t in ts},
        un_a={t: max(0.0, exp_a - d["live_agents"].get(t, exp_a)) for t in ts},
        succ=d.get("succ_rps", {}), err=d.get("err_rps", {}))


def main():
    runs = sorted(glob.glob(os.path.join(RUNS, "run-*")))
    if not runs:
        print(f"no run dirs under {RUNS}", file=sys.stderr)
        sys.exit(1)
    stamp = datetime.datetime.utcnow().strftime("%Y%m%dT%H%M%SZ")
    for rd in runs:
        rj = os.path.join(rd, "recovery.json")
        tl = os.path.join(rd, "timeline.jsonl")
        if not (os.path.exists(rj) and os.path.exists(tl)):
            print(f"[skip] {os.path.basename(rd)}: missing recovery.json/timeline")
            continue
        old = json.load(open(rj))
        step = old.get("step", 10)
        t0 = old.get("t0")
        av = avail_from_frozen(rd)
        phases = be.derive_phases(tl)
        rmetrics = be.recovery_metrics(av, phases, step)
        skipped = sum(1 for m in rmetrics.values() if m.get("baseline_unhealthy"))
        shutil.copy2(rj, rj + f".pre-gate-{stamp}")
        with open(rj, "w") as fh:
            json.dump(dict(run_dir=os.path.basename(rd), t0=t0, step=step,
                           scenarios=rmetrics), fh, ensure_ascii=False, indent=2)
        print(f"[ok] {os.path.basename(rd)}: scenarios={len(rmetrics)} "
              f"baseline_unhealthy(excluded)={skipped}")


if __name__ == "__main__":
    main()
