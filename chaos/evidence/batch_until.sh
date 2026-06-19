#!/usr/bin/env bash
# Deadline-bounded variant of batch_runs.sh: keep launching chaos runs until a
# wall-clock deadline (default 09:00 local tomorrow), then aggregate.
#
#   batch_until.sh [deadline_epoch]
#
# A new iteration is started only while (deadline - now) >= BUDGET_SEC, so the
# last run is expected to finish before the deadline (no fixed-N overrun/idle).
# Each iteration mirrors batch_runs.sh: re-register test-server (testsrvsyncer,
# tg-port 10000) -> run_experiment.sh (built-in recovery tail; self-quarantines
# bad runs as runs/_invalid-*). After the loop: aggregate.py.
#
# set -e deliberately OFF: one bad iteration must not abort the overnight batch.
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
NS="${CHAOS_NS:-cloud-nlb}"
PY="$HERE/.venv/bin/python"
BUDGET_SEC="${BUDGET_SEC:-6000}"   # don't start a run unless >=100m remain
LOG="$HERE/batch-$(date -u +%Y%m%dT%H%M%SZ).log"
exec > >(tee -a "$LOG") 2>&1

# Deadline: arg1 (epoch) or 09:00 local tomorrow.
if [ "$#" -ge 1 ]; then
  DEADLINE="$1"
else
  DEADLINE="$(date -j -f "%Y-%m-%d %H:%M:%S" "$(date -v+1d +%Y-%m-%d) 09:00:00" +%s)"
fi

echo "[batch] start (until-deadline) ns=$NS budget=${BUDGET_SEC}s log=$LOG ts=$(date -u +%FT%TZ)"
echo "[batch] deadline=$(date -r "$DEADLINE" '+%F %T %Z')  (epoch=$DEADLINE)"
[ -x "$PY" ] || { echo "[batch] FATAL: venv missing ($PY) — run: make -C $HERE setup"; exit 1; }

i=0; ok=0
while :; do
  remain=$(( DEADLINE - $(date +%s) ))
  if [ "$remain" -lt "$BUDGET_SEC" ]; then
    echo "[batch] stop: ${remain}s left < budget ${BUDGET_SEC}s — not starting another run"
    break
  fi
  i=$((i+1))
  echo "[batch] ===== run $i (remain ${remain}s) : testsrvsyncer sync ($(date -u +%FT%TZ)) ====="
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

  echo "[batch] ===== run $i : run_experiment.sh ====="
  "$HERE/run_experiment.sh" || echo "[batch] run $i: run_experiment.sh exit non-zero (continuing)"

  RD="$(ls -1dt "$HERE"/runs/run-* 2>/dev/null | head -1)"
  ok=$((ok+1))
  echo "[batch] ===== run $i COMPLETE -> $RD ====="
done

echo "[batch] runs finished ($ok started, $i attempted); building cross-run aggregate"
"$PY" "$HERE/aggregate.py" "$HERE/runs" "$HERE/aggregate" \
  && echo "[batch] aggregate OK -> $HERE/aggregate" \
  || echo "[batch] aggregate FAILED"
echo "[batch] BATCH COMPLETE ts=$(date -u +%FT%TZ) log=$LOG"
