#!/usr/bin/env bash
# Orchestrates ONE chaos run and freezes all evidence for it immediately.
#
#   ./run_experiment.sh [workflow.yaml]
#
# Default workflow: chaos/chaos-mesh-workflow.yaml (the full suite).
# Pass chaos/evidence/dryrun-workflow.yaml for a ~2-3 min pipeline validation.
#
# Output: chaos/evidence/runs/run-<UTC>/  with frozen metrics, phases, graphs,
# summary.md  — committable, survives Prometheus TSDB rotation.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
WF_FILE="${1:-$REPO/chaos/chaos-mesh-workflow.yaml}"
NS="${CHAOS_NS:-cloud-nlb}"
RUN_DEADLINE="${RUN_DEADLINE:-7800}"   # 130m default; covers the 95m suite + buffer
PY="$HERE/.venv/bin/python"

[ -f "$WF_FILE" ] || { echo "workflow file not found: $WF_FILE" >&2; exit 1; }
[ -x "$PY" ] || { echo "venv missing — run: make -C $HERE setup" >&2; exit 1; }

WF_NAME="$(awk '/^metadata:/{m=1} m&&/^  name:/{print $2; exit}' "$WF_FILE")"
[ -n "$WF_NAME" ] || { echo "cannot parse workflow metadata.name from $WF_FILE" >&2; exit 1; }

RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)"
RUN_DIR="$HERE/runs/run-$RUN_ID"
mkdir -p "$RUN_DIR/metrics" "$RUN_DIR/graphs"
T0=$(date +%s)
cat > "$RUN_DIR/run.json" <<EOF
{"run_id":"$RUN_ID","wf_file":"$(basename "$WF_FILE")","wf_name":"$WF_NAME","namespace":"$NS","t0":$T0}
EOF
echo "[run] $RUN_ID  workflow=$WF_NAME  dir=$RUN_DIR"

# Delete any prior instance and wait until it is fully gone. This defeats the
# stale-status race: kubectl replace/create can otherwise race the chaos-mesh
# controller and a freshly-applied workflow reads the PREVIOUS run's terminal
# Accomplished=True for a few seconds.
kubectl delete workflow "$WF_NAME" -n "$NS" --ignore-not-found --wait=false >/dev/null 2>&1 || true
for _ in $(seq 1 60); do
  kubectl get workflow "$WF_NAME" -n "$NS" >/dev/null 2>&1 || break
  sleep 2
done

kubectl create -f "$WF_FILE" >/dev/null
echo "[run] workflow created, waiting for controller to own the run"

# Wait until the controller has actually started THIS run (status populated).
OWNED=0
for _ in $(seq 1 40); do
  ST="$(kubectl get workflow "$WF_NAME" -n "$NS" -o jsonpath='{.status.startTime}' 2>/dev/null || true)"
  EN="$(kubectl get workflow "$WF_NAME" -n "$NS" -o jsonpath='{.status.entryNode}' 2>/dev/null || true)"
  if [ -n "$ST" ] && [ -n "$EN" ]; then OWNED=1; echo "[run] run owned: entry=$EN start=$ST"; break; fi
  sleep 3
done
[ "$OWNED" = 1 ] || echo "[run] WARNING: controller did not populate status; proceeding" >&2

# Start the durable phase-timeline poller only once the real run is underway.
WF_NAME="$WF_NAME" CHAOS_NS="$NS" bash "$HERE/poll_timeline.sh" "$RUN_DIR/timeline.jsonl" &
POLLER=$!
cleanup() { kill "$POLLER" 2>/dev/null || true; }
trap cleanup EXIT INT TERM

echo "[run] waiting for completion (deadline ${RUN_DEADLINE}s)"
WAITED=0
while :; do
  J="$(kubectl get workflow "$WF_NAME" -n "$NS" -o json 2>/dev/null || echo '{}')"
  ACC="$(echo "$J" | jq -r '(.status.conditions // []) | map(select(.type=="Accomplished")) | (.[0].status // "")')"
  ST="$(echo "$J" | jq -r '.status.startTime // ""')"
  ET="$(echo "$J" | jq -r '.status.endTime // ""')"
  PH="$(echo "$J" | jq -r '.status.phase // ""')"
  # Done only if Accomplished AND a real (non-zero) duration elapsed. A
  # zero-duration Accomplished (endTime == startTime) is a stale artifact.
  if [ "$ACC" = "True" ] && [ -n "$ET" ] && [ "$ET" != "$ST" ]; then
    echo "[run] workflow finished (start=$ST end=$ET)"; break
  fi
  if [ "$PH" = "Failed" ]; then echo "[run] workflow Failed"; break; fi
  if [ "$WAITED" -ge "$RUN_DEADLINE" ]; then
    echo "[run] deadline reached, stopping (workflow may be incomplete)" >&2; break
  fi
  sleep 15; WAITED=$((WAITED+15))
done

# Capture the post-chaos recovery tail before freezing. Keep the poller alive
# so timeline.jsonl also covers it. Without this, build_evidence's
# [t0-60, t1+60] window clips the last phase's return-to-baseline (observed:
# Phase-4 backend_unavail mis-counted 340 vs true 310).
TAIL="${POST_RECOVERY_TAIL:-300}"
echo "[run] workflow done; capturing ${TAIL}s recovery tail before freezing"
sleep "$TAIL"

T1=$(date +%s)
cleanup; trap - EXIT INT TERM

# Freeze raw chaos-mesh state alongside the timeline.
kubectl get workflow "$WF_NAME" -n "$NS" -o yaml > "$RUN_DIR/workflow-status.yaml" 2>/dev/null || true
kubectl get workflownodes -n "$NS" -o yaml     > "$RUN_DIR/workflownodes.yaml"   2>/dev/null || true

# Validity gate: the stale-status race can make the completion detector fire on
# a PREVIOUS run's terminal Accomplished, producing a short corrupt run. The
# frozen status is authoritative — if it is not genuinely Accomplished with a
# real endTime, self-quarantine the dir so it never pollutes the aggregate
# (aggregate.py globs runs/run-*; an _invalid- prefix is excluded).
FACC="$(kubectl get workflow "$WF_NAME" -n "$NS" -o jsonpath='{.status.conditions[?(@.type=="Accomplished")].status}' 2>/dev/null || true)"
FEND="$(kubectl get workflow "$WF_NAME" -n "$NS" -o jsonpath='{.status.endTime}' 2>/dev/null || true)"
FSTA="$(kubectl get workflow "$WF_NAME" -n "$NS" -o jsonpath='{.status.startTime}' 2>/dev/null || true)"
if [ "$FACC" != "True" ] || [ -z "$FEND" ] || [ "$FEND" = "$FSTA" ]; then
  BAD="$(dirname "$RUN_DIR")/_invalid-$(basename "$RUN_DIR")"
  echo "[run] INVALID run (Accomplished=$FACC endTime='$FEND') — quarantining -> $BAD" >&2
  mv "$RUN_DIR" "$BAD" 2>/dev/null || true
  exit 2
fi

# Build frozen evidence (Prometheus query_range -> JSON + graphs + summary).
"$PY" "$HERE/build_evidence.py" --run-dir "$RUN_DIR" --t0 "$T0" --t1 "$T1"

echo "[run] DONE -> $RUN_DIR"
ls -1 "$RUN_DIR/graphs" 2>/dev/null || true
