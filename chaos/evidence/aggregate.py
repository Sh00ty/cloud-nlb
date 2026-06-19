#!/usr/bin/env python3
"""Cross-run aggregation: turns N frozen runs/run-*/recovery.json into the
thesis centrepiece — per-scenario box-plots of recovery / outage time across
runs + an aggregate table (n, mean±std, median, max) and a per-group rollup.

Usage:  aggregate.py [runs_dir] [out_dir]
Defaults: chaos/evidence/runs , chaos/evidence/aggregate
Works with N>=1 (N=1 = degenerate box; meaningful at N>=3).
"""
import glob, json, os, statistics, sys

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

# Shared withheld-scenario set (single source of truth lives next to the
# headline figures). Keeps this table consistent with the figures: a scenario
# hidden in one artifact must be hidden in all.
from aggregate_timeseries import EXCLUDE_SCENARIOS

HERE = os.path.dirname(os.path.abspath(__file__))
RUNS = sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, "runs")
OUT = sys.argv[2] if len(sys.argv) > 2 else os.path.join(HERE, "aggregate")
GROUP_COLORS = {"1": "#1f77b4", "2": "#2ca02c", "3": "#ff7f0e", "4": "#d62728"}
GROUP_NAME = {"1": "одиночные", "2": "парные", "3": "тройные", "4": "тотальный хаос"}


def load():
    runs = sorted(glob.glob(os.path.join(RUNS, "run-*", "recovery.json")))
    sc = {}
    excluded = 0
    for rj in runs:
        with open(rj) as fh:
            data = json.load(fh)
        for label, m in data.get("scenarios", {}).items():
            if label in EXCLUDE_SCENARIOS:
                continue  # withheld stand artifact, see EXCLUDE_SCENARIOS
            e = sc.setdefault(label, dict(group=m.get("group", "1"),
                                          kind=m.get("kind", ""),
                                          recover=[], outage=[],
                                          bun=[], dun=[], skipped=0))
            # Skip samples whose pre-fault baseline was not steady-healthy:
            # outage/recovery is not attributable to the scenario there
            # (cold-start warmup or unrecovered prior scenario).
            if m.get("baseline_unhealthy"):
                e["skipped"] += 1
                excluded += 1
                continue
            e["recover"].append(m["recover_s"])
            e["outage"].append(m["outage_s"])
            e["bun"].append(m["backend_unavail_s"])
            e["dun"].append(m["dpl_unavail_s"])
    return len(runs), sc, excluded


def box(sc, metric, title, png):
    items = [kv for kv in sorted(sc.items(), key=lambda kv: (kv[1]["group"], kv[0]))
             if kv[1][metric]]  # drop scenarios with no attributable samples
    if not items:
        return
    labels = [k for k, _ in items]
    data = [v[metric] for _, v in items]
    colors = [GROUP_COLORS.get(v["group"], "#777") for _, v in items]
    h = max(4, 0.34 * len(items))
    fig, ax = plt.subplots(figsize=(11, h))
    bp = ax.boxplot(data, vert=False, patch_artist=True, widths=0.6)
    for patch, c in zip(bp["boxes"], colors):
        patch.set_facecolor(c)
        patch.set_alpha(0.55)
    for med in bp["medians"]:
        med.set_color("black")
    ax.set_yticks(range(1, len(labels) + 1))
    ax.set_yticklabels(labels, fontsize=7)
    ax.invert_yaxis()
    ax.set_xlabel("секунды")
    ax.set_title(title)
    ax.grid(True, axis="x", alpha=0.25)
    handles = [plt.Line2D([0], [0], color=GROUP_COLORS[g], lw=6, alpha=0.55,
                          label=f"Фаза {g}: {GROUP_NAME[g]}")
               for g in sorted(GROUP_COLORS)]
    ax.legend(handles=handles, fontsize=8, loc="lower right")
    fig.savefig(png, dpi=130, bbox_inches="tight")
    plt.close(fig)


def stat(xs):
    if not xs:
        return "—"
    m = statistics.mean(xs)
    sd = statistics.pstdev(xs) if len(xs) > 1 else 0.0
    return f"{m:.1f}±{sd:.1f} (med {statistics.median(xs):.1f}, max {max(xs):.1f})"


def main():
    os.makedirs(OUT, exist_ok=True)
    n, sc, excluded = load()
    if not sc:
        print(f"no runs/*/recovery.json under {RUNS}", file=sys.stderr)
        sys.exit(1)
    # No per-CR box-plots here. recover_s is metric-resolution-dominated, and
    # per-CR outage is dishonest for Phase 2-4 (CRs co-fire, Phase 4 fires all
    # at once — attributing outage to a single chaos-CR implies e.g.
    # "chaos-hc-kill loses traffic on the LB", which is false). The honest,
    # slide-readable figures live in aggregate_timeseries.py, which clusters
    # co-firing CRs into one logical scenario (Phase 4 = one box). This module
    # only emits the raw per-CR table below, with the caveat, as an appendix.

    items = sorted(sc.items(), key=lambda kv: (kv[1]["group"], kv[0]))
    with open(os.path.join(OUT, "summary.md"), "w") as fh:
        fh.write(f"# Кросс-прогонная сводка (N={n})\n\n")
        fh.write("Значения: mean±std (med, max) по валидным замерам.\n\n")
        fh.write(f"> Исключено {excluded} замеров сценариев, у которых "
                 "предотказный базлайн не был стабильно здоров (прогрев "
                 "холодного старта или недовосстановление предыдущего "
                 "сценария). Простой/восстановление в таких замерах не "
                 "атрибутируемы данному сценарию и отброшены, а не усреднены. "
                 "Колонка n — число валидных замеров (из %d).\n\n" % n)
        if EXCLUDE_SCENARIOS:
            withheld = ", ".join(sorted(EXCLUDE_SCENARIOS))
            fh.write("> Из показа изъят сценарий %s (партиция сети до "
                     "etcd). Он вскрывает известный устранимый артефакт "
                     "стенда, искажающий атрибуцию длительности клиентских "
                     "ошибок, и до повторного прогона приводится отдельным "
                     "текстом, а не в таблице и не на графиках.\n\n"
                     % withheld)
        fh.write("> Восстановление приведено в таблице, но НЕ в виде графика: "
                 "его величина задаётся разрешением метрики (окно `rate[30s]` "
                 "добавляет ~30 с мнимого восстановления к любому реальному "
                 "провалу), поэтому отдельный boxplot восстановления вводил бы "
                 "в заблуждение. Робастные величины — полный простой (succ≈0) "
                 "и временные ряды: `aggregate/hc_coverage_resilience.png`, "
                 "`aggregate/availability_min_by_scenario.png`, "
                 "`aggregate/errors_by_kind_agg.png` и поран `graphs/"
                 "availability_percent.png`.\n\n")
        # Per-group rollup — the headline numbers.
        fh.write("## По группам фаз\n\n")
        fh.write("| Группа | Сценариев | Восстановление, с | Полный простой, с | Прогонов с простоем |\n")
        fh.write("|---|--:|--|--|--:|\n")
        for g in sorted(GROUP_COLORS):
            gi = [v for _, v in items if v["group"] == g]
            if not gi:
                continue
            rec = [x for v in gi for x in v["recover"]]
            out = [x for v in gi for x in v["outage"]]
            with_out = sum(1 for x in out if x > 0)
            fh.write(f"| Фаза {g}: {GROUP_NAME[g]} | {len(gi)} | "
                     f"{stat(rec)} | {stat(out)} | {with_out}/{len(out)} |\n")
        fh.write("\n## По сценариям\n\n")
        fh.write("| Сценарий | Гр. | n | Восстановление, с | Простой, с | backend недост., с | dpl/агент недост., с |\n")
        fh.write("|---|:-:|--:|--|--|--|--|\n")
        for label, v in items:
            nvalid = len(v["recover"])
            ncol = f"{nvalid}" + (f" (+{v['skipped']}✗)" if v["skipped"] else "")
            fh.write(f"| {label} | {v['group']} | {ncol} | "
                     f"{stat(v['recover'])} | {stat(v['outage'])} | "
                     f"{stat(v['bun'])} | {stat(v['dun'])} |\n")
        fh.write("\n> Стенд: data-plane совмещён с nlb-agent — `*-agent-*` "
                 "сценарии бьют и forwarding; числа консервативны (верхняя "
                 "граница относительно прода с отдельным VPP).\n")

    print(f"[aggregate] N={n} scenarios={len(sc)} excluded_samples={excluded} -> {OUT}")
    print("  outage_boxplot.png  summary.md  (recovery NOT plotted — see note)")
    print("  run aggregate_timeseries.py for the headline time-series figures")


if __name__ == "__main__":
    main()
