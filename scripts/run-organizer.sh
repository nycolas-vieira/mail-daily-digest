#!/usr/bin/env bash

set -uo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOG_FILE="$HOME/.local/logs/mail-daily-digest.log"

export PATH="/usr/local/bin:/opt/homebrew/bin:/Applications/Docker.app/Contents/Resources/bin:$PATH"

export OLLAMA_ENDPOINT="http://host.docker.internal:11434"

PROBE_URL="https://oauth2.googleapis.com"
MAX_TRIES=10
SLEEP_SECS=6

mkdir -p "$(dirname "$LOG_FILE")"

log() {
  printf '%s [run-organizer] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >>"$LOG_FILE"
}

log "=== invoked: args=[$*] ==="

if ! command -v docker >/dev/null 2>&1; then
  log "ERROR: docker CLI not found on PATH. Aborting."
  exit 1
fi
if ! docker info >/dev/null 2>&1; then
  if command -v colima >/dev/null 2>&1; then
    log "Docker daemon down - starting Colima..."
    colima start >>"$LOG_FILE" 2>&1 || true
  fi
  if ! docker info >/dev/null 2>&1; then
    log "ERROR: Docker daemon is not running and could not be started. Aborting."
    exit 1
  fi
  log "Colima is up."
fi

ready=0
i=1
while [ "$i" -le "$MAX_TRIES" ]; do
  if curl -sS -o /dev/null --max-time 5 "$PROBE_URL" 2>/dev/null \
     || nslookup oauth2.googleapis.com >/dev/null 2>&1; then
    ready=1
    log "network ready (attempt $i/$MAX_TRIES)"
    break
  fi
  log "network not ready yet (attempt $i/$MAX_TRIES), retrying in ${SLEEP_SECS}s"
  sleep "$SLEEP_SECS"
  i=$((i + 1))
done

if [ "$ready" -ne 1 ]; then
  log "ERROR: network still unreachable after $MAX_TRIES attempts. Aborting."
  exit 1
fi

cd "$REPO_DIR" || {
  log "ERROR: cannot cd to $REPO_DIR. Aborting."
  exit 1
}

log "running: docker compose run --rm --no-deps organizer $*"

export GID="$(id -g)"
export UID
docker compose run --rm --no-deps organizer "$@" >>"$LOG_FILE" 2>&1
rc=$?

log "=== finished: exit=$rc ==="
exit "$rc"
