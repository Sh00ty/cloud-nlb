#!/usr/bin/env python3
"""Cross-run resilience view built around the TWO client-facing SLIs, OFFLINE
from frozen runs/run-*/metrics (no Prometheus, reproducible). ADDITIVE: writes
only into aggregate/v2/, never touches aggregate.py / aggregate_timeseries.py
outputs.

Primary SLI ............ client error rate (testclient transport errors + non-2xx).
                          The only question that matters: did the client see
                          errors, and did they vanish on their own.
Secondary SLI .......... testserver_hc_coverage_interval p90 vs the 3 s probe
                          reference and the 3x3 s DOWN-detect budget.

Unit of comparison = logical scenario (co-firing wave), reused from
aggregate_timeseries. The attributability gate (baseline_unhealthy / cold-start
warmup) is reused too. A scenario contributes ONE sample per run (the worst of
its co-fire windows in that run).

Figures (aggregate/v2/):
  recovery_when_impacted.png  THE figure. Per logical scenario that produced
        client-visible errors in >=1 run: median time-to-heal (errors back to
        zero and staying there) with IQR and every run's point. Scenarios that
        never produced a client error are stated as one caption line, not
        plotted (absence of impact is the result).
  client_errors_aligned.png   Per impacted scenario, client error rate aligned
        to the fault-injection instant, cross-run median + IQR band. Shows the
        appear-then-self-heal shape repeating across runs.
  hc_coverage_when_impacted.png  Same scenario axis, hc coverage-interval p90:
        cross-run median vs the 3 s / 9 s budgets. Ties the second SLI to the
        same scenarios as the first.
  recovery_summary.md         Narrative + compact scorecard table.

Resolution caveat: the frozen success/error series are rate(...[30s]); a
heal time read from them is an UPPER bound (<= one rate window). The bias is
constant across scenarios, so cross-run medians and the scenario ranking are
sound. Count-based backend / data-plane unavailability is step-resolution
(~10 s) and reported alongside as the un-smeared companion.

Usage: aggregate_resilience.py [runs_dir] [out_dir]
"""
import glob, json, os, statistics, sys

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

import aggregate_timeseries as ats  # reuse clustering / windows / helpers

plt.rcParams.update({"font.size": 13, "axes.titlesize": 15,
                     "axes.labelsize": 13, "legend.fontsize": 11})

HERE = os.path.dirname(os.path.abspath(__file__))
RUNS = sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, "runs")
OUT = sys.argv[2] if len(sys.argv) > 2 else os.path.join(HERE, "aggregate", "v2")

GROUP_COLORS = ats.GROUP_COLORS
GROUP_NAME = ats.GROUP_NAME
# A window is client-impacted only if it loses a non-trivial share of traffic.
# Both thresholds mean roughly the same severity at the ~50 rps test load:
# AVAIL_BAD = lose >=1 % of requests in the window; ERR_EPS aligned to that
# (~1 % of 50 rps), so a single dropped request per 10 s window is NOT flagged
# (that was measurement noise, e.g. one late blip in single-reconciler-kill).
ERR_EPS = 0.5           # err/s; below this the window counts as clean
AVAIL_BAD = 99.0        # availability %, below this the window is client-visible
HC_FILE = ats.HC_FILE

# Fully withheld: appears in NO v2 artifact (figures, hc-coverage, summary,
# counts). single-infra-partition triggers a known, separately-mitigated etcd
# network-partition bug that mis-attributes client-error duration; documented
# as prose, not re-run. Trim once the bug is fixed and the suite is re-run.
EXCLUDE_CLIENT = frozenset({
    "single-infra-partition",
})

# Co-located-stand HC artifacts: on the single colima node (no resource
# isolation) memory-stressing / long-killing hc-worker bleeds node pressure
# into co-scheduled test-server pods, so their CLIENT-error attribution is
# contaminated. They are withheld from the recovery / client-error figures and
# the summary table, but their HC coverage-interval signal is legitimate — it
# is exactly the HC subsystem under HC failure — so they ARE drawn on
# hc_coverage_when_impacted alongside single-hc-kill. Trim once the stand
# isolates resources.
HC_ONLY = frozenset({
    "single-hc-mem-stress",
    "single-hc-failure-long",
})

# Clean scenarios (zero client errors) that must STILL appear on the
# HC-coverage figure: losing 3/4 HC nodes with a flat coverage interval and
# zero client impact is the headline HC-resilience result. Figure-scoped, so
# recovery / client-error figures are unaffected.
HC_INCLUDE = frozenset({"single-hc-kill"})

# Pre-injection baseline window, s. Curves are recorded from -PRE_WINDOW
# (injection at t=0), but on the aligned grids the x-axis is shifted by
# +PRE_WINDOW so every panel starts at 0 (no negative ticks to read). This is
# a pure constant offset, identical for all scenarios: it changes nothing in
# the data or in scenario ordering, the injection moment stays explicitly
# marked by the vertical line (now at x=PRE_WINDOW).
PRE_WINDOW = 30.0


def run_dirs():
    return sorted(glob.glob(os.path.join(RUNS, "run-*")))


def step_of(rd):
    f = os.path.join(rd, "recovery.json")
    if os.path.exists(f):
        return json.load(open(f)).get("step", 10)
    return 10


def unavail_series(rd):
    """Reconstruct count-based backend / data-plane unavailability (un-smeared,
    step-resolution) the same way recompute_recovery does."""
    def s(name):
        return ats.load_series(rd, f"avail__{name}.json")
    lb, la = s("live_backends"), s("live_agents")
    exp_b = max(lb.values()) if lb else 0
    exp_a = max(la.values()) if la else 0
    ts = sorted(set(lb) | set(la))
    un_b = {t: max(0.0, exp_b - lb.get(t, exp_b)) for t in ts}
    un_a = {t: max(0.0, exp_a - la.get(t, exp_a)) for t in ts}
    return un_b, un_a


def median_iqr(xs):
    xs = sorted(xs)
    n = len(xs)
    med = statistics.median(xs)
    lo = xs[max(0, round(0.25 * (n - 1)))]
    hi = xs[min(n - 1, round(0.75 * (n - 1)))]
    return med, lo, hi


def counter_client(rd):
    """Honest ~scrape-step (~10 s) client error rate + availability % derived
    OFFLINE by differencing the raw cumulative counters frozen by the updated
    build_evidence. Returns (err_series, avail_series) or None if a run has no
    counter files (every old run -> None -> rate[30s] fallback). Negative
    deltas (counter reset on pod restart) are clamped to 0."""
    s = ats.load_series(rd, "avail__succ_ctr.json")
    r = ats.load_series(rd, "avail__resp_ctr.json")
    e = ats.load_series(rd, "avail__err_ctr.json")
    if not (s and r and e):
        return None
    ts = sorted(set(s) & set(r) & set(e))
    err, av, prev = {}, {}, None
    for t in ts:
        if prev is not None and t > prev:
            dt = t - prev
            ds = max(0.0, s[t] - s[prev])
            dr = max(0.0, r[t] - r[prev])
            de = max(0.0, e[t] - e[prev])
            err[t] = de / dt
            tot = dr + de
            av[t] = (100.0 * ds / tot) if tot >= 0.5 else None
        prev = t
    return err, av


def collect():
    """Per logical scenario, gather per-run samples. One sample per (run,
    scenario): the worst co-fire window seen in that run. Resolution is
    homogeneous across the set: counter-derived (~10 s) only if EVERY run has
    counters, otherwise rate[30s] for all (comparable, upper bound)."""
    sc = {}                    # label -> dict(group, runs=[per-run sample])
    clean = set()              # scenarios that never showed client errors
    n_runs = 0
    rds = run_dirs()
    use_ctr = bool(rds) and all(counter_client(rd) for rd in rds)
    res_mode = "ctr" if use_ctr else "rate"
    for rd in rds:
        if use_ctr:
            err, av = counter_client(rd)
        else:
            err = ats.load_series(rd, "avail__err_rps.json")
            av = ats.availability_pct(rd)
        succ = ats.load_series(rd, "throughput__успешные (2xx).json")
        hc = ats.load_series(rd, HC_FILE)
        un_b, un_a = unavail_series(rd)
        step = step_of(rd)
        if not (err or av):
            continue
        n_runs += 1
        seen = {}
        for label, g, s0, wend, _ in ats.scenario_windows(rd):
            if label in EXCLUDE_CLIENT:
                continue
            tl = [t for t in sorted(set(err) | set(av)) if s0 <= t <= wend]
            badset = {t for t in tl
                      if err.get(t, 0.0) > ERR_EPS
                      or (av.get(t) is not None and av[t] < AVAIL_BAD)}
            errwin = [err.get(t, 0.0) for t in err if s0 <= t <= wend]
            peak_err = max(errwin) if errwin else 0.0
            impacted = bool(badset)
            # Longest CONTIGUOUS client-error stretch (robust to sporadic late
            # single-sample blips during a long fault; a 1 % flicker is one
            # ~step span, a real outage is a long span). total_bad is the
            # cumulative time the client saw errors anywhere in the window.
            longest = cur = 0.0
            total_bad = 0.0
            end_bad = False
            for j, t in enumerate(tl):
                if t in badset:
                    cur += step
                    total_bad += step
                    longest = max(longest, cur)
                    end_bad = (j == len(tl) - 1)
                else:
                    cur = 0.0
            heal = round(longest, 1)
            total_bad = round(total_bad, 1)
            censored = bool(badset) and end_bad
            outage = ats.outage_seconds(av, succ, s0, wend) or 0.0
            bun = round(sum(step for t in un_b
                            if s0 <= t <= wend and un_b[t] > 0), 1)
            dun = round(sum(step for t in un_a
                            if s0 <= t <= wend and un_a[t] > 0), 1)
            # Clamp curves to the scenario window (wend already stops at the
            # next disaster) so the next scenario's spike does not leak in.
            cend = min(s0 + 300, wend)
            ecurve = sorted((t - s0, err.get(t, 0.0)) for t in err
                            if s0 - 30 <= t <= cend)
            hcurve = sorted((t - s0, hc[t]) for t in hc
                            if s0 - 30 <= t <= cend) if hc else []
            samp = dict(impacted=impacted, heal=heal, censored=censored,
                        total_bad=total_bad, peak_err=round(peak_err, 2),
                        outage=outage, bun=bun, dun=dun,
                        ecurve=ecurve, hcurve=hcurve)
            old = seen.get(label)
            # worst window for this scenario in this run
            if old is None or (samp["impacted"], samp["heal"]) > \
                    (old["impacted"], old["heal"]):
                samp["group"] = g
                seen[label] = samp
        for label, samp in seen.items():
            e = sc.setdefault(label, dict(group=samp["group"], runs=[]))
            e["runs"].append(samp)
    for label, e in sc.items():
        if not any(s["impacted"] for s in e["runs"]):
            clean.add(label)
    return sc, clean, n_runs, res_mode


def order(items):
    return sorted(items, key=lambda kv: (kv[1]["group"], kv[0]))


def shown_impacted(sc, clean):
    """Impacted scenarios drawn on the client-SLI artifacts. HC_ONLY scenarios
    are impacted, but their client errors are co-located-stand contaminated, so
    they are withheld here and surface only on the HC-coverage figure."""
    return order((k, v) for k, v in sc.items()
                 if k not in clean and k not in HC_ONLY)


def all_logical_scenarios():
    """Every logical scenario the suite defines, before any attributability gate
    or display filter. The denominator for the off-chart count: it sweeps in the
    cold-start-gated and HC_ONLY singles that collect() never yields."""
    seen = {}
    for rd in run_dirs():
        for cl in ats.clusters_of(rd):
            seen.setdefault(cl["label"], cl["group"])
    return seen


def res_note(res_mode, step):
    if res_mode == "ctr":
        return f"разрешение шага выборки ~{step:g} с"
    return "верхняя граница, окно rate 30 с"


# ---------------------------------------------------------- headline figure
def fig_recovery(sc, clean, n_runs, res_mode="rate", step=10):
    """Horizontal box-and-whisker ('свечки') per impacted scenario: box = IQR,
    whiskers = min..max across runs, line = median. One short value label
    (median, '*' if a run never recovered)."""
    impacted = shown_impacted(sc, clean)
    if not impacted:
        return None
    labels, data, colors, meds, cens = [], [], [], [], []
    for label, e in impacted:
        heals = [s["heal"] for s in e["runs"] if s["impacted"]]
        labels.append(label)
        data.append(heals)
        colors.append(GROUP_COLORS[e["group"]])
        meds.append(statistics.median(heals))
        cens.append(any(s["censored"] for s in e["runs"] if s["impacted"]))
    fig, ax = plt.subplots(figsize=(12, max(4.0, 0.5 * len(labels) + 1.4)))
    bp = ax.boxplot(data, vert=False, patch_artist=True, widths=0.62,
                    whis=(0, 100), showfliers=False)
    for patch, c in zip(bp["boxes"], colors):
        patch.set_facecolor(c)
        patch.set_alpha(0.55)
    for el in ("whiskers", "caps"):
        for ln, c in zip(bp[el], [c for c in colors for _ in (0, 1)]):
            ln.set_color(c)
    for med in bp["medians"]:
        med.set_color("black")
        med.set_linewidth(2)
    xmax = max((max(d) for d in data if d), default=10)
    for i, (d, m, cn, c) in enumerate(zip(data, meds, cens, colors), start=1):
        ax.text(max(d) + xmax * 0.012, i, f"{m:.0f}s" + ("*" if cn else ""),
                va="center", fontsize=11, fontweight="bold", color=c)
    ax.set_yticks(range(1, len(labels) + 1))
    ax.set_yticklabels(labels, fontsize=11)
    ax.invert_yaxis()
    ax.set_xlabel(f"секунды ({res_note(res_mode, step)})")
    # Off-chart scenarios with no client errors: everything the suite defines,
    # minus the bars drawn here, minus the fully-withheld bug scenario
    # (single-infra-partition DID error and is documented separately, §7.6).
    # This sweeps in the cold-start-gated and stand-artifact (HC_ONLY) singles,
    # which the per-figure filters drop but which saw no balancer-caused errors.
    shown = {k for k, _ in impacted}
    nclean = len(set(all_logical_scenarios()) - shown - EXCLUDE_CLIENT)
    ax.set_title("Длительность клиентских ошибок по бедствиям, где они возникали")
    ax.set_xlim(-2, xmax * 1.08)          # only room for the short "20s" tags
    ax.grid(True, axis="x", alpha=0.3)
    handles = [plt.Line2D([0], [0], color=GROUP_COLORS[g], lw=8, alpha=0.55,
                          label=f"Фаза {g}: {GROUP_NAME[g]}")
               for g in sorted(GROUP_COLORS) if any(
                   v["group"] == g for _, v in impacted)]
    if any(cens):
        handles.append(plt.Line2D([0], [0], marker="$*$", color="w",
                                   markerfacecolor="#333", markersize=11,
                                   label="был прогон без восстановления"))
    # Legend OUT of the plot area (below it), so it neither overlaps the bars
    # nor stretches the x-range.
    fig.legend(handles=handles, loc="lower center",
               bbox_to_anchor=(0.5, 0.055),
               ncol=min(len(handles), 5), framealpha=0.92, fontsize=10)
    if nclean:
        fig.text(0.5, 0.012,
                 f"ещё {nclean} логич. сценариев не дали ни одной ошибки "
                 f"клиента ни в одном прогоне",
                 ha="center", va="bottom", fontsize=9, color="#555")
    fig.tight_layout(rect=(0, 0.12, 1, 1))
    p = os.path.join(OUT, "recovery_when_impacted.png")
    fig.savefig(p, dpi=140, bbox_inches="tight")
    plt.close(fig)
    return p


# --------------------------------------------- pooled recovery survival
def fig_recovery_survival(sc, clean, n_runs, res_mode="rate", step=10):
    """Survival curve S(t) = fraction of client-impacting events whose error
    span exceeded t, over EVERY event (one point per impacted scenario-window
    per run). Pooling many events sidesteps the small-N problem: a per-run
    p90/p99 over 5-7 runs is just ~the max and is meaningless, but the pooled
    distribution has tens of samples and reads honestly as "доля бедствий, где
    ошибки длились дольше t".

    The area under S(t) equals the MEAN error span per event (the meaningful
    "integral" of the curve) and is annotated as a single number. Censored
    events (never recovered in-window) are kept at their lower-bound span and
    flagged, so both the curve and the mean are conservative lower bounds."""
    spans, ncens = [], 0
    for label, e in sc.items():
        if label in clean or label in HC_ONLY:
            continue
        for s in e["runs"]:
            if s["impacted"]:
                spans.append(s["heal"])
                ncens += int(s["censored"])
    if not spans:
        return None
    xs = sorted(spans)
    n = len(xs)
    mean = statistics.mean(xs)                 # == area under S(t)
    med = statistics.median(xs)
    # Pooled quantiles are legitimate here — over `n` events, not the 5-7 runs.
    p90 = xs[min(n - 1, round(0.90 * (n - 1)))]
    # Right-continuous survival staircase: 100 % until the smallest span, then
    # one (n-k)/n step down at each observed value, reaching 0 at the largest.
    step_x = [0.0] + xs
    surv = [100.0] + [(n - (i + 1)) / n * 100.0 for i in range(n)]
    fig, ax = plt.subplots(figsize=(10, 5.5))
    ax.fill_between(step_x, surv, step="post", color="#1f77b4", alpha=0.12,
                    label=f"площадь = среднее {mean:.0f} с")
    ax.step(step_x, surv, where="post", color="#1f77b4", lw=2.4)
    for q, lbl, col in ((med, f"медиана {med:.0f} с", "#2ca02c"),
                        (p90, f"p90 {p90:.0f} с", "#ff7f0e")):
        ax.axvline(q, ls="--", lw=1.6, color=col, label=lbl)
    ax.set_xlim(left=0)
    ax.set_ylim(0, 102)
    ax.set_xlabel(f"t — длительность непрерывных клиентских ошибок, с "
                  f"({res_note(res_mode, step)})")
    ax.set_ylabel("доля событий с ошибками дольше t, %")
    cap = (f"объединено {n} событий с клиентскими ошибками по {n_runs} прогонам"
           + (f"; из них {ncens} не закрылись в окне (нижняя граница)"
              if ncens else ""))
    ax.set_title("Длительность клиентских ошибок: кривая выживания\n"
                 "по всем событиям (площадь под кривой = среднее)")
    ax.grid(True, alpha=0.3)
    ax.legend(loc="upper right", framealpha=0.92)
    fig.text(0.5, -0.02, cap, ha="center", va="top", fontsize=9, color="#555")
    fig.tight_layout()
    p = os.path.join(OUT, "recovery_survival.png")
    fig.savefig(p, dpi=140, bbox_inches="tight")
    plt.close(fig)
    return p


# --------------------------------------------- aligned small-multiples
def _grid(sc, clean, curve_key, ylabel, title, hlines, png, ymax=None,
          extra=frozenset(), drop=frozenset(), mark_injection=True, grid_dx=10):
    # Normally only client-impacted scenarios are drawn. `extra` re-adds
    # specific clean scenarios that are still essential for THIS figure
    # (e.g. single-hc-kill on the HC-coverage figure: it produced zero client
    # errors, which together with a flat coverage interval IS the HC result).
    # `drop` removes scenarios from THIS figure even though they are impacted
    # (e.g. the HC_ONLY stand artifacts off the client-error figure).
    impacted = order((k, v) for k, v in sc.items()
                     if (k not in clean and k not in drop) or k in extra)
    impacted = [(k, v) for k, v in impacted
                if any(s[curve_key] for s in v["runs"])]
    if not impacted:
        return None
    ncol = 3
    nrow = (len(impacted) + ncol - 1) // ncol
    fig, axes = plt.subplots(nrow, ncol, figsize=(5.4 * ncol, 3.0 * nrow),
                             squeeze=False)
    for idx, (label, e) in enumerate(impacted):
        ax = axes[idx // ncol][idx % ncol]
        curves = [s[curve_key] for s in e["runs"] if s[curve_key]]
        for c in curves:
            ax.plot([x + PRE_WINDOW for x, _ in c], [y for _, y in c],
                    color="#9ecae1", lw=0.9, alpha=0.5)
        xs, mv, lo, hi = ats.grid_median(curves, -PRE_WINDOW, 300, grid_dx)
        if xs:
            xss = [x + PRE_WINDOW for x in xs]
            ax.fill_between(xss, lo, hi, color=GROUP_COLORS[e["group"]],
                            alpha=0.20)
            ax.plot(xss, mv, color=GROUP_COLORS[e["group"]], lw=2.6)
        if mark_injection:
            ax.axvline(PRE_WINDOW, color="#333", lw=1.4)
        for yv, lbl, col in hlines:
            ax.axhline(yv, ls="--", lw=1.3, color=col)
        ax.set_title(label, fontsize=11)
        ax.grid(True, alpha=0.3)
        # Individual x-scale: trim the empty tail to this scenario's own
        # window so a short single-* panel is not stretched to 300 s.
        xr = max((x for c in curves for x, _ in c), default=60.0)
        ax.set_xlim(0, max(60.0, 30.0 * (int(xr // 30) + 1)) + PRE_WINDOW)
        if ymax:
            ax.set_ylim(0, ymax)
        else:
            ax.set_ylim(bottom=0)
        if idx % ncol == 0:
            ax.set_ylabel(ylabel)
        if idx // ncol == nrow - 1:
            ax.set_xlabel("время наблюдения, с")
    for j in range(len(impacted), nrow * ncol):
        axes[j // ncol][j % ncol].axis("off")
    leg = [plt.Line2D([0], [0], color="#9ecae1", lw=1.2,
                       label="тонкая линия — отдельный прогон"),
           plt.Line2D([0], [0], color="#444", lw=2.6,
                      label="жирная линия — медиана по прогонам"),
           plt.Rectangle((0, 0), 1, 1, color="#888", alpha=0.30,
                          label="заливка — разброс между прогонами (IQR 25–75 %)")]
    if mark_injection:
        leg.append(plt.Line2D([0], [0], color="#333", lw=1.4,
                              label=f"вертикаль — момент инъекции отказа "
                                    f"(t={PRE_WINDOW:.0f} с)"))
    leg += [plt.Line2D([0], [0], ls="--", lw=1.3, color=col, label=lbl)
            for _, lbl, col in hlines]
    fig.legend(handles=leg, loc="lower center",
               ncol=min(3, len(leg)), framealpha=0.92, fontsize=11)
    fig.suptitle(title, fontsize=15, y=1.0)
    fig.tight_layout(rect=(0, 0.06, 1, 1))
    fig.savefig(png, dpi=135, bbox_inches="tight")
    plt.close(fig)
    return png


# ------------------------------------------------------------ scorecard md
def write_summary(sc, clean, n_runs, res_mode="rate", step=10):
    impacted = shown_impacted(sc, clean)
    cleans = order((k, v) for k, v in sc.items()
                   if k in clean and k not in HC_ONLY)
    p = os.path.join(OUT, "recovery_summary.md")

    def stat(xs):
        if not xs:
            return "—"
        med, lo, hi = median_iqr(xs)
        return f"{med:.0f} (IQR {lo:.0f}..{hi:.0f}, max {max(xs):.0f})"

    with open(p, "w") as fh:
        fh.write("# Кросс-прогонная отказоустойчивость по клиентским SLI\n\n")
        fh.write("Единица сравнения это логический сценарий, то есть волна "
                 "одновременно сработавших chaos-объектов. Каждый прогон даёт "
                 "по одному замеру на сценарий, берётся худшее окно прогона. "
                 "Замеры с нестабильным предотказным базлайном (прогрев "
                 "холодного старта или недовосстановление предыдущего "
                 "сценария) исключены гейтом атрибутируемости.\n\n")
        fh.write("Главный показатель это частота ошибок у клиента, то есть "
                 "транспортные ошибки testclient и ответы не 2xx. Сценарий "
                 "считается давшим клиентский impact, если в окне частота "
                 f"ошибок превышала {ERR_EPS} ошиб/с либо доступность "
                 f"опускалась ниже {AVAIL_BAD:.0f} %. Заголовочная величина "
                 "это самый длинный непрерывный отрезок, в течение которого "
                 "клиент видел ошибки. Она устойчива к редким одиночным "
                 "всплескам в ходе долгого отказа, у которых каждый всплеск "
                 "длится один шаг выборки, тогда как реальная просадка даёт "
                 "длинный отрезок. Рядом приведено суммарное время с ошибками "
                 "за окно.\n\n")
        if res_mode == "ctr":
            fh.write("> Восстановление снято дифференцированием сырого "
                     f"счётчика на шаге выборки около {step:g} с, отрицательные "
                     "приращения при перезапуске подов обнулены. Недоступность "
                     "бэкендов и data-plane посчитана по count-сигналам того "
                     "же разрешения. На стенде data-plane совмещён с "
                     "nlb-agent, поэтому сценарии с агентом задевают и "
                     "форвардинг, оценка консервативна сверху.\n\n")
        else:
            fh.write("> Восстановление снято по rate-ряду с окном 30 с, "
                     "поэтому это верхняя граница, смещение постоянно для всех "
                     "сценариев и не искажает медиану и порядок сценариев. "
                     "Недоступность бэкендов и data-plane посчитана по "
                     f"count-сигналам с разрешением шага выборки около {step:g} с и "
                     "приведена рядом как несмазанная величина. На стенде "
                     "data-plane совмещён с nlb-agent, поэтому сценарии с "
                     "агентом задевают и форвардинг, оценка консервативна "
                     "сверху.\n\n")

        fh.write("## Бедствия с клиентскими ошибками\n\n")
        fh.write("| Сценарий | Гр. | Impact | Пик ошибок, ошиб/с "
                 "| Макс. отрезок ошибок, с | Суммарно ошибок, с | Полный "
                 "простой, с | backend недост., с | dpl/агент недост., с |\n")
        fh.write("|---|:-:|:-:|--:|--|--|--|--|--|\n")
        for label, e in impacted:
            imp = [s for s in e["runs"] if s["impacted"]]
            ni, nt = len(imp), len(e["runs"])
            freq = ("во всех" if ni == nt
                    else "в одном" if ni == 1 else "в части")
            heals = [s["heal"] for s in imp]
            cens = any(s["censored"] for s in imp)
            pk = stat([s["peak_err"] for s in imp])
            hl = stat(heals) + (" ⚠невосст." if cens else "")
            tb = stat([s["total_bad"] for s in imp])
            ou = stat([s["outage"] for s in e["runs"]])
            bu = stat([s["bun"] for s in e["runs"]])
            du = stat([s["dun"] for s in e["runs"]])
            fh.write(f"| {label} | {e['group']} | {freq} | {pk} | {hl} "
                     f"| {tb} | {ou} | {bu} | {du} |\n")

        fh.write("\n## Бедствия без клиентских ошибок\n\n")
        fh.write(f"Следующие {len(cleans)} логических сценариев не дали ни "
                 "одной ошибки у клиента ни в одном из прогонов, поэтому "
                 "восстанавливать там нечего. Это и есть основной результат "
                 "по ним.\n\n")
        for label, e in cleans:
            bu = max((s["bun"] for s in e["runs"]), default=0.0)
            du = max((s["dun"] for s in e["runs"]), default=0.0)
            extra = ""
            if bu or du:
                extra = (f" (внутренняя перебалансировка без потерь у "
                         f"клиента: backend до {bu:.0f} с, dpl до "
                         f"{du:.0f} с)")
            fh.write(f"- {label}{extra}\n")

        fh.write("\n## Вторичный показатель: интервал покрытия проверками\n\n")
        fh.write("См. `aggregate/v2/hc_coverage_when_impacted.png` по тем же "
                 "сценариям и общую картину в `aggregate/hc_resilience_main."
                 "png` / `aggregate/hc_resilience_summary.png`. Эталонный "
                 "интервал зонда 3 с, бюджет обнаружения DOWN 9 с.\n")
    return p


def main():
    os.makedirs(OUT, exist_ok=True)
    if not run_dirs():
        print(f"no run dirs under {RUNS}", file=sys.stderr)
        sys.exit(1)
    sc, clean, n, res = collect()
    steps = [step_of(rd) for rd in run_dirs()]
    step = statistics.median(steps) if steps else 10
    # Aligned-curve grid: bin to the actual step (clamped to >=2s) instead of a
    # fixed 10s, so the appear-then-heal shape is not over-smoothed on 2s runs.
    grid_dx = max(2, round(step))
    print(f"[resilience] N={n} runs, scenarios={len(sc)}, "
          f"impacted={len(shown_impacted(sc, clean))}, clean={len(clean)}, "
          f"hc_only={len(HC_ONLY & set(sc))}, "
          f"step={step:g}s, "
          f"resolution={f'~{step:g}s counter' if res == 'ctr' else 'rate[30s] (no counters yet)'}")
    print("  recovery_when_impacted   ->", fig_recovery(sc, clean, n, res, step))
    print("  recovery_survival        ->", fig_recovery_survival(sc, clean, n, res, step))
    print("  client_errors_aligned    ->", _grid(
        sc, clean, "ecurve", "ошибок/с",
        "Частота ошибок у клиента, выровнено по инъекции отказа",
        [(0.0, "ноль", "#2ca02c")],
        os.path.join(OUT, "client_errors_aligned.png"),
        drop=HC_ONLY, mark_injection=False, grid_dx=grid_dx))
    print("  hc_coverage_when_impacted->", _grid(
        sc, clean, "hcurve", "p90 интервала, с",
        "Интервал покрытия проверками, включая отказ узлов HC",
        [(3.0, "эталон 3 с", "#2ca02c"), (9.0, "бюджет DOWN 9 с", "#d62728")],
        os.path.join(OUT, "hc_coverage_when_impacted.png"), ymax=10,
        extra=HC_INCLUDE, mark_injection=False, grid_dx=grid_dx))
    print("  recovery_summary         ->", write_summary(sc, clean, n, res, step))


if __name__ == "__main__":
    main()
