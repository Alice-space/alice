#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)"
CASE_DIR="${1:-$ROOT_DIR/data/weather_case/$RUN_ID}"
PORT="${ALICE_WEATHER_PORT:-18080}"
SERVER_URL="http://127.0.0.1:${PORT}"

# Avoid corporate proxy interception for local loopback traffic.
export NO_PROXY="${NO_PROXY:-},127.0.0.1,localhost"
export no_proxy="${no_proxy:-},127.0.0.1,localhost"

mkdir -p "$CASE_DIR/logs" "$CASE_DIR/artifacts" "$CASE_DIR/data"

CONFIG_PATH="$CASE_DIR/config.yaml"
APP_LOG_PATH="$CASE_DIR/logs/alice.app.jsonl"
SERVER_STDOUT_PATH="$CASE_DIR/logs/server.stdout.log"
CLI_SUBMIT_PATH="$CASE_DIR/artifacts/cli.submit.json"
EVENT_LOG_DUMP="$CASE_DIR/artifacts/eventlog.full.jsonl"
EVENT_TYPES_SUMMARY="$CASE_DIR/artifacts/event_types.summary.txt"
EVENT_SEQUENCE="$CASE_DIR/artifacts/event_sequence.tsv"
REQUEST_SNAPSHOT="$CASE_DIR/artifacts/request.snapshot.json"
EVENTS_SNAPSHOT="$CASE_DIR/artifacts/events.snapshot.json"

cat > "$CONFIG_PATH" <<YAML
http:
  listen_addr: "127.0.0.1:${PORT}"

storage:
  root_dir: "${CASE_DIR}/data"
  snapshot_interval: 100

runtime:
  shard_count: 16
  outbox_workers: 2

promotion:
  min_confidence: 0.6

workflow:
  manifest_roots:
    - "configs/workflows"

mcp:
  domains: {}

scheduler:
  poll_interval: "1h"

ops:
  metrics_enabled: true
  admin_event_injection_enabled: true
  admin_schedule_fire_replay_enabled: true

auth:
  admin_token: "dev-admin-token"
  human_action_secret: "dev-human-action-secret"
  github_webhook_secret: "dev-github-webhook-secret"
  gitlab_webhook_secret: "dev-gitlab-webhook-secret"
  scheduler_ingress_secret: "dev-scheduler-ingress-secret"

agent:
  kimi_executable: "kimi"
  work_dir: "."
  timeout: "90s"
  max_steps: 8
  skills_dir: "skills"
  enable_direct_answer: true

logging:
  level: "debug"
  format: "json"
  console: false
  file:
    path: "${APP_LOG_PATH}"
    max_size_mb: 100
    max_backups: 2
    max_age_days: 3
    compress: false
  components:
    bus: "debug"
    reception: "debug"
    direct_answer: "debug"
YAML

pushd "$ROOT_DIR" >/dev/null

go run ./cmd/alice --config "$CONFIG_PATH" serve >"$SERVER_STDOUT_PATH" 2>&1 &
SERVER_PID=$!
cleanup() {
  if kill -0 "$SERVER_PID" >/dev/null 2>&1; then
    kill -TERM "$SERVER_PID" >/dev/null 2>&1 || true
    wait "$SERVER_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

for _ in $(seq 1 120); do
  if curl --noproxy '*' -sf "$SERVER_URL/healthz" >/dev/null; then
    break
  fi
  sleep 0.5
done

if ! curl --noproxy '*' -sf "$SERVER_URL/healthz" >/dev/null; then
  echo "server did not become healthy at ${SERVER_URL}" >&2
  exit 1
fi

IDEMPOTENCY_KEY="idem_weather_case_${RUN_ID}"
CONV_ID="conv_weather_case_${RUN_ID}"

go run ./cmd/alice \
  --server "$SERVER_URL" \
  --timeout 120s \
  --output json \
  submit message \
  --text "查询上海明天天气" \
  --conversation-id "$CONV_ID" \
  --thread-id "root" \
  --idempotency-key "$IDEMPOTENCY_KEY" \
  --wait \
  --wait-timeout 90s | tee "$CLI_SUBMIT_PATH"

REQUEST_ID="$(jq -r '.request_id // empty' "$CLI_SUBMIT_PATH")"
if [[ -n "$REQUEST_ID" ]]; then
  go run ./cmd/alice --server "$SERVER_URL" --timeout 120s --output json get request "$REQUEST_ID" >"$REQUEST_SNAPSHOT"
fi

go run ./cmd/alice --server "$SERVER_URL" --timeout 120s --output json list events --limit 200 >"$EVENTS_SNAPSHOT"

if compgen -G "${CASE_DIR}/data/eventlog/*.jsonl" > /dev/null; then
  cat "${CASE_DIR}"/data/eventlog/*.jsonl > "$EVENT_LOG_DUMP"
  jq -r '.event_type' "$EVENT_LOG_DUMP" | sort | uniq -c > "$EVENT_TYPES_SUMMARY"
  jq -r '[.global_hlc, .event_type, .aggregate_kind, .aggregate_id, .causation_id] | @tsv' "$EVENT_LOG_DUMP" > "$EVENT_SEQUENCE"
fi

kill -TERM "$SERVER_PID" >/dev/null 2>&1 || true
wait "$SERVER_PID" >/dev/null 2>&1 || true

popd >/dev/null

echo "weather case completed"
echo "case_dir=$CASE_DIR"
echo "cli_submit=$CLI_SUBMIT_PATH"
echo "app_log=$APP_LOG_PATH"
echo "agent_context_dir=${CASE_DIR}/logs/agent_context"
echo "eventlog=$EVENT_LOG_DUMP"
echo "event_types=$EVENT_TYPES_SUMMARY"
echo "event_sequence=$EVENT_SEQUENCE"
