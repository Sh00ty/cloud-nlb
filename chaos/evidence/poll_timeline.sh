#!/usr/bin/env bash
# Durable phase-timeline recorder.
#
# Samples chaos-mesh CR state + workflow status every POLL_INTERVAL seconds and
# appends one JSON line per tick. This raw timeseries is the source of truth for
# "which scenario was active when" — derived offline by build_evidence.py, so it
# survives TSDB rotation and k8s event/log expiry.
#
# Usage: poll_timeline.sh <output.jsonl>
set -u

OUT="${1:?usage: poll_timeline.sh <output.jsonl>}"
NS="${CHAOS_NS:-cloud-nlb}"
WF="${WF_NAME:-nlb-resilience-suite-colima-safe}"
INTERVAL="${POLL_INTERVAL:-5}"
KINDS="podchaos,networkchaos,stresschaos,dnschaos,iochaos,timechaos,httpchaos"

while true; do
  TS=$(date +%s)
  ISO=$(date -u +%Y-%m-%dT%H:%M:%SZ)

  CHAOS=$(kubectl get $KINDS -n "$NS" -o json 2>/dev/null \
    | jq -c '[.items[] | {
        kind:.kind,
        name:.metadata.name,
        created:.metadata.creationTimestamp,
        conds:([.status.conditions[]? | {(.type):.status}] | add // {})
      }]' 2>/dev/null) || CHAOS='[]'
  [ -z "$CHAOS" ] && CHAOS='[]'

  WF_JSON=$(kubectl get workflow "$WF" -n "$NS" -o json 2>/dev/null \
    | jq -c '{
        phase:(.status.phase // ((.status.conditions // []) | map(select(.type=="Accomplished")) | (if (.[0].status // "")=="True" then "Accomplished" else "Running" end))),
        entry:(.status.entryNode // "")
      }' 2>/dev/null) || WF_JSON='{}'
  [ -z "$WF_JSON" ] && WF_JSON='{}'

  echo "{\"ts\":$TS,\"iso\":\"$ISO\",\"chaos\":$CHAOS,\"workflow\":$WF_JSON}" >> "$OUT"
  sleep "$INTERVAL"
done
