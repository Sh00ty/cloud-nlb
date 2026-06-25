#!/usr/bin/env python3
"""Строит графики сравнения двухшагового алгоритма размещения с консистентным
хешированием для раздела 7.2 НИР.

Входные данные (генерируются тестами в control-plane/internal/reconciler,
запуск с тегом placementfix, чтобы two-step был исправленным вариантом):
  /tmp/placement_compare.csv  TestPlacementComparison (равномерность, время)
  /tmp/placement_churn.csv    TestPlacementChurn (перемещения при отказе/добавлении)

Выход:
  nir_text/compare_uniformity_time.png
  nir_text/compare_churn.png

Запуск:
  chaos/evidence/.venv/bin/python nir_text/plot_placement_compare.py
"""
import csv
import sys
from collections import defaultdict
from pathlib import Path

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib.ticker import FuncFormatter

HERE = Path(__file__).resolve().parent
COMPARE_CSV = Path("/tmp/placement_compare.csv")
CHURN_CSV = Path("/tmp/placement_churn.csv")

plt.rcParams.update({
    "font.size": 11,
    "axes.titlesize": 12,
    "axes.labelsize": 11,
    "figure.dpi": 150,
    "savefig.dpi": 150,
    "axes.grid": True,
    "grid.alpha": 0.3,
})

ALGOS = {
    "two-step": ("Двухшаговый алгоритм", "#1f77b4", "o"),
    "ring-ch-v1": ("Кольцевое хеширование, 1 вирт. точка", "#ff7f0e", "v"),
    "ring-ch-v200": ("Кольцевое хеширование, 200 вирт. точек", "#2ca02c", "s"),
    "rendezvous": ("Рандеву-хеширование", "#d62728", "D"),
}

intfmt = FuncFormatter(lambda v, _: f"{int(v):,}".replace(",", " "))


def by_algo(rows, key):
    out = defaultdict(lambda: ([], []))
    for r in rows:
        xs, ys = out[r["algo"]]
        xs.append(int(r["nodes"]))
        ys.append(float(r[key]))
    return out


def plot_series(ax, data):
    for algo, (label, color, marker) in ALGOS.items():
        if algo not in data:
            continue
        xs, ys = data[algo]
        ax.plot(xs, ys, marker=marker, color=color, label=label, markersize=5)
    ax.set_xscale("log")
    ax.xaxis.set_major_formatter(intfmt)
    ax.set_xlabel("Число узлов плоскости данных")


def plot_uniformity_time(rows, out: Path):
    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(11, 4.4))

    plot_series(ax1, by_algo(rows, "max_overhead_pct"))
    ax1.set_ylabel("Перегрузка самого нагруженного узла, %")
    ax1.set_title("Отклонение от равномерного размещения")

    plot_series(ax2, by_algo(rows, "time_ms"))
    ax2.set_yscale("log")
    ax2.set_ylabel("Время построения, мс (лог. шкала)")
    ax2.set_title("Время полного построения размещения")

    handles, labels = ax1.get_legend_handles_labels()
    fig.legend(handles, labels, loc="lower center", ncol=2, fontsize=9, frameon=False)
    fig.tight_layout(rect=(0, 0.12, 1, 1))
    fig.savefig(out)
    print(f"wrote {out}")


def plot_churn(rows, out: Path):
    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(11, 4.4))

    death = [r for r in rows if r["event"] == "death"]
    join = [r for r in rows if r["event"] == "join"]

    plot_series(ax1, by_algo(death, "churn_ratio"))
    ax1.set_ylabel("Перемещения, кратно минимуму")
    ax1.set_title("Отказ одного узла")
    ax1.axhline(1.0, color="gray", linestyle="--", linewidth=1)

    plot_series(ax2, by_algo(join, "churn_ratio"))
    ax2.set_ylabel("Перемещения, кратно минимуму")
    ax2.set_title("Добавление одного узла")
    ax2.axhline(1.0, color="gray", linestyle="--", linewidth=1)

    for ax in (ax1, ax2):
        ax.set_ylim(bottom=0)

    handles, labels = ax1.get_legend_handles_labels()
    fig.legend(handles, labels, loc="lower center", ncol=2, fontsize=9, frameon=False)
    fig.tight_layout(rect=(0, 0.12, 1, 1))
    fig.savefig(out)
    print(f"wrote {out}")


def main():
    ok = True
    if COMPARE_CSV.exists():
        rows = list(csv.DictReader(COMPARE_CSV.open()))
        plot_uniformity_time(rows, HERE / "compare_uniformity_time.png")
    else:
        print(f"missing {COMPARE_CSV}", file=sys.stderr)
        ok = False
    if CHURN_CSV.exists():
        rows = list(csv.DictReader(CHURN_CSV.open()))
        plot_churn(rows, HERE / "compare_churn.png")
    else:
        print(f"missing {CHURN_CSV}", file=sys.stderr)
        ok = False
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
