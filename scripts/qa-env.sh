#!/usr/bin/env bash
#
# Boots a throwaway backend + frontend pair for browser QA.
#
# This exists so an agent driving a browser can exercise the real app without
# touching ./data or fighting `make dev-backend` / `make dev-frontend` for
# ports: the QA stack gets its own SQLite database under .qa/data and its own
# pair of ports, so it can run alongside the stack you're using by hand.
#
# The database starts empty, which is also how the QA account gets created:
# /api/auth/register is public for the very first account on an instance
# (ADR-0044), so a fresh data dir bootstraps without ENABLE_SIGNUPS.
#
#   scripts/qa-env.sh up      start both servers and seed the QA account
#   scripts/qa-env.sh down    stop both servers, leave the database in place
#   scripts/qa-env.sh reset   down, delete the database, up
#   scripts/qa-env.sh status  report what's running and on which ports
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
QA_DIR="$ROOT/.qa"
DATA_DIR="$QA_DIR/data"
LOG_DIR="$QA_DIR/logs"
RUN_DIR="$QA_DIR/run"

API_PORT="${QA_API_PORT:-8091}"
WEB_PORT="${QA_WEB_PORT:-5191}"

QA_NAME="${QA_NAME:-QA Tester}"
QA_EMAIL="${QA_EMAIL:-qa@calich.test}"
QA_PASSWORD="${QA_PASSWORD:-qa-password}"

API_URL="http://localhost:$API_PORT"
WEB_URL="http://localhost:$WEB_PORT"

BACKEND_PID="$RUN_DIR/backend.pid"
FRONTEND_PID="$RUN_DIR/frontend.pid"

# --- process helpers ---

is_running() {
	local pidfile="$1"
	[ -f "$pidfile" ] || return 1
	local pid
	pid="$(cat "$pidfile")"
	[ -n "$pid" ] && kill -0 "$pid" 2>/dev/null
}

stop_pidfile() {
	local pidfile="$1" name="$2"
	if is_running "$pidfile"; then
		local pid
		pid="$(cat "$pidfile")"
		echo "stopping $name (pid $pid)"
		kill "$pid" 2>/dev/null || true
		for _ in $(seq 1 20); do
			kill -0 "$pid" 2>/dev/null || break
			sleep 0.25
		done
		kill -9 "$pid" 2>/dev/null || true
	fi
	rm -f "$pidfile"
}

# wait_for_url polls until the URL answers or we give up, so `up` doesn't
# hand back a stack that isn't listening yet.
wait_for_url() {
	local url="$1" name="$2" log="$3"
	for _ in $(seq 1 60); do
		if curl -fsS -o /dev/null "$url" 2>/dev/null; then
			return 0
		fi
		sleep 0.5
	done
	echo "error: $name never became ready at $url" >&2
	echo "--- last 30 lines of $log ---" >&2
	tail -30 "$log" >&2 || true
	return 1
}

# --- steps ---

start_backend() {
	if is_running "$BACKEND_PID"; then
		echo "backend already running on :$API_PORT"
		return
	fi

	# Build first and run the binary directly: `go run` would leave the real
	# server as a grandchild, so the pid we record wouldn't be the one to kill.
	echo "building backend..."
	(cd "$ROOT/server" && go build -o bin/calich-server ./cmd/server)

	echo "starting backend on :$API_PORT (data: $DATA_DIR)"
	DATA_DIR="$DATA_DIR" PORT="$API_PORT" \
		nohup "$ROOT/server/bin/calich-server" >"$LOG_DIR/backend.log" 2>&1 &
	echo $! >"$BACKEND_PID"

	wait_for_url "$API_URL/api/health" "backend" "$LOG_DIR/backend.log"
}

start_frontend() {
	if is_running "$FRONTEND_PID"; then
		echo "frontend already running on :$WEB_PORT"
		return
	fi

	echo "starting frontend on :$WEB_PORT (proxying /api to :$API_PORT)"
	# Run vite's binary rather than `yarn dev` for the same pid reason, and
	# point its /api proxy at the QA backend via API_PROXY_TARGET.
	API_PROXY_TARGET="$API_URL" \
		nohup "$ROOT/node_modules/.bin/vite" --port "$WEB_PORT" --strictPort \
		>"$LOG_DIR/frontend.log" 2>&1 &
	echo $! >"$FRONTEND_PID"

	wait_for_url "$WEB_URL" "frontend" "$LOG_DIR/frontend.log"
}

seed_account() {
	local has_accounts
	has_accounts="$(curl -fsS "$API_URL/api/auth/setup-status" | grep -o '"has_accounts":[a-z]*' | cut -d: -f2)"

	if [ "$has_accounts" = "true" ]; then
		echo "account already seeded: $QA_EMAIL"
		return
	fi

	echo "seeding QA account: $QA_EMAIL"
	curl -fsS -X POST "$API_URL/api/auth/register" \
		-H 'Content-Type: application/json' \
		-d "{\"name\":\"$QA_NAME\",\"email\":\"$QA_EMAIL\",\"password\":\"$QA_PASSWORD\"}" \
		-o /dev/null
}

# --- commands ---

cmd_up() {
	mkdir -p "$DATA_DIR" "$LOG_DIR" "$RUN_DIR"
	start_backend
	seed_account
	start_frontend
	echo
	echo "QA stack is up:"
	echo "  app:      $WEB_URL"
	echo "  api:      $API_URL"
	echo "  login:    $QA_EMAIL / $QA_PASSWORD"
	echo "  logs:     $LOG_DIR"
}

cmd_down() {
	stop_pidfile "$FRONTEND_PID" "frontend"
	stop_pidfile "$BACKEND_PID" "backend"
	echo "QA stack is down (database kept at $DATA_DIR)"
}

cmd_reset() {
	cmd_down
	echo "deleting $DATA_DIR"
	rm -rf "$DATA_DIR"
	cmd_up
}

cmd_status() {
	if is_running "$BACKEND_PID"; then
		echo "backend:  running (pid $(cat "$BACKEND_PID")) on $API_URL"
	else
		echo "backend:  stopped"
	fi
	if is_running "$FRONTEND_PID"; then
		echo "frontend: running (pid $(cat "$FRONTEND_PID")) on $WEB_URL"
	else
		echo "frontend: stopped"
	fi
}

case "${1:-}" in
up) cmd_up ;;
down) cmd_down ;;
reset) cmd_reset ;;
status) cmd_status ;;
*)
	echo "usage: scripts/qa-env.sh {up|down|reset|status}" >&2
	exit 1
	;;
esac
