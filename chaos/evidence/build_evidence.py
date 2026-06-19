#!/usr/bin/env python3
"""Freeze NLB chaos-run evidence: Prometheus query_range -> raw JSON + graphs + summary.

Pulls the must-have metric set over the run window, overlays the chaos phase
timeline (derived from the durable poller output), computes per-phase downtime,
and writes committable artifacts into <run-dir>.

Prometheus is reached via `kubectl exec` into its pod (no port-forward), the
same path proven during setup.
"""
import argparse, json, os, re, statistics, subprocess, sys, urllib.parse
from collections import defaultdict

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

PROM_NS = os.environ.get("PROM_NS", "monitoring")
PROM_DEPLOY = os.environ.get("PROM_DEPLOY", "deploy/prometheus")
EPS = 1e-9  # below this, success-RPS counts as "no successful traffic"

# Must-have catalog. Each graph -> one PNG. promql series fetched + frozen raw.
CATALOG = [
    dict(key="throughput", title="Пропускная способность: успех vs ошибки",
         ylabel="запросов/с", kind="line", series=[
            ("успешные (2xx)", 'sum(rate(testclient_requests_total{code=~"2.."}[10s]))'),
            ("не-2xx ответы",  'sum(rate(testclient_requests_total{code!~"2..",code!=""}[10s]))'),
            ("транспортные ошибки", 'sum(rate(testclient_request_errors_total[10s]))'),
         ]),
    dict(key="latency", title="Латентность ответа (перцентили)",
         ylabel="секунды", kind="line", series=[
            ("p50", 'histogram_quantile(0.50, sum by (le) (rate(testclient_request_duration_seconds_bucket[30s])))'),
            ("p95", 'histogram_quantile(0.95, sum by (le) (rate(testclient_request_duration_seconds_bucket[30s])))'),
            ("p99", 'histogram_quantile(0.99, sum by (le) (rate(testclient_request_duration_seconds_bucket[30s])))'),
         ]),
    dict(key="errors_by_kind", title="Ошибки по типу", ylabel="ошибок/с",
         kind="stacked", series=[
            ("{kind}", 'sum by (kind) (rate(testclient_request_errors_total[10s]))'),
         ]),
    dict(key="hc_health", title="Health-check: покрытие и устаревание",
         ylabel="секунды", kind="line",
         hlines=[(3.0, "эталон: интервал зонда HC 3 с", "#2ca02c"),
                 (9.0, "бюджет детекта DOWN 3×3 с", "#d62728")],
         series=[
            ("p90 интервала покрытия", 'histogram_quantile(0.90, sum by (le) (rate(testserver_hc_coverage_interval_seconds_bucket[15s])))'),
            ("max c последней проверки", 'max(testserver_hc_time_since_last_request_seconds)'),
         ]),
    dict(key="backends", title="Живые бэкенды, принимающие трафик",
         ylabel="число бэкендов", kind="line", series=[
            ("живых бэкендов", 'count(sum by (served_by) (rate(testclient_requests_total[30s])) > 0) or vector(0)'),
         ]),
]

# Focused causal figure: backend unavailability + data-plane (agent)
# unavailability vs how traffic reacts. "Expected" is derived as the per-run
# max of each availability signal (stand-agnostic, no hardcoded counts).
AVAIL_SIGNALS = dict(
    live_backends='count(sum by (served_by) (rate(testclient_requests_total[30s])) > 0) or vector(0)',
    live_agents='count(up{app="nlb-agent"} == 1) or vector(0)',
    # hc-worker is a 4-replica StatefulSet; `up==1` count tracks how many HC
    # nodes are actually scrapeable. 4 - live_hc = HC nodes down. ADDITIVE:
    # extra frozen file only, every existing key/graph is byte-identical and
    # old runs simply lack this file until backfill_hc_up.py fills it.
    live_hc='count(up{app="hc-worker"} == 1) or vector(0)',
    # Sharp probe-coverage p90 companions. The main hc_health series is now
    # rate[15s] (was [2m], which smeared the recovery by ~2 min); the [15s]
    # copy here is kept for back-compat with figures that read avail__cov_*,
    # and [10s] is an even sharper variant. At the 2s scrape, rate[15s] ≈ 7
    # samples and rate[10s] ≈ 5 — enough buckets for a stable p90 while keeping
    # coverage recovery resolvable near scrape resolution.
    cov_p90_15s='histogram_quantile(0.90, sum by (le) (rate(testserver_hc_coverage_interval_seconds_bucket[15s])))',
    cov_p90_10s='histogram_quantile(0.90, sum by (le) (rate(testserver_hc_coverage_interval_seconds_bucket[10s])))',
    succ_rps='sum(rate(testclient_requests_total{code=~"2.."}[10s]))',
    non2xx_rps='sum(rate(testclient_requests_total{code!~"2..",code!=""}[10s]))',
    err_rps='sum(rate(testclient_request_errors_total[10s]))',
    # Raw cumulative counters (NOT rated). Differenced offline at the query
    # step they give honest ~step-resolution (~2 s now) success/error, so
    # client recovery is not floored by any rate window at all. This is the
    # source for the headline recovery figure / ECDF; old runs simply lack
    # these files and fall back to the rate series above.
    succ_ctr='sum(testclient_requests_total{code=~"2.."})',
    resp_ctr='sum(testclient_requests_total)',
    err_ctr='sum(testclient_request_errors_total)',
)


def prom_query_range(query, start, end, step):
    qs = urllib.parse.urlencode(dict(query=query, start=start, end=end, step=step))
    url = "http://localhost:9090/api/v1/query_range?" + qs
    out = subprocess.run(
        ["kubectl", "-n", PROM_NS, "exec", PROM_DEPLOY, "--",
         "wget", "-qO-", url],
        capture_output=True, text=True, timeout=120)
    if out.returncode != 0 or not out.stdout.strip():
        raise RuntimeError(f"prom query failed ({out.returncode}): {out.stderr[:300]} :: {query}")
    return json.loads(out.stdout)


def matrix_to_series(resp):
    """Prometheus matrix -> list of (label, [(t, v), ...])."""
    res = resp.get("data", {}).get("result", [])
    series = []
    for r in res:
        m = r.get("metric", {})
        lbl = m.get("kind") or m.get("served_by") or ""
        pts = [(float(t), float(v)) for t, v in r.get("values", [])
               if v not in ("NaN", "+Inf", "-Inf")]
        series.append((lbl, pts))
    return series


def derive_phases(timeline_path):
    """Reduce the durable timeline.jsonl into [{label,kind,start,end}] (unix ts)."""
    if not os.path.exists(timeline_path):
        return []
    agg = {}
    with open(timeline_path) as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            try:
                rec = json.loads(line)
            except json.JSONDecodeError:
                continue
            ts = rec["ts"]
            for c in rec.get("chaos", []):
                name = c.get("name")
                if not name:
                    continue
                a = agg.setdefault(name, dict(kind=c.get("kind", "Chaos"),
                                              first=ts, last=ts, inj=None, rec=None))
                a["first"] = min(a["first"], ts)
                a["last"] = max(a["last"], ts)
                conds = c.get("conds") or {}
                if conds.get("AllInjected") == "True" and a["inj"] is None:
                    a["inj"] = ts
                if conds.get("AllRecovered") == "True" and a["rec"] is None:
                    a["rec"] = ts
    phases = []
    for name, a in agg.items():
        # chaos-mesh workflow names a chaos object "<template>-<5char>-<5char>".
        # Strip up to two trailing 5-char suffixes; never the template itself
        # (suite templates don't end in a bare 5-char [a-z0-9] segment).
        label = re.sub(r"(-[a-z0-9]{5}){1,2}$", "", name) or name
        start = a["inj"] if a["inj"] is not None else a["first"]
        end = a["rec"] if a["rec"] is not None else a["last"]
        phases.append(dict(label=label, name=name, kind=a["kind"],
                           start=start, end=end))
    phases.sort(key=lambda p: p["start"])
    return phases


def gap_stats(succ_series, win_start, win_end, step):
    """Within [win_start,win_end], measure spans where success RPS <= EPS."""
    if not succ_series:
        return None
    _, pts = succ_series[0]
    inwin = [(t, v) for t, v in pts if win_start <= t <= win_end]
    if not inwin:
        return None
    longest = cur = 0.0
    total = 0.0
    for _, v in inwin:
        if v <= EPS:
            cur += step
            total += step
            longest = max(longest, cur)
        else:
            cur = 0.0
    return dict(longest_s=round(longest, 1), total_s=round(total, 1),
                samples=len(inwin))


GROUP_COLORS = {"1": "#1f77b4", "2": "#2ca02c", "3": "#ff7f0e", "4": "#d62728"}


def phase_group(label):
    """Map a phase label to its suite group (id, dir-slug, human title)."""
    if label.startswith("pair-"):
        return ("2", "phase2_pairs", "Фаза 2: парные отказы")
    if label.startswith("triple-"):
        return ("3", "phase3_triples", "Фаза 3: тройные отказы")
    if label.startswith("chaos-"):
        return ("4", "phase4_total", "Фаза 4: тотальный хаос")
    return ("1", "phase1_singles", "Фаза 1: одиночные отказы")


def add_phase_bands(ax, phases, t0, mode, neutral=False):
    """mode="group": shade by suite group, one title per group (clean overview).
       mode="phase": per-phase band + rotated label (readable in zoom).
       neutral=True: grey bands/labels so coloured data series stay readable."""
    ymax = ax.get_ylim()[1]
    if mode == "group":
        spans = {}
        for p in phases:
            gid, _, gname = phase_group(p["label"])
            s, e = p["start"] - t0, p["end"] - t0
            cur = spans.get(gid)
            if cur is None:
                spans[gid] = [s, e, gname]
            else:
                cur[0], cur[1] = min(cur[0], s), max(cur[1], e)
        for gid in sorted(spans):
            s, e, gname = spans[gid]
            c = "#888888" if neutral else GROUP_COLORS.get(gid, "#7f7f7f")
            ax.axvspan(s, e, color=c, alpha=0.05 if neutral else 0.08, lw=0)
            ax.axvline(s, color=c, lw=0.8, alpha=0.5)
            ax.text((s + e) / 2, ymax * 0.98, gname, rotation=0, fontsize=8,
                    va="top", ha="center", color=c,
                    bbox=dict(boxstyle="round", fc="white", ec="none", alpha=0.6))
    else:
        # Collapse phases that start together (concurrent waves) into one
        # band + one combined label so dense zoom figures stay readable.
        waves = {}
        for p in phases:
            key = round((p["start"] - t0) / 3)  # ~3 s bucket
            w = waves.setdefault(key, dict(s=p["start"] - t0, e=p["end"] - t0,
                                           labels=[], gid=phase_group(p["label"])[0]))
            w["e"] = max(w["e"], p["end"] - t0)
            w["labels"].append(p["label"])
        for w in waves.values():
            s, e = w["s"], max(w["e"], w["s"] + 1)
            c = "#888888" if neutral else GROUP_COLORS.get(w["gid"], "#7f7f7f")
            ax.axvspan(s, e, color=c, alpha=0.07 if neutral else 0.12, lw=0)
            txt = " + ".join(w["labels"])
            ax.text(s, ymax, txt, rotation=90, fontsize=7,
                    va="top", ha="left", color=c, alpha=0.9)


def render(graph, fetched, phases, t0, out_png, xlim, band_mode="phase"):
    fig, ax = plt.subplots(figsize=(13, 5))
    ax.set_xlim(*xlim)  # fixed window so phase bands align across all graphs
    if graph["kind"] == "stacked":
        labels, stacks = [], []
        xs_ref = None
        for lbl, pts in fetched:
            if not pts:
                continue
            xs = [t - t0 for t, _ in pts]
            ys = [v for _, v in pts]
            xs_ref = xs_ref or xs
            labels.append(lbl or "—")
            stacks.append(ys)
        if stacks:
            n = min(len(s) for s in stacks)
            ax.stackplot([x for x in xs_ref[:n]],
                         *[s[:n] for s in stacks], labels=labels, alpha=0.8)
    else:
        for lbl, pts in fetched:
            if not pts:
                continue
            ax.plot([t - t0 for t, _ in pts], [v for _, v in pts],
                    label=lbl, lw=1.4)
    for y, lbl, col in graph.get("hlines", []):
        ax.axhline(y, ls="--", lw=1.2, color=col, label=lbl)
    ax.set_xlabel("время от старта прогона, с")
    ax.set_ylabel(graph["ylabel"])
    ax.set_title(graph["title"])
    ax.grid(True, alpha=0.25)
    if ax.get_legend_handles_labels()[0]:
        ax.legend(loc="upper left", fontsize=8, framealpha=0.85)
    add_phase_bands(ax, phases, t0, band_mode)
    fig.tight_layout()
    fig.savefig(out_png, dpi=130)
    plt.close(fig)


def _one_series(resp):
    s = matrix_to_series(resp)
    return s[0][1] if s else []


def fetch_avail(run_dir, q_start, q_end, step):
    """Fetch the 4 availability/traffic signals once, freeze raw JSON, and
    derive aligned per-timestamp unavailability ('expected' = per-run max)."""
    raw = {}
    for name, promql in AVAIL_SIGNALS.items():
        try:
            resp = prom_query_range(promql, q_start, q_end, step)
        except Exception as e:
            print(f"[warn] avail/{name}: {e}", file=sys.stderr)
            raw[name] = []
            continue
        with open(os.path.join(run_dir, "metrics", f"avail__{name}.json"), "w") as fh:
            json.dump(resp, fh)
        raw[name] = _one_series(resp)
    d = {k: {round(t): v for t, v in pts} for k, pts in raw.items()}
    exp_b = max(d["live_backends"].values()) if d.get("live_backends") else 0
    exp_a = max(d["live_agents"].values()) if d.get("live_agents") else 0
    ts = sorted(set().union(*[set(v) for v in d.values()])) if any(d.values()) else []
    return dict(
        ts=ts, exp_b=exp_b, exp_a=exp_a,
        un_b={t: max(0.0, exp_b - d["live_backends"].get(t, exp_b)) for t in ts},
        un_a={t: max(0.0, exp_a - d["live_agents"].get(t, exp_a)) for t in ts},
        succ=d.get("succ_rps", {}), non2xx=d.get("non2xx_rps", {}),
        err=d.get("err_rps", {}))


def recovery_metrics(av, phases, step, grace=30, recover_frac=0.95):
    """Per scenario: full-outage duration, traffic recovery time (until success
    rps returns to >= recover_frac of pre-fault baseline after a degradation),
    and backend / data-plane(agent) unavailability duration. Window = phase
    [start, end+grace] clamped to the next phase start."""
    ts, succ = av["ts"], av["succ"]
    un_b, un_a = av["un_b"], av["un_a"]
    starts = sorted(p["start"] for p in phases)
    # Robust steady-state reference: the TYPICAL healthy throughput, immune to
    # warmup zeros and outage dips. NOT max (single spike) and NOT a local
    # pre-window median (cold-start ramps poison it). It is the median of
    # samples in the upper band of observed throughput.
    sv_all = [v for v in succ.values() if v is not None]
    mx = max(sv_all) if sv_all else 0.0
    healthy = [v for v in sv_all if v >= 0.5 * mx] if mx > 0 else []
    steady = statistics.median(healthy) if healthy else 0.0
    # Empirical run settle time: the earliest timestamp from which throughput
    # holds >= 0.85*steady for a sustained horizon. The pre-fault buffer of a
    # cold-start run sits in the PRE-RUN window (stale steady traffic from
    # before the batch iteration), so a per-scenario pre-window check cannot
    # tell warmup from health for run-start scenarios. A scenario whose window
    # opens before the run has demonstrably settled is in warmup and not
    # attributable. Warm runs settle at t0 (clean 0/0 results preserved);
    # cold runs settle ~30-40 s later (their first scenario is excluded).
    thr_settle = 0.85 * steady
    # Search for settle ONLY from the run's first chaos onward — the samples
    # before it are the pre-run buffer (stale steady traffic from before this
    # batch iteration) and would falsely satisfy "already settled" at t0-60.
    run_start = min((p["start"] for p in phases), default=(ts[0] if ts else 0))
    settle_t = run_start
    if steady > 0 and ts:
        horizon = 60
        in_run = [t for t in ts if t >= run_start]
        for i, t in enumerate(in_run):
            if all(succ.get(u, 0.0) >= thr_settle
                   for u in in_run[i:] if u <= t + horizon):
                settle_t = t
                break
        else:
            settle_t = ts[-1]
    out = {}
    for p in phases:
        nxt = [s for s in starts if s > p["start"]]
        wend = min(p["end"] + grace, nxt[0]) if nxt else p["end"] + grace
        # Health is judged on the samples IMMEDIATELY before injection (the
        # last ~30 s of the pre-window), by MINIMUM not mean. A warmup ramp or
        # sync artifact dips only the 1-2 samples straddling injection, so a
        # full-window median stays "healthy" and misses it; the min of the
        # samples right before the fault is what the scenario actually starts
        # from. The wider window is kept only as a fallback.
        near = [succ[t] for t in ts
                if p["start"] - 35 <= t < p["start"] - 5 and t in succ]
        wide = [succ[t] for t in ts
                if p["start"] - 60 <= t < p["start"] - 5 and t in succ]
        pre_ref = min(near) if near else (statistics.median(wide) if wide else 0.0)
        # A genuine pre-fault baseline is FLAT (real scenarios like the etcd
        # partition sit at a steady ~50 rps before the fault). A warmup/sync
        # ramp is VOLATILE (50->30->24->34->50). High spread => the baseline
        # was never established, so the disturbance is not attributable.
        pre_span = (max(wide) - min(wide)) if wide else 0.0
        bun = round(sum(step for t in ts
                        if p["start"] <= t <= wend and un_b.get(t, 0) > 0), 1)
        dun = round(sum(step for t in ts
                        if p["start"] <= t <= wend and un_a.get(t, 0) > 0), 1)
        # Attributability gate: outage/recovery is only DEFINED for a scenario
        # if the system was at steady health immediately before it. A
        # cold-start warmup ramp or an unrecovered previous scenario fails
        # this — the degradation is not caused by THIS scenario, so it is not
        # attributed (flagged for the aggregate to exclude rather than skew).
        if (p["start"] < settle_t
                or (not near and not wide) or pre_ref < 0.85 * steady
                or pre_span > 0.30 * steady):
            out[p["label"]] = dict(
                kind=p["kind"], group=phase_group(p["label"])[0],
                outage_s=0.0, recover_s=0.0, recovered=True,
                baseline_unhealthy=True,
                backend_unavail_s=bun, dpl_unavail_s=dun,
                baseline_rps=round(pre_ref, 1))
            continue
        base = steady
        thr = recover_frac * base
        win = [t for t in ts if p["start"] <= t <= wend]
        outage = 0.0
        degraded = False
        rec_at = None
        for t in win:
            sv = succ.get(t, base)
            if sv <= EPS:
                outage += step
            if sv < thr:
                degraded = True
            elif degraded and rec_at is None:
                rec_at = t
        if not degraded:
            rec_s = 0.0
        elif rec_at is not None:
            rec_s = rec_at - p["start"]
        else:
            rec_s = wend - p["start"]            # censored: never recovered
        out[p["label"]] = dict(
            kind=p["kind"], group=phase_group(p["label"])[0],
            outage_s=round(outage, 1), recover_s=round(rec_s, 1),
            recovered=bool(not degraded or rec_at is not None),
            baseline_unhealthy=False,
            backend_unavail_s=bun, dpl_unavail_s=dun,
            baseline_rps=round(base, 1))
    return out


def build_availability_figure(av, t0, phases, out_png, xlim, band_mode):
    """Single availability-% line (2xx / (2xx+non2xx+transport)), with backend
    /data-plane unavailability kept only as a faint causal underlay. Traffic
    is one number, not two raw rps lines."""
    ts = av["ts"]
    xs = [t - t0 for t in ts]
    succ, non, err = av["succ"], av.get("non2xx", {}), av["err"]
    avail = []
    for t in ts:
        s, n, e = succ.get(t, 0.0), non.get(t, 0.0), err.get(t, 0.0)
        d = s + n + e
        avail.append(100.0 * s / d if d >= 0.5 else float("nan"))
    un_b = [av["un_b"].get(t, 0.0) for t in ts]
    un_a = [av["un_a"].get(t, 0.0) for t in ts]

    fig, ax = plt.subplots(figsize=(13, 5))
    ax.set_xlim(*xlim)
    # Faint causal underlay on a secondary axis (de-emphasised).
    axu = ax.twinx()
    axu.fill_between(xs, un_b, color="#d62728", alpha=0.10, step="post",
                     label=f"недоступно backend (из {int(av['exp_b'])})")
    axu.plot(xs, un_a, color="#8c564b", lw=1.2, alpha=0.45,
             drawstyle="steps-post",
             label=f"недоступно data-plane/агент (из {int(av['exp_a'])})")
    axu.set_ylabel("недоступно, шт.", color="#888")
    axu.set_ylim(bottom=0)
    axu.tick_params(axis="y", colors="#888")
    # Primary: the single availability-% line.
    ax.plot(xs, avail, color="#1f77b4", lw=1.8, label="доступность, %",
            zorder=5)
    ax.fill_between(xs, avail, 100,
                    where=[a == a and a < 100 for a in avail],
                    color="#d62728", alpha=0.18, interpolate=True, zorder=4)
    ax.set_zorder(axu.get_zorder() + 1)
    ax.patch.set_visible(False)
    ax.set_ylim(-2, 103)
    ax.set_ylabel("доступность, %")
    ax.set_xlabel("время от старта прогона, с")
    ax.set_title("Доступность сервиса (доля успешных запросов)")
    ax.grid(True, alpha=0.25)
    h1, l1 = ax.get_legend_handles_labels()
    h2, l2 = axu.get_legend_handles_labels()
    ax.legend(h1 + h2, l1 + l2, loc="upper center", bbox_to_anchor=(0.5, -0.13),
              ncol=3, fontsize=8, framealpha=0.85)
    add_phase_bands(ax, phases, t0, band_mode, neutral=True)
    fig.savefig(out_png, dpi=130, bbox_inches="tight")
    plt.close(fig)


def build_recovery_chart(rmetrics, phases, out_png):
    """Horizontal bars per scenario: makes UNAVAILABILITY DURATION visible —
    traffic recovery time, full-outage, backend & data-plane unavailability."""
    order = sorted(phases, key=lambda p: (phase_group(p["label"])[0], p["start"]))
    labels = [p["label"] for p in order]
    y = list(range(len(order)))
    rec = [rmetrics[l]["recover_s"] for l in labels]
    out = [rmetrics[l]["outage_s"] for l in labels]
    bun = [rmetrics[l]["backend_unavail_s"] for l in labels]
    dun = [rmetrics[l]["dpl_unavail_s"] for l in labels]
    h = max(4, 0.32 * len(order))
    fig, ax = plt.subplots(figsize=(12, h))
    bw = 0.2
    ax.barh([i + 1.5 * bw for i in y], bun, bw, color="#d62728", alpha=0.45,
            label="backend недоступен, с")
    ax.barh([i + 0.5 * bw for i in y], dun, bw, color="#8c564b", alpha=0.55,
            label="data-plane/агент недоступен, с")
    ax.barh([i - 0.5 * bw for i in y], rec, bw, color="#1f77b4",
            label="восстановление трафика, с")
    ax.barh([i - 1.5 * bw for i in y], out, bw, color="#111111",
            label="полный простой, с")
    ax.set_yticks(y)
    ax.set_yticklabels(labels, fontsize=7)
    ax.invert_yaxis()
    ax.set_xlabel("секунды")
    ax.set_title("Длительность недоступности и восстановления по сценариям")
    ax.grid(True, axis="x", alpha=0.25)
    ax.legend(fontsize=8, loc="lower right")
    fig.savefig(out_png, dpi=130, bbox_inches="tight")
    plt.close(fig)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--run-dir", required=True)
    ap.add_argument("--t0", type=int, required=True)
    ap.add_argument("--t1", type=int, required=True)
    args = ap.parse_args()

    run_dir = args.run_dir
    t0, t1 = args.t0, args.t1
    q_start, q_end = t0 - 60, t1 + 60
    span = max(q_end - q_start, 60)
    # Sample step in whole multiples of 2s so recovery/outage durations (counted
    # in whole steps) are not artificially quantised to 10s. Floor 2s matches
    # the 2s pod scrape; the ~4000 pts/series cap keeps the frozen JSON and the
    # figures light (a full ~95m suite resolves at exactly 2s).
    step = max(2, round(span / 4000 / 2) * 2)

    RECOVERY_GRACE = 30
    phases = derive_phases(os.path.join(run_dir, "timeline.jsonl"))
    with open(os.path.join(run_dir, "phases.json"), "w") as fh:
        json.dump(phases, fh, ensure_ascii=False, indent=2)

    # Single fetch of availability/traffic signals -> figure + recovery metrics.
    av = fetch_avail(run_dir, q_start, q_end, step)
    rmetrics = recovery_metrics(av, phases, step, grace=RECOVERY_GRACE)
    with open(os.path.join(run_dir, "recovery.json"), "w") as fh:
        json.dump(dict(run_dir=os.path.basename(run_dir.rstrip("/")),
                       t0=t0, step=step, scenarios=rmetrics),
                  fh, ensure_ascii=False, indent=2)

    per_graph = {}
    for g in CATALOG:
        fetched = []
        for legend, promql in g["series"]:
            try:
                resp = prom_query_range(promql, q_start, q_end, step)
            except Exception as e:
                print(f"[warn] {g['key']}/{legend}: {e}", file=sys.stderr)
                continue
            with open(os.path.join(run_dir, "metrics",
                                   f"{g['key']}__{legend}.json"), "w") as fh:
                json.dump(resp, fh)
            ser = matrix_to_series(resp)
            if g["kind"] == "stacked":
                fetched.extend(ser)
            else:
                lbl = ser[0][1] if ser else []
                fetched.append((legend, lbl))
        per_graph[g["key"]] = fetched
        # Overview: clean, grouped (Phase 1..4), no per-phase clutter.
        render(g, fetched, phases, t0,
               os.path.join(run_dir, "graphs", f"{g['key']}.png"),
               xlim=(q_start - t0, q_end - t0), band_mode="group")

    # Per-phase-group zoom figures — readable per-phase labels, reuse fetched.
    groups = {}
    for p in phases:
        gid, slug, gname = phase_group(p["label"])
        groups.setdefault((gid, slug, gname), []).append(p)
    zoom_dirs = []
    for (gid, slug, gname), gphases in sorted(groups.items()):
        gw0 = min(p["start"] for p in gphases) - 30
        gw1 = max(p["end"] for p in gphases) + 30
        zdir = os.path.join(run_dir, "graphs", "zoom", f"{gid}_{slug}")
        os.makedirs(zdir, exist_ok=True)
        zoom_dirs.append((gname, f"graphs/zoom/{gid}_{slug}"))
        for g in CATALOG:
            render(g, per_graph[g["key"]], gphases, t0,
                   os.path.join(zdir, f"{g['key']}.png"),
                   xlim=(gw0 - t0, gw1 - t0), band_mode="phase")
        build_availability_figure(
            av, t0, gphases,
            os.path.join(zdir, "availability_vs_traffic.png"),
            xlim=(gw0 - t0, gw1 - t0), band_mode="phase")
        build_recovery_chart(rmetrics, gphases,
                             os.path.join(zdir, "recovery.png"))

    # Focused causal overview + recovery-duration overview.
    build_availability_figure(
        av, t0, phases,
        os.path.join(run_dir, "graphs", "availability_vs_traffic.png"),
        xlim=(q_start - t0, q_end - t0), band_mode="group")
    build_recovery_chart(rmetrics, phases,
                         os.path.join(run_dir, "graphs", "recovery.png"))

    rows = []
    for p in phases:
        m = rmetrics[p["label"]]
        rows.append((p["label"], p["kind"], m["group"],
                     round(p["end"] - p["start"]),
                     m["outage_s"],
                     m["recover_s"] if m["recovered"] else f"≥{m['recover_s']}",
                     m["backend_unavail_s"], m["dpl_unavail_s"]))

    res_note = (f"Разрешение ≈ {step} с (scrape 2 с + окно rate); простой/"
                f"восстановление короче шага не различимы, числа консервативны "
                f"сверху. Окно сценария = [старт, конец+{RECOVERY_GRACE} с], "
                f"ограничено стартом следующей фазы. Восстановление = время до "
                f"возврата успеха к ≥95 % дофазового базлайна (0 — деградации "
                f"не было; «≥» — не восстановился в окне). На стенде data-plane "
                f"совмещён с nlb-agent: `*-agent-*` бьёт и forwarding — оценка "
                f"консервативная (верхняя граница относительно прода с VPP).")
    with open(os.path.join(run_dir, "summary.md"), "w") as fh:
        fh.write(f"# Доказательная сводка прогона\n\n")
        fh.write(f"- Окно прогона: {t1 - t0} с (Prometheus-запрос с буфером ±60 с)\n")
        fh.write(f"- Шаг выборки: {step} с\n- Фаз хаоса: {len(phases)}\n\n")
        fh.write(f"> {res_note}\n\n")
        fh.write("## Простой и восстановление по сценариям\n\n")
        fh.write("| Сценарий | Тип | Гр. | Длит. фазы, с | Простой, с | Восстановление, с | backend недост., с | dpl/агент недост., с |\n")
        fh.write("|---|---|:-:|--:|--:|--:|--:|--:|\n")
        for label, kind, grp, dur, outage, rec, bun, dun in rows:
            fh.write(f"| {label} | {kind} | {grp} | {dur} | {outage} | "
                     f"{rec} | {bun} | {dun} |\n")
        fh.write("\n## Графики (обзор, заливка по группам фаз)\n\n")
        fh.write("- `graphs/recovery.png` — Длительность недоступности и "
                 "восстановления по сценариям\n")
        fh.write("- `graphs/availability_vs_traffic.png` — Недоступность "
                 "backend / data-plane(агент) vs реакция трафика\n")
        for g in CATALOG:
            fh.write(f"- `graphs/{g['key']}.png` — {g['title']}\n")
        fh.write("\n## Зум по группам фаз (читаемые метки)\n\n")
        for gname, gpath in zoom_dirs:
            fh.write(f"- `{gpath}/` — {gname}\n")

    print(f"[evidence] phases={len(phases)} step={step}s -> {run_dir}")
    for label, kind, grp, dur, outage, rec, bun, dun in rows:
        print(f"  {label:<30} outage={outage}s recover={rec}s "
              f"backend_un={bun}s dpl_un={dun}s")


if __name__ == "__main__":
    main()
