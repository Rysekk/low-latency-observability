#!/usr/bin/env bash
set -euo pipefail

log() { echo "[update_dash] $*" >&2; }
trap 'log "FAILED at line $LINENO: $BASH_COMMAND"' ERR

uid="${1:?usage: update_dash.sh <grafana-dashboard-uid>}"
cd "$(git rev-parse --show-toplevel)"
log "repo root: $PWD"

json=observability/grafana/dashboards/dashboard.json
cm=deploy/k8s/monitoring/grafana/configmap-dashboard.yaml

tmp_json="$(mktemp)"
tmp_cm="$(mktemp)"
trap 'rm -f "$tmp_json" "$tmp_cm"' EXIT

log "1/3 exporting dashboard uid=$uid from Grafana..."
curl -sf --max-time 10 "http://grafana.local:30080/api/dashboards/uid/${uid}" \
  | jq '.dashboard | .uid="dash-slo" | .title="Pipeline Latency / SLO" | del(.id, .version)' \
  > "$tmp_json"
log "    OK ($(wc -c < "$tmp_json") bytes)"

log "2/3 generating ConfigMap..."
kubectl create configmap grafana-dashboards -n trading-app \
  --from-file=dashboard.json="$tmp_json" \
  --dry-run=client -o yaml > "$tmp_cm"
log "    OK"

log "3/3 replacing files..."
chmod 644 "$tmp_json" "$tmp_cm"
mv "$tmp_json" "$json"
mv "$tmp_cm" "$cm"
log "    OK: $json"
log "    OK: $cm"

log "done"