#!/usr/bin/env python3
"""HC-failure resilience figure, built from the CORRECTED frozen recovery.json
across all runs (offline, no Prometheus).

The suite has NO single-hc-node scenario: hc-worker is a 4-replica StatefulSet
with HC_REPLICATION_FACTOR=2, and the hc scenarios kill `random-max-percent
75` (up to 3 of 4) or `mode: all` (4 of 4), optionally combined with other
failures. So the x-axis is HC fault severity actually tested:

  single-hc (75% / long 4m, hc-only)  -> isolated loss of 3/4 hc
  pair-hc   (hc loss + 1 more failure)
  triple-hc (hc loss + 2 more)
  chaos-hc  (hc loss inside full system-wide chaos)

Shows: isolated HC loss (even 3/4) has ~0 traffic impact; recovery time grows
only when HC failure is stacked with other failures (and that recovery is
driven by the OTHER failures / reconverge, not by HC liveness).

Usage: hc_resilience_graph.py [runs_dir] [out_png]
"""
import glob, json, os, statistics, sys

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

HERE = os.path.dirname(os.path.abspath(__file__))
RUNS = sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, "runs")
OUT = sys.argv[2] if len(sys.argv) > 2 else os.path.join(HERE, "aggregate", "hc_resilience.png")

# Severity buckets -> the hc scenario labels that belong to them.
BUCKETS = [
    ("HC изолированно\n(3/4 и 4 мин, hc-only)", ["single-hc-kill", "single-hc-failure-long"]),
    ("HC + ещё 1 отказ\n(парный)",              ["pair-hc-kill-1", "pair-hc-kill-2"]),
    ("HC + ещё 2 отказа\n(тройной)",            ["triple-hc-kill-1", "triple-hc-kill-2"]),
    ("HC в тотальном\nхаосе",                   ["chaos-hc-kill"]),
]


def collect():
    runs = sorted(glob.glob(os.path.join(RUNS, "run-*", "recovery.json")))
    per = {}
    for rj in runs:
        sc = json.load(open(rj)).get("scenarios", {})
        for label, m in sc.items():
            if m.get("baseline_unhealthy"):
                continue  # not attributable — corrected gate
            per.setdefault(label, dict(rec=[], out=[]))
            per[label]["rec"].append(m["recover_s"])
            per[label]["out"].append(m["outage_s"])
    return len(runs), per


def main():
    n, per = collect()
    rec_data, out_data, xlabels, ns = [], [], [], []
    for title, labels in BUCKETS:
        rec, out = [], []
        for l in labels:
            if l in per:
                rec += per[l]["rec"]
                out += per[l]["out"]
        rec_data.append(rec or [0])
        out_data.append(out or [0])
        ns.append(len(rec))
        xlabels.append(f"{title}\n(n={len(rec)})")

    fig, (a1, a2) = plt.subplots(1, 2, figsize=(13, 5.2))
    x = range(len(BUCKETS))
    for ax, data, ttl, col in (
        (a1, rec_data, "Время восстановления трафика, с", "#1f77b4"),
        (a2, out_data, "Полный простой трафика, с", "#d62728")):
        bp = ax.boxplot(data, positions=list(x), widths=0.55, patch_artist=True,
                        showmeans=True)
        for b in bp["boxes"]:
            b.set_facecolor(col)
            b.set_alpha(0.5)
        for med in bp["medians"]:
            med.set_color("black")
        ax.set_xticks(list(x))
        ax.set_xticklabels(xlabels, fontsize=8)
        ax.set_ylabel("секунды")
        ax.set_title(ttl)
        ax.grid(True, axis="y", alpha=0.25)
        ax.set_ylim(bottom=-1)

    fig.suptitle(
        f"Отказоустойчивость к потере узлов проверки состояния (HC), N={n} прогонов\n"
        "hc-worker: StatefulSet 4 реплики, фактор репликации проверок 2 — "
        "изолированная потеря 3/4 узлов не влияет на трафик",
        fontsize=10)
    fig.tight_layout(rect=[0, 0, 1, 0.92])
    fig.savefig(OUT, dpi=130)
    plt.close(fig)

    print(f"[hc] N={n} -> {OUT}")
    for (title, labels), r, o, k in zip(BUCKETS, rec_data, out_data, ns):
        rs = f"{statistics.mean(r):.1f}±{statistics.pstdev(r):.1f}" if k else "—"
        os_ = f"{statistics.mean(o):.1f}±{statistics.pstdev(o):.1f}" if k else "—"
        print(f"  {title.splitlines()[0]:32s} n={k:2d}  recover={rs:12s} outage={os_}")


if __name__ == "__main__":
    main()
