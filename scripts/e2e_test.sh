#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

API_BASE_URL="${API_BASE_URL:-http://localhost:8080/api}"
SERVER_BASE_URL="${SERVER_BASE_URL:-http://localhost:8080}"
AGENT_SERVER_URL="${AGENT_SERVER_URL:-ws://localhost:8080}"
WAIT_TIMEOUT="${E2E_WAIT_TIMEOUT:-90}"
POLL_INTERVAL="${E2E_POLL_INTERVAL:-2}"
E2E_SKIP_BUILD="${E2E_SKIP_BUILD:-0}"
E2E_KEEP_ARTIFACTS="${E2E_KEEP_ARTIFACTS:-0}"

if [[ -n "${AGENT_BIN:-}" ]]; then
  AGENT_BINARY="$AGENT_BIN"
else
  AGENT_BINARY="$REPO_ROOT/bin/clawquant-agent"
  case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*)
      AGENT_BINARY="${AGENT_BINARY}.exe"
      ;;
  esac
fi

detect_python() {
  if [[ -n "${PYTHON_BIN:-}" ]]; then
    if python_works "$PYTHON_BIN"; then
      printf '%s\n' "$PYTHON_BIN"
      return
    fi
    echo "Configured PYTHON_BIN not found: $PYTHON_BIN" >&2
    exit 1
  fi

  if python_works python3; then
    printf '%s\n' "python3"
    return
  fi

  if python_works python; then
    printf '%s\n' "python"
    return
  fi

  if command -v py >/dev/null 2>&1; then
    local resolved
    resolved="$(py -3 -c 'import sys; print(sys.executable)' 2>/dev/null | tr -d '\r')"
    if [[ -n "$resolved" ]]; then
      if command -v cygpath >/dev/null 2>&1; then
        resolved="$(cygpath -u "$resolved")"
      fi
      if python_works "$resolved"; then
        printf '%s\n' "$resolved"
        return
      fi
    fi
  fi

  echo "python3 or python is required" >&2
  exit 1
}

detect_node() {
  if [[ -n "${NODE_BIN:-}" ]]; then
    if command -v "$NODE_BIN" >/dev/null 2>&1; then
      printf '%s\n' "$NODE_BIN"
      return
    fi
    echo "Configured NODE_BIN not found: $NODE_BIN" >&2
    exit 1
  fi

  if command -v node >/dev/null 2>&1; then
    printf '%s\n' "node"
    return
  fi

  if command -v nodejs >/dev/null 2>&1; then
    printf '%s\n' "nodejs"
    return
  fi

  echo "node or nodejs is required to generate encrypted exchange credentials" >&2
  exit 1
}

require_cmd() {
  if [[ "$1" == */* || "$1" == *\\* ]]; then
    if [[ -x "$1" || -f "$1" ]]; then
      return
    fi
    echo "Required executable not found: $1" >&2
    exit 1
  fi

  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Required command not found: $1" >&2
    exit 1
  fi
}

python_works() {
  local candidate="$1"
  if [[ "$candidate" == */* || "$candidate" == *\\* ]]; then
    [[ -x "$candidate" || -f "$candidate" ]] || return 1
  else
    command -v "$candidate" >/dev/null 2>&1 || return 1
  fi

  "$candidate" -c 'import sys' >/dev/null 2>&1
}

PYTHON_BIN="$(detect_python)"
require_cmd "$PYTHON_BIN"
require_cmd curl
require_cmd go

FIXTURE_PATH="${FIXTURE_PATH:-$REPO_ROOT/sdk/tests/fixtures/test_strategy.py}"
if [[ ! -f "$FIXTURE_PATH" ]]; then
  echo "Strategy fixture not found: $FIXTURE_PATH" >&2
  exit 1
fi

if [[ -z "${CLAWQUANT_ENCRYPTED_API_KEY:-}" || -z "${CLAWQUANT_ENCRYPTED_SECRET:-}" ]]; then
  NODE_BIN="$(detect_node)"
  require_cmd "$NODE_BIN"
fi

json_get() {
  local path="$1"
  "$PYTHON_BIN" - "$path" <<'PY'
import json
import sys

path = [part for part in sys.argv[1].split(".") if part]
value = json.load(sys.stdin)

for part in path:
    if isinstance(value, list):
        value = value[int(part)]
    else:
        value = value[part]

if isinstance(value, (dict, list)):
    print(json.dumps(value, ensure_ascii=False, separators=(",", ":")))
elif value is None:
    print("")
else:
    print(value)
PY
}

json_array_field_by_id() {
  local wanted_id="$1"
  local field="$2"
  "$PYTHON_BIN" - "$wanted_id" "$field" <<'PY'
import json
import sys

wanted_id, field = sys.argv[1], sys.argv[2]
items = json.load(sys.stdin)
for item in items:
    if str(item.get("id", "")) == wanted_id:
        value = item.get(field, "")
        if isinstance(value, (dict, list)):
            print(json.dumps(value, ensure_ascii=False, separators=(",", ":")))
        elif value is None:
            print("")
        else:
            print(value)
        break
PY
}

request() {
  local method="$1"
  local path="$2"
  local body="${3-__NO_BODY__}"
  local with_auth="${4:-1}"
  local response
  local curl_args=(
    -sS
    -w
    $'\n%{http_code}'
    -X
    "$method"
    "${API_BASE_URL}${path}"
    -H
    "Accept: application/json"
  )

  if [[ "$with_auth" == "1" && -n "${AUTH_TOKEN:-}" ]]; then
    curl_args+=(-H "Authorization: Bearer ${AUTH_TOKEN}")
  fi

  if [[ "$body" != "__NO_BODY__" ]]; then
    curl_args+=(
      -H
      "Content-Type: application/json"
      --data
      "$body"
    )
  fi

  response="$(curl "${curl_args[@]}")"
  HTTP_STATUS="${response##*$'\n'}"
  HTTP_BODY="${response%$'\n'*}"
}

expect_status() {
  local expected="$1"
  if [[ "$HTTP_STATUS" != "$expected" ]]; then
    echo "Unexpected HTTP status: expected $expected, got $HTTP_STATUS" >&2
    if [[ -n "$HTTP_BODY" ]]; then
      echo "$HTTP_BODY" >&2
    fi
    exit 1
  fi
}

log_step() {
  printf '\n==> %s\n' "$1"
}

wait_until() {
  local description="$1"
  local timeout="$2"
  shift 2

  local deadline=$((SECONDS + timeout))
  while (( SECONDS < deadline )); do
    if "$@"; then
      return 0
    fi
    sleep "$POLL_INTERVAL"
  done

  echo "Timed out waiting for: $description" >&2
  return 1
}

platform_ready() {
  curl -fsS "${SERVER_BASE_URL}/readyz" >/dev/null 2>&1
}

agent_online() {
  request GET "/agents"
  [[ "$HTTP_STATUS" == "200" ]] || return 1
  local status
  status="$(printf '%s' "$HTTP_BODY" | json_array_field_by_id "$AGENT_ID" "status")"
  [[ "$status" == "online" ]]
}

bot_status_is() {
  local expected="$1"
  request GET "/bots/${BOT_ID}"
  [[ "$HTTP_STATUS" == "200" ]] || return 1
  local actual
  actual="$(printf '%s' "$HTTP_BODY" | json_get "status")"
  [[ "$actual" == "$expected" ]]
}

local_log_rows() {
  "$PYTHON_BIN" - "$AGENT_DB_PATH" "$BOT_ID" <<'PY'
import sqlite3
import sys

db_path, bot_id = sys.argv[1], sys.argv[2]
conn = sqlite3.connect(db_path)
try:
    try:
        row = conn.execute(
            "SELECT COUNT(*) FROM logs WHERE bot_id = ?",
            (bot_id,),
        ).fetchone()
        print(row[0] if row else 0)
    except sqlite3.Error:
        print(0)
finally:
    conn.close()
PY
}

storage_value() {
  "$PYTHON_BIN" - "$BOT_DB_PATH" "$BOT_ID" "$1" <<'PY'
import json
import sqlite3
import sys

db_path, bot_id, key = sys.argv[1], sys.argv[2], sys.argv[3]
conn = sqlite3.connect(db_path)
try:
    try:
        row = conn.execute(
            "SELECT value FROM storage WHERE bot_id = ? AND key = ?",
            (bot_id, key),
        ).fetchone()
    except sqlite3.Error:
        row = None

    if not row:
        print("")
    else:
        print(json.loads(row[0]))
finally:
    conn.close()
PY
}

local_logs_ready() {
  [[ -f "$AGENT_DB_PATH" ]] || return 1
  local count
  count="$(local_log_rows)"
  [[ "${count:-0}" -ge 4 ]]
}

storage_run_count_is() {
  [[ -f "$BOT_DB_PATH" ]] || return 1
  local count
  count="$(storage_value "run_count")"
  [[ "$count" == "$1" ]]
}

encrypt_secret() {
  local plaintext="$1"
  "$NODE_BIN" - "$plaintext" "$ENCRYPTION_KEY" <<'NODE'
const crypto = require("crypto");

const plaintext = process.argv[2];
const password = process.argv[3];

if (!password || !password.trim()) {
  throw new Error("Encryption password is required.");
}

const salt = crypto.randomBytes(16);
const iv = crypto.randomBytes(12);
const key = crypto.pbkdf2Sync(password, salt, 100000, 32, "sha256");
const cipher = crypto.createCipheriv("aes-256-gcm", key, iv);
const ciphertext = Buffer.concat([cipher.update(plaintext, "utf8"), cipher.final()]);
const authTag = cipher.getAuthTag();
const payload = Buffer.concat([salt, iv, ciphertext, authTag]);
process.stdout.write(payload.toString("base64"));
NODE
}

build_strategy_payload() {
  "$PYTHON_BIN" - "$FIXTURE_PATH" <<'PY'
import json
import sys
from pathlib import Path

strategy_path = Path(sys.argv[1])
payload = {
    "name": f"E2E Strategy {strategy_path.stat().st_mtime_ns}",
    "code": strategy_path.read_text(encoding="utf-8"),
    "params_schema": {
        "fast_period": {"type": "number", "default": 5},
        "slow_period": {"type": "number", "default": 20},
    },
}
print(json.dumps(payload, ensure_ascii=False))
PY
}

build_exchange_payload() {
  "$PYTHON_BIN" - "$ENCRYPTED_API_KEY" "$ENCRYPTED_SECRET" <<'PY'
import json
import sys
import time

payload = {
    "name": f"E2E Exchange {int(time.time())}",
    "exchange_type": "binance",
    "encrypted_api_key": sys.argv[1],
    "encrypted_secret": sys.argv[2],
}
print(json.dumps(payload, ensure_ascii=False))
PY
}

build_agent_payload() {
  "$PYTHON_BIN" <<'PY'
import json
import time

payload = {"name": f"E2E Agent {int(time.time())}"}
print(json.dumps(payload, ensure_ascii=False))
PY
}

build_bot_payload() {
  "$PYTHON_BIN" - "$STRATEGY_ID" "$AGENT_ID" "$EXCHANGE_ID" <<'PY'
import json
import sys
import time

payload = {
    "name": f"E2E Bot {int(time.time())}",
    "strategy_id": sys.argv[1],
    "agent_id": sys.argv[2],
    "exchange_config_id": sys.argv[3],
    "trading_pair": "BTC_USDT",
    "params": {
        "fast_period": 5,
        "slow_period": 20,
    },
}
print(json.dumps(payload, ensure_ascii=False))
PY
}

cleanup() {
  local exit_code="$?"

  if [[ -n "${AUTH_TOKEN:-}" && -n "${BOT_ID:-}" ]]; then
    request POST "/bots/${BOT_ID}/stop" "{}" || true
  fi

  if [[ -n "${AGENT_PID:-}" ]]; then
    kill "$AGENT_PID" >/dev/null 2>&1 || true
    wait "$AGENT_PID" >/dev/null 2>&1 || true
  fi

  if [[ "$E2E_KEEP_ARTIFACTS" != "1" && -n "${RUNTIME_DIR:-}" && -d "${RUNTIME_DIR}" ]]; then
    rm -rf "$RUNTIME_DIR"
  fi

  exit "$exit_code"
}

trap cleanup EXIT

wait_until "platform readiness" "$WAIT_TIMEOUT" platform_ready

if [[ "$E2E_SKIP_BUILD" != "1" ]]; then
  log_step "Building agent binary"
  mkdir -p "$(dirname "$AGENT_BINARY")"
  (
    cd "$REPO_ROOT"
    go build -trimpath -o "$AGENT_BINARY" ./cmd/agent
  )
fi

if [[ ! -x "$AGENT_BINARY" && ! -f "$AGENT_BINARY" ]]; then
  echo "Agent binary not found: $AGENT_BINARY" >&2
  exit 1
fi

RUNTIME_DIR="${E2E_RUNTIME_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/clawquant-agent-e2e.XXXXXX")}"
AGENT_DATA_DIR="${E2E_AGENT_DATA_DIR:-$RUNTIME_DIR/agent-data}"
AGENT_LOG_FILE="$RUNTIME_DIR/agent.log"
mkdir -p "$AGENT_DATA_DIR"

USERNAME="${E2E_USERNAME:-e2e-$(date +%s)-$RANDOM}"
PASSWORD="${E2E_PASSWORD:-Password123!}"
TEST_API_KEY="${E2E_API_KEY:-test-api-key}"
TEST_API_SECRET="${E2E_API_SECRET:-test-secret}"
ENCRYPTION_KEY="${CLAWQUANT_ENCRYPTION_KEY:-}"
if [[ -z "$ENCRYPTION_KEY" ]]; then
  ENCRYPTION_KEY="$("$PYTHON_BIN" - <<'PY'
import secrets
print(secrets.token_urlsafe(32))
PY
)"
fi

if [[ -n "${CLAWQUANT_ENCRYPTED_API_KEY:-}" ]]; then
  ENCRYPTED_API_KEY="$CLAWQUANT_ENCRYPTED_API_KEY"
else
  ENCRYPTED_API_KEY="$(encrypt_secret "$TEST_API_KEY")"
fi

if [[ -n "${CLAWQUANT_ENCRYPTED_SECRET:-}" ]]; then
  ENCRYPTED_SECRET="$CLAWQUANT_ENCRYPTED_SECRET"
else
  ENCRYPTED_SECRET="$(encrypt_secret "$TEST_API_SECRET")"
fi

log_step "Registering or logging in test user"
AUTH_PAYLOAD="$("$PYTHON_BIN" - "$USERNAME" "$PASSWORD" <<'PY'
import json
import sys
print(json.dumps({"username": sys.argv[1], "password": sys.argv[2]}))
PY
)"
request POST "/auth/register" "$AUTH_PAYLOAD" 0
case "$HTTP_STATUS" in
  201)
    AUTH_TOKEN="$(printf '%s' "$HTTP_BODY" | json_get "token")"
    ;;
  409)
    request POST "/auth/login" "$AUTH_PAYLOAD" 0
    expect_status "200"
    AUTH_TOKEN="$(printf '%s' "$HTTP_BODY" | json_get "token")"
    ;;
  *)
    echo "Register failed before E2E could continue." >&2
    if [[ -n "$HTTP_BODY" ]]; then
      echo "$HTTP_BODY" >&2
    fi
    exit 1
    ;;
esac

log_step "Creating strategy"
request POST "/strategies" "$(build_strategy_payload)"
expect_status "201"
STRATEGY_ID="$(printf '%s' "$HTTP_BODY" | json_get "id")"

log_step "Creating exchange config"
request POST "/exchanges" "$(build_exchange_payload)"
expect_status "201"
EXCHANGE_ID="$(printf '%s' "$HTTP_BODY" | json_get "id")"

log_step "Creating agent credentials"
request POST "/agents" "$(build_agent_payload)"
expect_status "201"
AGENT_ID="$(printf '%s' "$HTTP_BODY" | json_get "id")"
AGENT_TOKEN="$(printf '%s' "$HTTP_BODY" | json_get "token")"
AGENT_SECRET="$(printf '%s' "$HTTP_BODY" | json_get "secret")"

log_step "Starting local agent"
(
  cd "$REPO_ROOT"
  export CLAWQUANT_ENCRYPTION_KEY="$ENCRYPTION_KEY"
  "$AGENT_BINARY" \
    --token "$AGENT_TOKEN" \
    --secret "$AGENT_SECRET" \
    --server "$AGENT_SERVER_URL" \
    --data-dir "$AGENT_DATA_DIR"
) >"$AGENT_LOG_FILE" 2>&1 &
AGENT_PID="$!"

wait_until "agent online" "$WAIT_TIMEOUT" agent_online

log_step "Creating bot"
request POST "/bots" "$(build_bot_payload)"
expect_status "201"
BOT_ID="$(printf '%s' "$HTTP_BODY" | json_get "id")"
AGENT_DB_PATH="$AGENT_DATA_DIR/agent.db"
BOT_DB_PATH="$AGENT_DATA_DIR/bots/$BOT_ID/agent.db"

log_step "Starting bot and waiting for first run to complete"
request POST "/bots/${BOT_ID}/start" "{}"
expect_status "200"
wait_until "local log capture" "$WAIT_TIMEOUT" local_logs_ready
wait_until "bot storage run_count=1" "$WAIT_TIMEOUT" storage_run_count_is 1
wait_until "bot stopped after normal exit" "$WAIT_TIMEOUT" bot_status_is "stopped"

log_step "Restarting bot and stopping it explicitly"
request POST "/bots/${BOT_ID}/start" "{}"
expect_status "200"
wait_until "bot storage run_count=2" "$WAIT_TIMEOUT" storage_run_count_is 2
request POST "/bots/${BOT_ID}/stop" "{}"
expect_status "200"
wait_until "bot stopped after stop command" "$WAIT_TIMEOUT" bot_status_is "stopped"

log_step "Inspecting logs"
request GET "/bots/${BOT_ID}/logs"
if [[ "$HTTP_STATUS" == "200" ]]; then
  printf '%s\n' "$HTTP_BODY"
else
  echo "Platform log endpoint unavailable (status $HTTP_STATUS), falling back to local SQLite cache."
  "$PYTHON_BIN" - "$AGENT_DB_PATH" "$BOT_ID" <<'PY'
import sqlite3
import sys

db_path, bot_id = sys.argv[1], sys.argv[2]
conn = sqlite3.connect(db_path)
try:
    rows = conn.execute(
        """
        SELECT level, message, created_at
        FROM logs
        WHERE bot_id = ?
        ORDER BY id ASC
        LIMIT 10
        """,
        (bot_id,),
    ).fetchall()
    for level, message, created_at in rows:
        print(f"[{created_at}] {level}: {message}")
finally:
    conn.close()
PY
fi

log_step "Artifacts"
if [[ "$E2E_KEEP_ARTIFACTS" == "1" ]]; then
  echo "Runtime directory: $RUNTIME_DIR"
  echo "Agent log file: $AGENT_LOG_FILE"
  echo "Agent SQLite: $AGENT_DB_PATH"
  echo "Bot SQLite: $BOT_DB_PATH"
else
  echo "Artifacts will be removed on exit. Set E2E_KEEP_ARTIFACTS=1 to keep $RUNTIME_DIR."
fi

log_step "E2E completed successfully"
