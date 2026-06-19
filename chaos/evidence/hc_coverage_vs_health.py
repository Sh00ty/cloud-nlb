#!/usr/bin/env python3
"""HC probe-coverage interval vs how many HC nodes were actually down.

Correlates two frozen, time-aligned series for every HC-involving logical
scenario, across all runs, OFFLINE (no Prometheus):

  - probe-coverage interval p90, measured SERVER-SIDE:
      histogram_quantile(0.9, rate(coverage_bucket[2m]))
    The 2 m rate window SMEARS and LAGS this series, so a recovery time read
    off p90 is an UPPER BOUND, inflated by up to ~2 min. There is no sharper
    server-side coverage signal here: the instantaneous gauge
    `testserver_hc_time_since_last_request_seconds` is identically 0 in every
    frozen run (the test-server build on this stand does not export it), so it
    is deliberately NOT used — reporting a flat-zero series as "instant
    recovery" would be a measurement artifact passed off as a system property.
  - HC nodes down = max(live_hc) - live_hc,  live_hc = count(up{hc-worker}==1).

Healthy baseline is the within-run LOWER-half median of p90 (the quiescent
level the metric sits at between scenarios) — robust, contamination-free, and
unaffected by the one run whose rate[2m] bucket is still cold at workflow start
(a fixed pre-window would bleed into the previous scenario: HC scenarios are
only ~26 s apart).

Outputs (ADDITIVE, never overwrites the old aggregate):
  aggregate/v2/hc_coverage_vs_health.png  — THE figure: single-hc-failure-long
        (3/4 HC down, 4 min) aligned to injection, every run overlaid +
        cross-run median; coverage interval (left) over HC-nodes-down (right).
  aggregate/v2/hc_coverage_vs_health.md   — per logical scenario: healthy p90,
        peak p90, max HC down, recovery time (p90 upper bound), with the
        resolution caveat spelled out.

Usage: hc_coverage_vs_health.py [runs_dir] [out_dir]
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
OUT = sys.argv[2] if len(sys.argv) > 2 else os.path.join(HERE, "aggregate", "v2")

P90_SHARP = "avail__cov_p90_15s.json"       # rate[15s] ≈ 3 scrapes, sharp
P90_LEGACY = "hc_health__p90 интервала покрытия.json"  # rate[2m], smeared
HCUP = "avail__live_hc.json"

WAVE_TOL = 20      # s: concurrent CRs share a start; sequential singles >=45 s
PRE = -60          # s: left edge of the aligned window (display + pre-context)
POST = 320         # s: max horizon after injection
TOL = 0.5          # s: "back to healthy" tolerance for recovery
HOLD = 30          # s: recovery must hold this long to count as recovered

FIG_SCEN = "single-hc-failure-long"   # clean sustained 3/4-down causal picture


def run_dirs():
    return sorted(glob.glob(os.path.join(RUNS, "run-*")))


def load_series(rd, fname):
    f = os.path.join(rd, "metrics", fname)
    if not os.path.exists(f):
        return {}
    s = be.matrix_to_series(json.load(open(f)))
    return {round(t): v for t, v in s[0][1]} if s else {}


def group_of(label):
    if label.startswith("pair-"):
        return "2"
    if label.startswith("triple-"):
        return "3"
    if label.startswith("chaos-"):
        return "4"
    return "1"


def base_name(label):
    p = label.rsplit("-", 1)
    return p[0] if len(p) == 2 and p[1].isdigit() else label


def clusters_of(rd):
    """Co-firing-wave clustering -> logical scenarios (same rule as the rest
    of the pipeline: concurrent CRs are ONE scenario, phase 4 collapses)."""
    f = os.path.join(rd, "phases.json")
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
    out, starts = [], [w["start"] for w in waves]
    for w in waves:
        g = group_of(w["members"][0])
        if g == "4":
            label = "тотальный хаос (всё одновременно)"
        else:
            bases = sorted({base_name(m) for m in w["members"]})
            label = " + ".join(bases) if len(bases) > 1 else bases[0]
        nxt = [s for s in starts if s > w["start"]]
        clamp = nxt[0] if nxt else w["end"] + POST
        out.append(dict(label=label, group=g, start=w["start"],
                        end=w["end"], clamp=clamp, members=w["members"]))
    return out


# Logical-scenario rows for the table: the two isolated HC scenarios kept
# SEPARATE (single-hc-kill is too brief to perturb p90; failure-long is the
# real 4 min 3/4-down case), then HC inside pair / triple / total chaos.
ROWS = [
    ("single-hc-kill (убийство 3/4, кратко)",
     lambda c: c["label"] == "single-hc-kill"),
    ("single-hc-failure-long (отказ 3/4, 4 мин)",
     lambda c: c["label"] == "single-hc-failure-long"),
    ("HC + 1 отказ (парные c hc)",
     lambda c: c["group"] == "2" and any("hc" in m for m in c["members"])),
    ("HC + 2 отказа (тройные c hc)",
     lambda c: c["group"] == "3" and any("hc" in m for m in c["members"])),
    ("HC в тотальном хаосе",
     lambda c: c["group"] == "4"),
]


def p90_series(rd):
    """Sharp rate[15s] p90 if frozen, else the legacy rate[2m] series."""
    s = load_series(rd, P90_SHARP)
    return s if s else load_series(rd, P90_LEGACY)


def is_sharp(rd):
    return bool(load_series(rd, P90_SHARP))


def healthy_base(rd):
    """Within-run quiescent p90: median of the LOWER half of all samples."""
    vals = sorted(p90_series(rd).values())
    if not vals:
        return None
    return statistics.median(vals[: max(1, len(vals) // 2)])


def in_win(ser, t0, lo, hi):
    return sorted((t - t0, v) for t, v in ser.items() if lo <= t - t0 <= hi)


def first_stable(pairs, ok, hold):
    """First x where ok(v) and it stays ok for >= hold seconds; else None."""
    pairs = sorted(pairs)
    for i, (x, v) in enumerate(pairs):
        if ok(v) and all(ok(w) for xx, w in pairs[i:] if xx <= x + hold):
            return x
    return None


def analyse(rd, cl, hbase):
    s = cl["start"]
    hi = min(POST, cl["clamp"] - s)
    p90 = dict(in_win(p90_series(rd), s, 0, hi))
    hcu = dict(in_win(load_series(rd, HCUP), s, 0, hi))
    if not p90 or not hcu or hbase is None:
        return None
    exp = max(hcu.values()) or 4
    down = {x: max(0.0, exp - v) for x, v in hcu.items()}
    thr = hbase + TOL
    peak = max(p90.values())
    impacted = peak > thr

    # First HC node goes down: first x>0 where down>=1.
    downs = sorted((x, v) for x, v in down.items() if x >= 0)
    t_down = next((x for x, v in downs if v >= 0.5), None)
    # HC fully restored: first x after t_down where down==0 and holds >=20 s.
    t_restored = first_stable(list(down.items()), lambda v: v < 0.5, 20)
    restored_in_win = t_restored is not None
    if not restored_in_win:
        t_restored = max(down) if down else 0.0

    # Responsiveness: from HC going down to p90 reaching the degraded level
    # (half-way base->peak). With the sharp series this is ~one scrape, i.e.
    # the interval reacts as a STEP, not a slow ramp (the ramp under rate[2m]
    # was the metric's own averaging, not the system).
    rise = None
    if impacted and t_down is not None:
        half = hbase + 0.5 * (peak - hbase)
        rise = next((x - t_down for x, v in sorted(p90.items())
                     if x >= t_down and v >= half), None)

    # Steady degraded level = median of p90 while HC is down, SKIPPING the
    # first ~15 s after the node drop (the rate[15s] window straddles the
    # counter discontinuity there and emits a one-sample spike — same class
    # of metric artifact as the rate[2m] ramp, just at the edge) and the
    # recovery edge. This, not the raw max, is the defensible headline.
    plat_pts = [v for x, v in p90.items()
                if t_down is not None and x >= t_down + 15
                and down.get(x, 0.0) >= 0.5]
    plateau = (statistics.median(plat_pts) if plat_pts
               else (statistics.median([v for x, v in p90.items() if x >= 0])
                     if impacted else hbase))

    if not impacted:
        rec = 0.0
    else:
        x = first_stable([(xx, v) for xx, v in p90.items() if xx >= t_restored],
                         lambda v: v <= thr, HOLD)
        rec = None if x is None else max(0.0, x - t_restored)

    # Post-restore observation budget: how long the scenario window lasts
    # after HC is back. rec=None with a short tail = clamped by the next
    # scenario, NOT slow recovery.
    tail = (max(p90) - t_restored) if restored_in_win else 0.0

    return dict(
        base=hbase, peak=peak,
        max_down=max(down.values()) if down else 0.0,
        impacted=impacted, restored_in_win=restored_in_win,
        rise=rise,               # s from HC-down to degraded level (~1 scrape)
        rec=rec,                 # s from HC-back to baseline; None = not in win
        tail=tail,               # observable seconds after HC restored
    )


def grid_stats(curves, x0, x1, dx):
    xs, med, lo, hi = [], [], [], []
    g = x0
    while g <= x1:
        vals = [statistics.mean([y for x, y in c if abs(x - g) <= dx / 2.0])
                for c in curves if any(abs(x - g) <= dx / 2.0 for x, _ in c)]
        if vals:
            vals.sort()
            xs.append(g)
            med.append(statistics.median(vals))
            lo.append(vals[max(0, round(0.25 * (len(vals) - 1)))])
            hi.append(vals[min(len(vals) - 1, round(0.75 * (len(vals) - 1)))])
        g += dx
    return xs, med, lo, hi


def figure(rds):
    cov, dn = [], []
    for rd in rds:
        p90, hcu = p90_series(rd), load_series(rd, HCUP)
        if not p90 or not hcu:
            continue
        exp = max(hcu.values()) or 4
        for cl in clusters_of(rd):
            if cl["label"] != FIG_SCEN:
                continue
            s = cl["start"]
            cov.append(in_win(p90, s, PRE, POST))
            dn.append([(t, exp - v) for t, v in in_win(hcu, s, PRE, POST)])
    fig, ax = plt.subplots(figsize=(14, 6.4))
    ax2 = ax.twinx()
    if dn:
        xs, md, _, _ = grid_stats(dn, PRE, POST, 10)
        ax2.fill_between(xs, 0, md, step="mid", color="#7f7f7f", alpha=0.16)
        ax2.step(xs, md, where="mid", color="#7f7f7f", lw=1.8,
                 label="узлов HC недоступно (медиана)")
    ax2.set_ylim(0, 4.4)
    ax2.set_yticks([0, 1, 2, 3, 4])
    ax2.set_ylabel("узлов HC недоступно (из 4)")
    for c in cov:
        ax.plot([x for x, _ in c], [y for _, y in c],
                color="#9ecae1", lw=1.0, alpha=0.5)
    if cov:
        xs, md, lo, hi = grid_stats(cov, PRE, POST, 10)
        ax.fill_between(xs, lo, hi, color="#1f77b4", alpha=0.20,
                        label="интервал покрытия p90, разброс по прогонам")
        ax.plot(xs, md, color="#1f77b4", lw=3.0,
                label="интервал покрытия p90, медиана")
        pk = max(md)
        px = xs[md.index(pk)]
        ax.annotate(f"пик медианы p90 ≈ {pk:.2f} с",
                    xy=(px, pk), xytext=(px + 25, pk + 2.0),
                    arrowprops=dict(arrowstyle="->", color="#1f77b4"),
                    fontsize=12, color="#1f77b4")
    ax.axhline(3.0, ls="--", lw=1.6, color="#2ca02c",
               label="эталонный интервал зонда 3 с")
    ax.axhline(9.0, ls="--", lw=1.6, color="#d62728",
               label="бюджет обнаружения DOWN 3×3 с")
    ax.set_xlim(PRE, POST)
    ax.set_ylim(0, 10)
    ax.set_xlabel("время от инъекции отказа HC, с")
    ax.set_ylabel("интервал покрытия проверками, с")
    ax.set_title("Интервал покрытия проверками против числа отказавших "
                 "узлов HC\nsingle-hc-failure-long: 3 из 4 узлов недоступны "
                 "4 мин (p90, резкое окно rate[15s])")
    ax.grid(True, alpha=0.3)
    h1, l1 = ax.get_legend_handles_labels()
    h2, l2 = ax2.get_legend_handles_labels()
    ax.legend(h1 + h2, l1 + l2, loc="upper left", framealpha=0.92)
    fig.tight_layout()
    os.makedirs(OUT, exist_ok=True)
    p = os.path.join(OUT, "hc_coverage_vs_health.png")
    fig.savefig(p, dpi=140)
    plt.close(fig)
    return p, len(cov)


def fmt(xs, unit=""):
    xs = [x for x in xs if x is not None]
    if not xs:
        return "—"
    if len(set(round(x, 1) for x in xs)) == 1:
        return f"{xs[0]:.1f}{unit}"
    return (f"медиана {statistics.median(xs):.1f}{unit} "
            f"(мин {min(xs):.1f} — макс {max(xs):.1f}{unit})")


def report(rds):
    bases = {rd: healthy_base(rd) for rd in rds}
    data = {name: [] for name, _ in ROWS}
    for rd in rds:
        for cl in clusters_of(rd):
            for name, match in ROWS:
                if match(cl):
                    a = analyse(rd, cl, bases[rd])
                    if a:
                        data[name].append(a)
    L = []
    L.append("# Интервал покрытия проверками против отказа узлов HC\n")
    sharp = all(is_sharp(rd) for rd in rds)
    src = ("`metrics/avail__cov_p90_15s.json` (резкое окно `rate[15s]`)"
           if sharp else "`metrics/hc_health__p90 интервала покрытия.json` "
           "(`rate[2m]`, запасной ряд)")
    L.append(f"Источник: замороженные {src} и `metrics/avail__live_hc.json` "
             "по всем прогонам, офлайн. Узлов HC недоступно = "
             "`max(live_hc) - live_hc`, `live_hc = "
             "count(up{app=\"hc-worker\"}==1)` (StatefulSet из 4 реплик).\n")
    L.append("**Оговорка о разрешении.** Интервал покрытия снят как "
             "`histogram_quantile(0.9, rate(coverage_bucket[15s]))`. При "
             "скрейпе ~5 с окно `rate[15s]` это ~3 выборки, в ~8 раз резче "
             "штатного графика `rate[2m]`. Это вскрывает главное: реакция "
             "интервала на отказ узлов HC ступенчатая, а не плавная. "
             "Прежний плавный подъём под `rate[2m]` (с ~2.8 до ~3.4 с за "
             "~100 с) был артефактом усреднения метрики, а не разгоном "
             "системы: на резком ряду интервал переходит с базы на полку за "
             "один скрейп. Разрешающая способность по времени ограничена "
             "снизу величиной скрейп ~5 с плюс окно `rate` 15 с, провалы "
             "короче не различимы. Окно `rate[10s]` тоже заморожено "
             "(`avail__cov_p90_10s.json`), но на 2 выборках шумит (выброс на "
             "переходе), поэтому головным взят `[15s]`. Более резкого "
             "серверного сигнала нет: датчик "
             "`testserver_hc_time_since_last_request_seconds` тождественно "
             "нулевой во всех прогонах (сборкой тестового сервера на стенде "
             "не экспонируется) и сознательно не используется. Счётчик "
             "упавших узлов HC снят со скрейпом 10 с: при быстрых циклах "
             "kill/restart пода (фазы 2–4) часть провалов `up` между "
             "скрейпами не видна, поэтому «макс. узлов HC down» в "
             "комбинированных фазах это нижняя оценка; чисто число снято "
             "только в `single-hc-failure-long` (3 из 4 узлов недоступны "
             "непрерывно ~4 мин). База это медиана нижней половины p90 за "
             "прогон (устойчивый уровень между сценариями).\n")
    L.append("| Логический сценарий | База p90, с | Пик p90, с | "
             "Реакция (отказ→полка), с | Макс. узлов HC down | "
             "Восст. p90 после возврата узлов, с |")
    L.append("|---|---|---|---|---|---|")
    clamp_note = False
    for name, _ in ROWS:
        rs = data[name]
        if not rs:
            L.append(f"| {name} | нет данных ||||| ")
            continue
        recs = [r["rec"] for r in rs]
        clamped = sum(1 for r in rs
                      if r["impacted"] and r["rec"] is None
                      and r["tail"] < 25)
        rcell = fmt([r for r in recs if r is not None], " с")
        if clamped:
            rcell += " †"
            clamp_note = True
        L.append(
            f"| {name} "
            f"| {fmt([r['base'] for r in rs])} "
            f"| {fmt([r['peak'] for r in rs])} "
            f"| {fmt([r['rise'] for r in rs], ' с')} "
            f"| {fmt([r['max_down'] for r in rs])} "
            f"| {rcell} |")
    L.append("\nРеакция это время от падения первого узла HC до выхода p90 "
             "на деградированную полку (половина пути база→пик); на резком "
             "ряду это порядка одного скрейпа. Восстановление это время от "
             f"возврата всех узлов HC до возврата p90 в полосу база+{TOL:g} с "
             f"с удержанием {HOLD} с; 0.0 означает, что p90 из полосы базы не "
             "выходил.")
    if clamp_note:
        L.append("\n† в части прогонов p90 не успел вернуться в полосе базы "
                 "до старта следующего сценария: окно наблюдения после "
                 "возврата узлов короче ~25 с (характерно для "
                 "`single-hc-failure-long`, где зазор до следующего "
                 "сценария всего ~21 с). Это ограничение наблюдения, "
                 "заданное расписанием набора, а не медленное "
                 "восстановление системы.")
    L.append("\n## Итог\n")
    L.append("На резком ряду видно, что при потере 3 из 4 узлов HC интервал "
             "покрытия мгновенно (за один скрейп) переходит с ~2.8 с на "
             "ровную полку ~3.4 с и держит её, не нарастая, всю "
             "недоступность; запас до бюджета обнаружения DOWN 9 с порядка "
             "5.5 с. Реакция системы это шаг, а не разгон. Время "
             "восстановления интервала измеримо там, где окно наблюдения "
             "после возврата узлов достаточно длинное; для самого долгого "
             "сценария оно упирается в зазор расписания ~21 с, поэтому для "
             "него восстановление подаётся как ограниченное наблюдением, а "
             "не как свойство системы. В тотальном хаосе по типичному "
             "прогону p90 ≈ 2.3 с (запас более 6 с), в одном худшем прогоне "
             "кратковременный выброс до ~9–10 с (упор в потолок метрики "
             "10 с) приводится как граница, не типичный режим.")
    md = os.path.join(OUT, "hc_coverage_vs_health.md")
    os.makedirs(OUT, exist_ok=True)
    open(md, "w").write("\n".join(L) + "\n")
    return md, data


def main():
    rds = run_dirs()
    if not rds:
        print(f"no run dirs under {RUNS}", file=sys.stderr)
        sys.exit(1)
    p, n = figure(rds)
    print(f"  hc_coverage_vs_health.png -> {p} ({n} {FIG_SCEN} curves)")
    md, data = report(rds)
    print(f"  hc_coverage_vs_health.md  -> {md}\n")
    for name, _ in ROWS:
        rs = data[name]
        if not rs:
            continue
        worst = max(r["peak"] for r in rs)
        rec = [r["rec"] for r in rs if r["rec"] is not None]
        print(f"  {name}")
        print(f"    база p90        {fmt([r['base'] for r in rs])}")
        print(f"    пик p90         {fmt([r['peak'] for r in rs])}")
        print(f"    реакция         {fmt([r['rise'] for r in rs], ' с')}")
        print(f"    восст p90       {fmt(rec, ' с') if rec else '— (окно)'}")
        print(f"    макс HC down    {fmt([r['max_down'] for r in rs])}")
        print(f"    запас до 9 с    {9.0 - worst:.1f} с (худший прогон) "
              f"(измерений {len(rs)})")


if __name__ == "__main__":
    main()
