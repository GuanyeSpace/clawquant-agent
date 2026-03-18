#!/usr/bin/env bash

set -euo pipefail

SERVICE_NAME="clawquant-agent"
SERVICE_USER="clawquant"
INSTALL_DIR="${INSTALL_DIR:-/opt/clawquant-agent}"
ENV_FILE="${ENV_FILE:-/etc/default/${SERVICE_NAME}}"
SERVICE_FILE="${SERVICE_FILE:-/etc/systemd/system/${SERVICE_NAME}.service}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PACKAGE_DIR="$SCRIPT_DIR"

require_root() {
  if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
    echo "Please run this installer as root." >&2
    exit 1
  fi
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Required command not found: $1" >&2
    exit 1
  fi
}

detect_python() {
  local candidate
  for candidate in python3 python; do
    if command -v "$candidate" >/dev/null 2>&1; then
      if "$candidate" - <<'PY' >/dev/null 2>&1
import sys
raise SystemExit(0 if sys.version_info >= (3, 11) else 1)
PY
      then
        printf '%s\n' "$candidate"
        return 0
      fi
    fi
  done

  echo "Python 3.11 or newer is required." >&2
  exit 1
}

prompt_value() {
  local var_name="$1"
  local prompt_text="$2"
  local secret="${3:-0}"
  local current="${!var_name:-}"

  if [[ -n "$current" ]]; then
    return 0
  fi

  if [[ "$secret" == "1" ]]; then
    read -r -s -p "$prompt_text: " current
    echo
  else
    read -r -p "$prompt_text: " current
  fi

  printf -v "$var_name" '%s' "$current"
}

ensure_service_user() {
  if id -u "$SERVICE_USER" >/dev/null 2>&1; then
    return 0
  fi

  useradd \
    --system \
    --home-dir "$INSTALL_DIR" \
    --create-home \
    --shell /usr/sbin/nologin \
    "$SERVICE_USER"
}

install_payload() {
  mkdir -p "$INSTALL_DIR"
  install -m 0755 "$PACKAGE_DIR/clawquant-agent" "$INSTALL_DIR/clawquant-agent"

  rm -rf "$INSTALL_DIR/sdk"
  cp -a "$PACKAGE_DIR/sdk" "$INSTALL_DIR/sdk"

  install -m 0644 "$PACKAGE_DIR/README.md" "$INSTALL_DIR/README.md"
  install -m 0644 "$PACKAGE_DIR/clawquant-agent.service" "$INSTALL_DIR/clawquant-agent.service"

  mkdir -p "$INSTALL_DIR/data"
  chown -R "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR"
}

install_python_sdk() {
  local python_bin="$1"
  "$python_bin" -m pip --version >/dev/null 2>&1 || "$python_bin" -m ensurepip --upgrade
  "$python_bin" -m pip install -r "$INSTALL_DIR/sdk/requirements.txt"
  "$python_bin" -m pip install -e "$INSTALL_DIR/sdk"
}

write_env_file() {
  umask 077
  cat >"$ENV_FILE" <<EOF
TOKEN=$TOKEN
SECRET=$SECRET
SERVER=$SERVER
CLAWQUANT_ENCRYPTION_KEY=$ENCRYPTION_KEY
EOF
}

install_service() {
  install -m 0644 "$INSTALL_DIR/clawquant-agent.service" "$SERVICE_FILE"
  systemctl daemon-reload
  systemctl enable --now "$SERVICE_NAME"
}

print_summary() {
  echo
  echo "ClawQuant Agent installed."
  echo "  Install dir: $INSTALL_DIR"
  echo "  Service:     $SERVICE_NAME"
  echo "  Env file:    $ENV_FILE"
  echo
  systemctl --no-pager --full status "$SERVICE_NAME" || true
}

main() {
  require_root
  require_cmd systemctl
  require_cmd install
  require_cmd cp

  if [[ ! -f "$PACKAGE_DIR/clawquant-agent" ]]; then
    echo "clawquant-agent binary not found next to install.sh" >&2
    exit 1
  fi

  if [[ ! -d "$PACKAGE_DIR/sdk" ]]; then
    echo "sdk directory not found next to install.sh" >&2
    exit 1
  fi

  if [[ ! -f "$PACKAGE_DIR/clawquant-agent.service" ]]; then
    echo "clawquant-agent.service not found next to install.sh" >&2
    exit 1
  fi

  local python_bin
  python_bin="$(detect_python)"

  prompt_value TOKEN "Agent token"
  prompt_value SECRET "Agent secret" 1
  prompt_value SERVER "Platform server URL (for example: wss://platform.example.com)"
  prompt_value ENCRYPTION_KEY "Encryption key (optional, press Enter to skip)" 1

  ensure_service_user
  install_payload
  install_python_sdk "$python_bin"
  write_env_file
  install_service
  print_summary
}

main "$@"
