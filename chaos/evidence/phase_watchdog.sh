#!/usr/bin/env bash
# Phase/incident watchdog. Blocks until ONE reportable event, then exits.
#
#   phase_watchdog.sh <run-dir> <baseline-phase> [run-experiment-pid]
#
# Reportable events (exit 0, emits one JSON line "WATCHDOG_RESULT {...}"):
#   PHASE_ADVANCE  - chaos suite moved to a higher phase than baseline
#   INCIDENT       - success rate < 0.95 OR RPS collapse, sustained 3 polls
#   WORKFLOW_END   - workflow Accomplished/Succeeded/Failed, or runner gone
#   WORKFLOW_HANG  - timeline poller stale, or no progress for >12m
#
# Phase is derived monotonically from the active chaos CR name prefix in
# timeline.jsonl: single-=1 pair-=2 triple-=3 chaos-=4. This survives cooldown
# gaps (no active chaos) by taking the max phase ever seen.
set -u

RUN_DIR="${1:?usage: phase_watchdog.sh <run-dir> <baseline-phase> [runner-pid]}"
BASELINE="${2:?baseline phase required}"
RUNNER_PID="${3:-}"
TL="$RUN_DIR/timeline.jsonl"
PROM_NS="${PROM_NS:-monitoring}"
PROM_POD="${PROM_POD:-$(kubectl get pods -n "$PROM_NS" -o name 2>/dev/null | grep -m1 prometheus | cut -d/ -f2)}"
INTERVAL="${WD_INTERVAL:-20}"
SR_FLOOR="${SR_FLOOR:-0.95}"
RPS_FLOOR="${RPS_FLOOR:-1.0}"
HANG_SECS="${HANG_SECS:-720}"     # 12m of zero forward progress => hang
STALE_SECS="${STALE_SECS:-90}"    # poller silent this long => poller dead

phase_of_name() {
  case "$1" in
    single-*) echo 1 ;;
    pair-*)   echo 2 ;;
    triple-*) echo 3 ;;
    chaos-*)  echo 4 ;;
    *)        echo 0 ;;
  esac
}

# Max phase ever observed across the whole timeline (monotonic).
current_phase() {
  jq -r '.chaos[]?.name' "$TL" 2>/dev/null | while read -r n; do phase_of_name "$n"; done \
    | sort -n | tail -1
}

# Count of distinct chaos-CR names ever seen. A single phase legitimately spans
# ~10 scenarios over 40-50m, so "phase unchanged" is NOT a hang signal; "no new
# distinct chaos CR for a long time while Running" is.
distinct_chaos_count() {
  jq -r '.chaos[]?.name' "$TL" 2>/dev/null | sort -u | grep -c . || echo 0
}

# success rate + total rps over last 1m from Prometheus testclient metrics.
prom_query() {
  local q enc
  q="$1"
  enc=$(python3 -c 'import urllib.parse,sys;print(urllib.parse.quote(sys.argv[1]))' "$q")
  kubectl exec -n "$PROM_NS" "$PROM_POD" -c prometheus -- \
    wget -qO- "http://localhost:9090/api/v1/query?query=$enc" 2>/dev/null \
    | python3 -c 'import sys,json
try:
  r=json.load(sys.stdin)["data"]["result"]
  print(r[0]["value"][1] if r else "")
except Exception:
  print("")' 2>/dev/null
}

emit() {
  # $1 reason  $2 detail
  # rps fields are OFFERED load (2xx + non2xx + transport errors). succ_rps is
  # 2xx only; err_rps is transport errors. sr = 2xx / offered (canonical NIR
  # availability, matches build_evidence.py — connection failures live in a
  # SEPARATE counter, not in testclient_requests_total).
  printf 'WATCHDOG_RESULT {"reason":"%s","baseline_phase":%s,"phase_now":%s,"min_sr":%s,"min_succ_rps":%s,"last_sr":%s,"last_succ_rps":%s,"last_err_rps":%s,"last_offered":%s,"active_chaos":"%s","iso":"%s","detail":"%s"}\n' \
    "$1" "$BASELINE" "${PNOW:-$BASELINE}" "${MIN_SR:-1}" "${MIN_SUCC:-0}" "${LAST_SR:-1}" "${LAST_SUCC:-0}" "${LAST_ERR:-0}" "${LAST_OFF:-0}" \
    "$(tail -1 "$TL" 2>/dev/null | jq -r '[.chaos[]?.name]|join(",")' 2>/dev/null)" \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$2"
}

MIN_SR=1; MIN_SUCC=999999; LAST_SR=1; LAST_SUCC=0; LAST_ERR=0; LAST_OFF=0
BAD=0; LAST_PROGRESS=$(date +%s); PREV_DISTINCT=$(distinct_chaos_count)

while true; do
  NOW=$(date +%s)

  # --- poller liveness: timeline must keep growing ---
  if [ -f "$TL" ]; then
    MT=$(stat -f %m "$TL" 2>/dev/null || echo 0)
    if [ $((NOW - MT)) -gt "$STALE_SECS" ]; then
      emit WORKFLOW_HANG "timeline poller silent for $((NOW-MT))s (poll_timeline.sh likely dead)"
      exit 0
    fi
  fi

  # --- runner liveness / workflow terminal state ---
  if [ -n "$RUNNER_PID" ] && ! kill -0 "$RUNNER_PID" 2>/dev/null; then
    emit WORKFLOW_END "run_experiment.sh (pid $RUNNER_PID) exited; evidence build should be done"
    exit 0
  fi
  WF_PHASE=$(tail -1 "$TL" 2>/dev/null | jq -r '.workflow.phase // ""' 2>/dev/null)
  case "$WF_PHASE" in
    Accomplished|Succeeded|Failed)
      emit WORKFLOW_END "workflow.phase=$WF_PHASE"
      exit 0 ;;
  esac

  # --- forward-progress tracking: a new distinct chaos scenario started ---
  DCOUNT=$(distinct_chaos_count)
  if [ "$DCOUNT" -gt "$PREV_DISTINCT" ]; then LAST_PROGRESS=$NOW; PREV_DISTINCT="$DCOUNT"; fi

  # --- phase progression ---
  PNOW=$(current_phase); [ -z "$PNOW" ] && PNOW="$BASELINE"
  if [ "$PNOW" -gt "$BASELINE" ]; then
    emit PHASE_ADVANCE "suite advanced phase $BASELINE -> $PNOW"
    exit 0
  fi

  # --- traffic health (canonical 3-series, [30s] window like build_evidence) ---
  SUCC=$(prom_query 'sum(rate(testclient_requests_total{code=~"2.."}[30s]))')
  NON=$(prom_query 'sum(rate(testclient_requests_total{code!~"2..",code!=""}[30s]))')
  ERR=$(prom_query 'sum(rate(testclient_request_errors_total[30s]))')
  if [ -n "$SUCC" ] && [ -n "$ERR" ]; then
    : "${NON:=0}"; [ -z "$NON" ] && NON=0
    read -r SR OFF <<EOF
$(python3 -c "
s=float('$SUCC' or 0); n=float('$NON' or 0); e=float('$ERR' or 0)
off=s+n+e
print(round(s/off,4) if off>1e-9 else 1.0, round(off,3))" 2>/dev/null || echo "1 0")
EOF
    LAST_SR="$SR"; LAST_SUCC="$SUCC"; LAST_ERR="$ERR"; LAST_OFF="$OFF"
    MIN_SR=$(python3 -c "print(min(float('$MIN_SR'),float('$SR')))" 2>/dev/null || echo "$MIN_SR")
    MIN_SUCC=$(python3 -c "print(min(float('$MIN_SUCC'),float('$SUCC' or 0)))" 2>/dev/null || echo "$MIN_SUCC")
    # ЧП: success fraction below floor, OR successful throughput collapsed to ~0
    # (canonical outage def) while load is being offered. Sustained 3 polls to
    # ignore single-scrape transients at injection edges.
    BADNOW=$(python3 -c "
sr=float('$SR'); s=float('$SUCC' or 0); off=float('$OFF' or 0)
print(1 if (sr<$SR_FLOOR or (s<$RPS_FLOOR and off>5)) else 0)" 2>/dev/null || echo 0)
    if [ "$BADNOW" = 1 ]; then BAD=$((BAD+1)); else BAD=0; fi
    if [ "$BAD" -ge 3 ]; then
      emit INCIDENT "sustained outage: succ=${SUCC}rps txErr=${ERR}rps offered=${OFF}rps sr=${SR} (sr<$SR_FLOOR or succ≈0, 3 polls) during phase $PNOW"
      exit 0
    fi
  fi

  # --- hang heuristic: Running but no NEW chaos scenario for too long ---
  if [ "$WF_PHASE" = "Running" ] && [ $((NOW - LAST_PROGRESS)) -gt "$HANG_SECS" ]; then
    emit WORKFLOW_HANG "no new chaos scenario for $((NOW-LAST_PROGRESS))s while workflow Running (stuck in phase $PNOW; $DCOUNT scenarios seen)"
    exit 0
  fi

  sleep "$INTERVAL"
done
