#!/usr/bin/env bash
# Run the chaos suite N times for cross-run aggregation (box-plots, N>=5).
#
#   batch_runs.sh [N]      (default N=5)
#
# Each iteration: re-register test-server (testsrvsyncer, tg-port 10000) ->
# full run_experiment.sh (now with built-in recovery tail) -> tail self-check.
# After all runs: aggregate.py -> chaos/evidence/aggregate/.
#
# set -e is deliberately OFF: one bad iteration must not abort a ~6h batch.
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
N="${1:-5}"
NS="${CHAOS_NS:-cloud-nlb}"
PY="$HERE/.venv/bin/python"
LOG="$HERE/batch-$(date -u +%Y%m%dT%H%M%SZ).log"
exec > >(tee -a "$LOG") 2>&1

echo "[batch] start N=$N ns=$NS log=$LOG ts=$(date -u +%FT%TZ)"
[ -x "$PY" ] || { echo "[batch] FATAL: venv missing ($PY) — run: make -C $HERE setup"; exit 1; }

ok=0
for i in $(seq 1 "$N"); do
  echo "[batch] ===== run $i/$N : testsrvsyncer sync ($(date -u +%FT%TZ)) ====="
  if ! ( cd "$REPO" && go run ./tools/cmd/testsrvsyncer \
        --kubeconfig "$HOME/.kube/config" \
        --namespace "$NS" \
        --selector "app=test-server" \
        --target-group test-server \
        --tg-port 10000 \
        --tg-vip 10.96.0.100 \
        --hc-port 8090 ); then
    echo "[batch] run $i: SYNC FAILED — skipping this iteration"
    continue
  fi

  echo "[batch] ===== run $i/$N : run_experiment.sh ====="
  "$HERE/run_experiment.sh" || echo "[batch] run $i: run_experiment.sh exit non-zero (continuing)"

  # Newest VALID run dir (run_experiment.sh self-quarantines bad ones as
  # _invalid-*, so a plain run-* glob already excludes them).
  RD="$(ls -1dt "$HERE"/runs/run-* 2>/dev/null | head -1)"
  python3 - "$RD" <<'PY' || echo "[batch] run $i: tail self-check could not run"
import json,sys,os,re,datetime
rd=sys.argv[1]
try:
    wf=open(os.path.join(rd,"workflow-status.yaml")).read()
    m=re.search(r'endTime: "([^"]+)"',wf)
    if not m:
        print(f"[batch] tail-check {os.path.basename(rd)}: NO endTime (corrupt run?)"); raise SystemExit
    end=datetime.datetime.strptime(m.group(1),"%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=datetime.timezone.utc).timestamp()
    # exact filename — NOT a *2xx* glob (that also matches "не-2xx", whose
    # result is legitimately empty -> false IndexError).
    f=os.path.join(rd,"metrics","throughput__успешные (2xx).json")
    res=json.load(open(f))["data"]["result"]
    if not res or not res[0].get("values"):
        print(f"[batch] tail-check {os.path.basename(rd)}: 2xx series EMPTY (bad run)"); raise SystemExit
    v=res[0]["values"]; tail=int(int(v[-1][0])-end)
    print(f"[batch] tail-check {os.path.basename(rd)}: +{tail}s past endTime "
          + ("OK" if tail>=120 else "TRUNCATED — re-freeze needed"))
except SystemExit:
    pass
except Exception as e:
    print("[batch] tail-check error:",repr(e))
PY
  ok=$((ok+1))
  echo "[batch] ===== run $i/$N COMPLETE -> $RD ====="
done

echo "[batch] runs finished ($ok/$N ok); building cross-run aggregate"
"$PY" "$HERE/aggregate.py" "$HERE/runs" "$HERE/aggregate" \
  && echo "[batch] aggregate OK -> $HERE/aggregate" \
  || echo "[batch] aggregate FAILED"
echo "[batch] BATCH COMPLETE ts=$(date -u +%FT%TZ) log=$LOG"
