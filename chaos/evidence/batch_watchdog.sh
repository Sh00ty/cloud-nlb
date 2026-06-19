#!/usr/bin/env bash
# Batch watchdog. Blocks until ONE reportable event, then exits with a JSON
# line "BATCH_RESULT {...}" so the orchestrator reports per run and relaunches.
#
#   batch_watchdog.sh <runs-dir> <baseline-run-count> <batch-pid>
#
# Events:
#   RUN_DONE   - a new run dir gained summary.md (one run finished)
#   BATCH_END  - batch_runs.sh process exited (all runs + aggregate done)
#   STALL      - no new completed run for >90m while batch alive (a run is ~75m)
set -u

RUNS="${1:?usage: batch_watchdog.sh <runs-dir> <baseline-count> <batch-pid>}"
BASE="${2:?baseline run count required}"
BPID="${3:?batch pid required}"
INTERVAL="${BW_INTERVAL:-60}"
STALL="${BW_STALL:-5400}"   # 90m
START=$(date +%s)

count_done() { ls -1d "$RUNS"/run-*/ 2>/dev/null | while read -r d; do [ -f "$d/summary.md" ] && echo x; done | grep -c x || echo 0; }

while true; do
  NOW=$(date +%s)
  DONE=$(count_done)
  NEWEST="$(ls -1dt "$RUNS"/run-* 2>/dev/null | head -1)"

  if [ "$DONE" -gt "$BASE" ]; then
    echo "BATCH_RESULT {\"reason\":\"RUN_DONE\",\"completed_runs\":$DONE,\"baseline\":$BASE,\"newest\":\"$(basename "${NEWEST:-?}")\",\"iso\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}"
    exit 0
  fi

  if ! kill -0 "$BPID" 2>/dev/null; then
    echo "BATCH_RESULT {\"reason\":\"BATCH_END\",\"completed_runs\":$DONE,\"iso\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}"
    exit 0
  fi

  if [ $((NOW - START)) -gt "$STALL" ]; then
    echo "BATCH_RESULT {\"reason\":\"STALL\",\"completed_runs\":$DONE,\"waited_s\":$((NOW-START)),\"iso\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}"
    exit 0
  fi

  sleep "$INTERVAL"
done
