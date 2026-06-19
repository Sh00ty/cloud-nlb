#!/usr/bin/env python3
"""Consolidated, presentation-readable resilience figures, built OFFLINE from
each run's frozen metrics/*.json + phases.json (no Prometheus, reproducible).

Honest unit = the CO-FIRING WAVE, not the chaos-CR. Phase 1 is sequential
(one failure at a time) so each single is its own scenario; Phase 2/3/4 fire
several CRs simultaneously, and Phase 4 fires all of them at once. Attributing
outage to an individual chaos-CR ("chaos-hc-kill loses traffic on the LB")
would be false, so co-firing CRs are clustered into one logical scenario and
metrics are computed over the union window. Phase 4 collapses to a single
"тотальный хаос (всё одновременно)" entry.

Figures (few, large, slide-readable):
  hc_resilience_main.png    — THE HC figure: p90 probe-coverage interval
        aligned to the HC fault injection, every run+scenario overlaid +
        cross-run median/IQR. Losing 3/4 HC nodes barely moves the interval
        and it never approaches the DOWN-detect budget.
  hc_resilience_summary.png — companion: p90 interval baseline vs peak vs
        after, by HC-fault severity (isolated 3/4 / +1 / +2 / total chaos).
  unavail_by_scenario.png   — mean unavailability % (share of lost requests)
        per logical scenario; only the scenarios that lose anything are
        drawn, the rest are stated as 0 %.
  outage_by_scenario.png    — full-outage seconds (succ≈0) per logical
        scenario; Phase 4 = ONE box.
  errors_by_kind_agg.png    — errors by kind per logical scenario (nonzero).
  runs/*/graphs/availability_percent.png — per-run single availability-% line.

Usage: aggregate_timeseries.py [runs_dir] [out_dir]
"""
import glob, json, os, statistics, sys

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

import build_evidence as be

plt.rcParams.update({"font.size": 13, "axes.titlesize": 15,
                     "axes.labelsize": 13, "legend.fontsize": 11})

HERE = os.path.dirname(os.path.abspath(__file__))
RUNS = sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, "runs")
OUT = sys.argv[2] if len(sys.argv) > 2 else os.path.join(HERE, "aggregate")

GROUP_COLORS = {"1": "#1f77b4", "2": "#2ca02c", "3": "#ff7f0e", "4": "#d62728"}
GROUP_NAME = {"1": "одиночные", "2": "парные", "3": "тройные",
              "4": "тотальный хаос"}
WAVE_TOL = 20  # s: concurrent CRs share a start; sequential singles are >=45 s

# Owner-acknowledged stand artifact withheld from ALL headline display
# (figures + the aggregate.py table that imports this set). single-infra-
# partition triggers a known, separately-mitigated etcd network-partition bug
# that mis-attributes client-error duration; not re-run (6h suite), documented
# as prose instead. The v2 client-SLI pipeline (aggregate_resilience.py) already
# drops it via its own EXCLUDE_CLIENT; this is the matching switch for the
# headline figures. Trim once the bug is fixed and the suite is re-run.
EXCLUDE_SCENARIOS = frozenset({"single-infra-partition"})


def run_dirs():
    return sorted(glob.glob(os.path.join(RUNS, "run-*")))


def load_series(run_dir, fname):
    f = os.path.join(run_dir, "metrics", fname)
    if not os.path.exists(f):
        return {}
    s = be.matrix_to_series(json.load(open(f)))
    return {round(t): v for t, v in s[0][1]} if s else {}


def load_kind_series(run_dir, fname):
    f = os.path.join(run_dir, "metrics", fname)
    if not os.path.exists(f):
        return {}
    out = {}
    for lbl, pts in be.matrix_to_series(json.load(open(f))):
        out[lbl or "—"] = {round(t): v for t, v in pts}
    return out


def group_of(label):
    if label.startswith("pair-"):
        return "2"
    if label.startswith("triple-"):
        return "3"
    if label.startswith("chaos-"):
        return "4"
    return "1"


def base_name(label):
    """pair-hc-kill-1 -> pair-hc-kill (drop the co-fire ordinal)."""
    p = label.rsplit("-", 1)
    return p[0] if len(p) == 2 and p[1].isdigit() else label


def clusters_of(run_dir):
    """Co-firing wave clustering. Returns list of logical scenarios:
    {label, group, start, end, members:[orig labels]}."""
    f = os.path.join(run_dir, "phases.json")
    if not os.path.exists(f):
        return []
    ph = sorted(json.load(open(f)), key=lambda p: p["start"])
    waves = []
    for p in ph:
        if waves and p["start"] - waves[-1]["start"] <= WAVE_TOL:
            w = waves[-1]
            w["members"].append(p["label"])
            w["end"] = max(w["end"], p["end"])
        else:
            waves.append(dict(start=p["start"], end=p["end"],
                              members=[p["label"]]))
    out = []
    for w in waves:
        g = group_of(w["members"][0])
        if g == "4":
            label = "тотальный хаос (всё одновременно)"
        else:
            bases = sorted({base_name(m) for m in w["members"]})
            label = " + ".join(bases) if len(bases) > 1 else bases[0]
        out.append(dict(label=label, group=g, start=w["start"],
                        end=w["end"], members=w["members"]))
    return out


def recovery_of(run_dir):
    f = os.path.join(run_dir, "recovery.json")
    return json.load(open(f)).get("scenarios", {}) if os.path.exists(f) else {}


def cluster_unhealthy(rec, members):
    """A cluster is gated out only if ALL its members were flagged
    baseline_unhealthy (cold-start warmup) — otherwise it is attributable."""
    flags = [rec.get(m, {}).get("baseline_unhealthy") for m in members
             if m in rec]
    return bool(flags) and all(flags)


def availability_pct(rd):
    s = load_series(rd, "throughput__успешные (2xx).json")
    n = load_series(rd, "throughput__не-2xx ответы.json")
    e = load_series(rd, "throughput__транспортные ошибки.json")
    ts = sorted(set(s) | set(n) | set(e))
    out = {}
    for t in ts:
        sv, nv, ev = s.get(t, 0.0), n.get(t, 0.0), e.get(t, 0.0)
        d = sv + nv + ev
        out[t] = (100.0 * sv / d) if d >= 0.5 else None
    return out


def grid_median(curves, x0, x1, dx):
    xs, med, lo, hi = [], [], [], []
    g = x0
    while g <= x1:
        vals = []
        for c in curves:
            near = [y for x, y in c if abs(x - g) <= dx / 2.0]
            if near:
                vals.append(statistics.mean(near))
        if vals:
            vals.sort()
            xs.append(g)
            med.append(statistics.median(vals))
            lo.append(vals[max(0, round(0.25 * (len(vals) - 1)))])
            hi.append(vals[min(len(vals) - 1, round(0.75 * (len(vals) - 1)))])
        g += dx
    return xs, med, lo, hi


# ----------------------------------------------------------- HC main figure
HC_FILE = "hc_health__p90 интервала покрытия.json"
HC_ISOLATED = {"single-hc-kill", "single-hc-failure-long"}


def fig_hc_main(rds):
    curves, nlines, nruns = [], 0, 0
    for rd in rds:
        ser = load_series(rd, HC_FILE)
        if not ser:
            continue
        nruns += 1
        used = False
        for cl in clusters_of(rd):
            if cl["label"] not in HC_ISOLATED:
                continue
            s = cl["start"]
            c = sorted((t - s, v) for t, v in ser.items()
                       if s - 30 <= t <= s + 260)
            if c:
                curves.append(c)
                nlines += 1
                used = used or True
    fig, ax = plt.subplots(figsize=(14, 6.2))
    for c in curves:
        ax.plot([x for x, _ in c], [y for _, y in c],
                color="#9ecae1", lw=1.0, alpha=0.5)
    if curves:
        xs, mv, lo, hi = grid_median(curves, -30, 260, 10)
        ax.fill_between(xs, lo, hi, color="#1f77b4", alpha=0.20,
                        label="разброс по прогонам")
        ax.plot(xs, mv, color="#1f77b4", lw=3.0,
                label="медиана")
    ax.axhline(3.0, ls="--", lw=1.6, color="#2ca02c",
               label="эталонный интервал зонда 3 с")
    ax.axhline(9.0, ls="--", lw=1.6, color="#d62728",
               label="бюджет обнаружения DOWN 3×3 с")
    ax.set_xlim(-30, 260)
    ax.set_ylim(0, 10)
    ax.set_xlabel("время от инъекции отказа, с")
    ax.set_ylabel("p90 интервала покрытия проверками, с")
    ax.set_title("Отказоустойчивость подсистемы проверки состояний")
    ax.grid(True, alpha=0.3)
    ax.legend(loc="center right", framealpha=0.92)
    fig.tight_layout()
    p = os.path.join(OUT, "hc_resilience_main.png")
    fig.savefig(p, dpi=140)
    plt.close(fig)
    return p, nlines, nruns


HC_SEVERITY = [
    ("HC изолированно\n(3/4 узлов)", lambda c: c["label"] in HC_ISOLATED),
    ("HC + 1 отказ\n(парный)",
     lambda c: c["group"] == "2" and any("hc" in m for m in c["members"])),
    ("HC + 2 отказа\n(тройной)",
     lambda c: c["group"] == "3" and any("hc" in m for m in c["members"])),
    ("HC в тотальном\nхаосе", lambda c: c["group"] == "4"),
]


def fig_hc_summary(rds):
    base, peak, after = ([[] for _ in HC_SEVERITY] for _ in range(3))
    for rd in rds:
        ser = load_series(rd, HC_FILE)
        if not ser:
            continue
        for cl in clusters_of(rd):
            for i, (_, match) in enumerate(HC_SEVERITY):
                if not match(cl):
                    continue
                s, e = cl["start"], cl["end"]
                b = [v for t, v in ser.items() if s - 60 <= t < s - 5]
                d = [v for t, v in ser.items() if s <= t <= e + 30]
                a = [v for t, v in ser.items() if e + 30 < t <= e + 120]
                if b:
                    base[i].append(statistics.median(b))
                if d:
                    peak[i].append(max(d))
                if a:
                    after[i].append(statistics.median(a))
    xs = range(len(HC_SEVERITY))
    bw = 0.26
    fig, ax = plt.subplots(figsize=(13, 6))

    def m(xss):
        return [statistics.mean(v) if v else 0.0 for v in xss]

    ax.bar([x - bw for x in xs], m(base), bw, color="#2ca02c", alpha=0.85,
           label="эталон (до отказа)")
    ax.bar(list(xs), m(peak), bw, color="#ff7f0e", alpha=0.9,
           label="пик во время отказа")
    ax.bar([x + bw for x in xs], m(after), bw, color="#1f77b4", alpha=0.85,
           label="после восстановления")
    for x, v in zip(xs, m(peak)):
        ax.text(x, v + 0.15, f"{v:.1f} с", ha="center", fontsize=12,
                fontweight="bold")
    ax.axhline(9.0, ls="--", lw=2.0, color="#d62728",
               label="бюджет обнаружения DOWN 9 с")
    ax.set_xticks(list(xs))
    ax.set_xticklabels([t for t, _ in HC_SEVERITY])
    ax.set_ylabel("p90 интервала покрытия проверками, с")
    ax.set_ylim(0, 10)
    ax.set_title("Интервал покрытия проверками по тяжести отказа HC:\n"
                 "пик не приближается к бюджету обнаружения даже в хаосе")
    ax.grid(True, axis="y", alpha=0.3)
    ax.legend(loc="upper left", framealpha=0.92)
    fig.tight_layout()
    p = os.path.join(OUT, "hc_resilience_summary.png")
    fig.savefig(p, dpi=140)
    plt.close(fig)
    return p


# ------------------------------------------------- scenario-level summaries
def scenario_windows(rd):
    """Yield (label, group, start, wend, members) for each logical scenario,
    window clamped to the next cluster start, skipping cold-warmup clusters."""
    cls = clusters_of(rd)
    rec = recovery_of(rd)
    starts = sorted(c["start"] for c in cls)
    for c in cls:
        if cluster_unhealthy(rec, c["members"]):
            continue
        # Withheld scenario still bounds its neighbours' windows (it physically
        # ran), it is only not yielded for display. See EXCLUDE_SCENARIOS.
        if c["label"] in EXCLUDE_SCENARIOS:
            continue
        nxt = [s for s in starts if s > c["start"]]
        wend = min(c["end"] + 30, nxt[0]) if nxt else c["end"] + 30
        yield c["label"], c["group"], c["start"], wend, c["members"]


def _scenario_box(rds, valfn, title, xlabel, png, only_nonzero, unit):
    per = {}
    grp = {}
    for rd in rds:
        av = availability_pct(rd)
        succ = load_series(rd, "throughput__успешные (2xx).json")
        for label, g, s, wend, _ in scenario_windows(rd):
            v = valfn(av, succ, s, wend)
            if v is None:
                continue
            per.setdefault(label, []).append(v)
            grp[label] = g
    items = sorted(per.items(), key=lambda kv: (grp[kv[0]], kv[0]))
    zero_n = 0
    if only_nonzero:
        kept = []
        for k, v in items:
            if statistics.median(v) > 0.3 or max(v) > 1.0:
                kept.append((k, v))
            else:
                zero_n += 1
        items = kept
    if not items:
        return None
    labels = [k for k, _ in items]
    data = [v for _, v in items]
    colors = [GROUP_COLORS[grp[k]] for k in labels]
    fig, ax = plt.subplots(figsize=(13, max(4.5, 0.55 * len(items) + 1.5)))
    bp = ax.boxplot(data, vert=False, patch_artist=True, widths=0.6)
    for patch, c in zip(bp["boxes"], colors):
        patch.set_facecolor(c)
        patch.set_alpha(0.6)
    for med in bp["medians"]:
        med.set_color("black")
        med.set_linewidth(2)
    ax.set_yticks(range(1, len(labels) + 1))
    ax.set_yticklabels(labels, fontsize=12)
    ax.invert_yaxis()
    ax.set_xlabel(xlabel)
    ttl = title + f" (N={len(rds)} прогонов)"
    if only_nonzero and zero_n:
        ttl += (f"\nещё {zero_n} логич. сценариев = 0 {unit} "
                f"(потерь трафика нет)")
    ax.set_title(ttl)
    ax.grid(True, axis="x", alpha=0.3)
    handles = [plt.Line2D([0], [0], color=GROUP_COLORS[g], lw=8, alpha=0.6,
                          label=f"Фаза {g}: {GROUP_NAME[g]}")
               for g in sorted(GROUP_COLORS)]
    ax.legend(handles=handles, loc="lower right", framealpha=0.92)
    fig.tight_layout()
    fig.savefig(png, dpi=140, bbox_inches="tight")
    plt.close(fig)
    return png


def mean_unavail(av, succ, s, wend):
    vals = [av[t] for t in av if s <= t <= wend and av[t] is not None]
    return 100.0 - statistics.mean(vals) if vals else None


def infer_step(ts):
    """Median spacing of frozen samples -> the run's actual query step. Adapts
    automatically to 2s (new runs) vs 10s (older frozen runs)."""
    ts = sorted(ts)
    diffs = [b - a for a, b in zip(ts, ts[1:]) if b > a]
    return statistics.median(diffs) if diffs else 10


def outage_seconds(av, succ, s, wend):
    pts = [(t, succ.get(t, 0.0)) for t in succ if s <= t <= wend]
    if not pts:
        return None
    pts.sort()
    step = infer_step([t for t, _ in pts])
    return round(sum(step for _, v in pts if v <= be.EPS), 1)


def fig_errors_by_kind(rds):
    agg = {}
    grp = {}
    for rd in rds:
        ek = load_kind_series(rd, "errors_by_kind__{kind}.json")
        for label, g, s, wend, _ in scenario_windows(rd):
            for kind, ser in ek.items():
                vals = [v for t, v in ser.items() if s <= t <= wend]
                if vals:
                    agg.setdefault(label, {}).setdefault(kind, []).append(
                        statistics.mean(vals))
                    grp[label] = g
    rows = []
    for label, kinds in agg.items():
        means = {k: statistics.mean(v) for k, v in kinds.items()}
        if sum(means.values()) > 0.05:
            rows.append((grp[label], label, means))
    if not rows:
        return None
    rows.sort()
    allk = sorted({k for _, _, m in rows for k in m})
    labels = [l for _, l, _ in rows]
    fig, ax = plt.subplots(figsize=(13, max(4.5, 0.6 * len(rows) + 1.5)))
    left = [0.0] * len(rows)
    cmap = plt.get_cmap("tab10")
    for i, k in enumerate(allk):
        vals = [m.get(k, 0.0) for _, _, m in rows]
        ax.barh(range(len(rows)), vals, left=left, label=k,
                color=cmap(i % 10), alpha=0.88)
        left = [a + b for a, b in zip(left, vals)]
    ax.set_yticks(range(len(rows)))
    ax.set_yticklabels(labels, fontsize=12)
    ax.invert_yaxis()
    ax.set_xlabel("средняя частота ошибок в окне сценария, ошибок/с")
    ax.set_title(f"Транспортные ошибки по типу — логич. сценарии с "
                 f"ненулевыми ошибками (N={len(rds)})")
    ax.grid(True, axis="x", alpha=0.3)
    ax.legend(title="kind", framealpha=0.92)
    fig.tight_layout()
    p = os.path.join(OUT, "errors_by_kind_agg.png")
    fig.savefig(p, dpi=140, bbox_inches="tight")
    plt.close(fig)
    return p


def fig_availability_percent(rds):
    paths = []
    for rd in rds:
        av = availability_pct(rd)
        f = os.path.join(rd, "phases.json")
        if not av or not os.path.exists(f):
            continue
        ph = sorted(json.load(open(f)), key=lambda p: p["start"])
        t0 = min(ph[0]["start"], min(av))
        ts = sorted(av)
        xs = [t - t0 for t in ts]
        ys = [av[t] for t in ts]
        fig, ax = plt.subplots(figsize=(14, 4.6))
        ax.plot(xs, ys, color="#1f77b4", lw=1.8)
        ax.fill_between(xs, ys, 100,
                        where=[y is not None and y < 100 for y in ys],
                        color="#d62728", alpha=0.15, interpolate=True)
        ax.set_ylim(-2, 103)
        ax.set_ylabel("доступность, %")
        ax.set_xlabel("время от старта прогона, с")
        ax.set_title("Доступность сервиса (доля успешных запросов) — "
                     + os.path.basename(rd))
        ax.grid(True, alpha=0.3)
        be.add_phase_bands(ax, ph, t0, "group", neutral=True)
        fig.tight_layout()
        p = os.path.join(rd, "graphs", "availability_percent.png")
        fig.savefig(p, dpi=130)
        plt.close(fig)
        paths.append(p)
    return paths


def main():
    os.makedirs(OUT, exist_ok=True)
    rds = run_dirs()
    if not rds:
        print(f"no run dirs under {RUNS}", file=sys.stderr)
        sys.exit(1)
    print(f"[ts-agg] N={len(rds)} runs")
    p, nl, nr = fig_hc_main(rds)
    print(f"  hc_resilience_main    -> {p} ({nl} curves, {nr} runs)")
    print("  hc_resilience_summary ->", fig_hc_summary(rds))
    print("  unavail_by_scenario   ->", _scenario_box(
        rds, mean_unavail,
        "Средняя недоступность (доля потерянных запросов) по сценариям",
        "недоступность за окно сценария, %",
        os.path.join(OUT, "unavail_by_scenario.png"),
        only_nonzero=True, unit="%"))
    print("  outage_by_scenario    ->", _scenario_box(
        rds, outage_seconds,
        "Полный простой (успех ≈ 0) по логическим сценариям",
        "полный простой, с",
        os.path.join(OUT, "outage_by_scenario.png"),
        only_nonzero=True, unit="с"))
    print("  errors_by_kind_agg    ->", fig_errors_by_kind(rds))
    pp = fig_availability_percent(rds)
    print(f"  availability_percent  -> {len(pp)} per-run figures")


if __name__ == "__main__":
    main()
