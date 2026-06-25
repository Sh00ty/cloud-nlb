#!/usr/bin/env python3
"""Строит графики микробенчмарков для раздела 8 НИР.

Входные данные:
  /tmp/reconciler_bench.txt     вывод go test -bench по реконсилятору (count>=1)
  /tmp/scheduler_throughput.csv CSV из TestSchedulerThroughputCSV

Выход:
  nir_text/bench_placement.png  время генерации размещения по масштабам
  nir_text/bench_scheduler.png  пропускная способность и латентность планировщика

Запуск:
  chaos/evidence/.venv/bin/python nir_text/plot_benchmarks.py
"""
import csv
import re
import sys
from collections import defaultdict
from pathlib import Path
from statistics import mean

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib.ticker import FuncFormatter

HERE = Path(__file__).resolve().parent
BENCH_TXT = Path("/tmp/reconciler_bench.txt")
SCHED_CSV = Path("/tmp/scheduler_throughput.csv")

plt.rcParams.update({
    "font.size": 11,
    "axes.titlesize": 12,
    "axes.labelsize": 11,
    "figure.dpi": 150,
    "savefig.dpi": 150,
    "axes.grid": True,
    "grid.alpha": 0.3,
})

BENCH_LINE = re.compile(
    r"^Benchmark(\w+)/tg=(\d+)/nodes=(\d+)/rf=(\d+)-\d+\s+\d+\s+([\d.]+) ns/op"
)


def load_reconciler(path: Path):
    """name -> {(tg, nodes): среднее время операции в мс по всем count}."""
    acc = defaultdict(lambda: defaultdict(list))
    for line in path.read_text().splitlines():
        m = BENCH_LINE.match(line)
        if not m:
            continue
        name, tg, nodes, _rf, ns = m.groups()
        acc[name][(int(tg), int(nodes))].append(float(ns) / 1e6)
    return {n: {k: mean(v) for k, v in d.items()} for n, d in acc.items()}


def plot_placement(data, out: Path):
    cold = data.get("PlacementCold", {})
    lazy = data.get("CalculateDesiredLazy", {})
    death = data.get("PlacementNodeDeath", {})
    cases = sorted(cold)
    labels = [f"{tg//1000} тыс. групп\n{nodes} узлов" for tg, nodes in cases]
    x = range(len(cases))
    w = 0.38

    fig, ax = plt.subplots(figsize=(8, 4.5))
    bars1 = ax.bar([i - w / 2 for i in x], [cold[c] for c in cases], w,
                   label="Полная генерация размещения с нуля", color="#1f77b4")
    bars2 = ax.bar([i + w / 2 for i in x], [lazy.get(c, 0) for c in cases], w,
                   label="Шаг 1: расчёт недореплицированных групп", color="#9ecae1")

    # точка повторной реконсиляции после отказа узла на соответствующем масштабе
    for (tg, nodes), v in death.items():
        try:
            i = cases.index((tg, nodes))
        except ValueError:
            continue
        ax.plot([i], [v], marker="D", color="#d62728", markersize=8, zorder=5,
                linestyle="none",
                label="Повторная реконсиляция после отказа одного узла")

    ax.set_yscale("log")
    ax.set_ylim(top=max(cold.values()) * 12)  # запас сверху под легенду
    ax.set_ylabel("Время, мс (логарифмическая шкала)")
    ax.set_xticks(list(x))
    ax.set_xticklabels(labels)
    ax.set_title("Время генерации размещения целевых групп (фактор репликации 3)")
    for bars in (bars1, bars2):
        for b in bars:
            h = b.get_height()
            if h <= 0:
                continue
            ax.annotate(f"{h:.2g}" if h < 10 else f"{h:.0f}",
                        (b.get_x() + b.get_width() / 2, h),
                        textcoords="offset points", xytext=(0, 3),
                        ha="center", fontsize=9)
    ax.legend(loc="upper left", fontsize=9)
    fig.tight_layout()
    fig.savefig(out)
    print(f"wrote {out}")


def plot_scheduler(path: Path, out: Path):
    rows = list(csv.DictReader(path.open()))
    targets = [int(r["targets"]) for r in rows]
    tps = [float(r["tasks_per_s"]) / 1e3 for r in rows]
    p50 = [float(r["p50_us"]) for r in rows]
    p99 = [float(r["p99_us"]) for r in rows]

    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(10, 4.2))

    ax1.plot(targets, tps, marker="o", color="#1f77b4")
    ax1.set_xscale("log")
    ax1.set_xlabel("Число целей проверки")
    ax1.set_ylabel("Тысяч циклов планирования в секунду")
    ax1.set_title("Пропускная способность планировщика")
    ax1.set_ylim(bottom=0)

    ax2.plot(targets, p50, marker="o", label="p50", color="#2ca02c")
    ax2.plot(targets, p99, marker="s", label="p99", color="#d62728")
    ax2.set_xscale("log")
    ax2.set_xlabel("Число целей проверки")
    ax2.set_ylabel("Время одного цикла, мкс")
    ax2.set_title("Латентность одного цикла планирования")
    ax2.set_ylim(bottom=0)
    ax2.legend()

    fmt = FuncFormatter(lambda v, _: f"{int(v):,}".replace(",", " "))
    ax1.xaxis.set_major_formatter(fmt)
    ax2.xaxis.set_major_formatter(fmt)
    for ax in (ax1, ax2):
        ax.tick_params(axis="x", labelrotation=30)

    fig.tight_layout()
    fig.savefig(out)
    print(f"wrote {out}")


def main():
    ok = True
    if BENCH_TXT.exists():
        plot_placement(load_reconciler(BENCH_TXT), HERE / "bench_placement.png")
    else:
        print(f"missing {BENCH_TXT}", file=sys.stderr)
        ok = False
    if SCHED_CSV.exists():
        plot_scheduler(SCHED_CSV, HERE / "bench_scheduler.png")
    else:
        print(f"missing {SCHED_CSV}", file=sys.stderr)
        ok = False
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
