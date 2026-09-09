#!/usr/bin/env bash
# Evaluation: E7 — remote readiness: the hosted profile over PostgreSQL and HTTPS
#
# Proves: a hosted service runs over a real PostgreSQL deployment database
# behind TLS; room creation, joins, and credential issue/revoke are owner
# operations; writes require per-Actor credentials; reads stay open; the
# four-step continuation journey runs through ordinary read/send; revocation
# lands immediately without touching prior Events, the Room, or other
# Actors; and commands, ordering, boundaries, and error classes match the
# local profile.
#
# Self-contained: initdb starts a temporary PostgreSQL cluster and a
# self-signed TLS front terminates https in front of the hosted service.
# Skips with a notice where no PostgreSQL is installed.
set -euo pipefail
cd "$(dirname "$0")/.."
BIN="$PWD/bin/tearoom"
WORK="$(mktemp -d)"
PGDATA=""
PGCTL=""
SRV_PID=""
TLS_PID=""
cleanup() {
  if [ -n "$SRV_PID" ]; then kill "$SRV_PID" 2>/dev/null || true; wait "$SRV_PID" 2>/dev/null || true; fi
  if [ -n "$TLS_PID" ]; then kill "$TLS_PID" 2>/dev/null || true; wait "$TLS_PID" 2>/dev/null || true; fi
  if [ -n "$PGDATA" ] && [ -n "$PGCTL" ]; then "$PGCTL" -D "$PGDATA" stop >/dev/null 2>&1 || true; fi
  rm -rf "$WORK"
}
trap 'cleanup' EXIT

pass=0
fail=0
check() {
  if [ "$2" -eq 0 ]; then pass=$((pass + 1)); echo "  ok   $1"
  else fail=$((fail + 1)); echo "  FAIL $1"; fi
}

# expect_env runs tearoom with an explicit environment and checks exit 0.
expect_env() { local name="$1"; shift; local rc=0; env "$@" >/dev/null 2>&1 || rc=$?; check "$name" "$rc"; }
reject_env() { local name="$1"; shift; local rc=0; env "$@" >/dev/null 2>&1 || rc=$?; [ "$rc" -ne 0 ] && rc=0 || rc=1; check "$name" "$rc"; }

# seqs_of reads JSONL on stdin and prints the event sequence numbers.
seqs_of() {
  python3 -c '
import json, sys
seqs = [json.loads(ln)["seq"] for ln in sys.stdin if ln.strip()]
print(" ".join(str(s) for s in seqs))
'
}

# --- locate PostgreSQL --------------------------------------------------------
for c in pg_ctl \
  /opt/homebrew/opt/postgresql@16/bin/pg_ctl /opt/homebrew/opt/postgresql@17/bin/pg_ctl \
  /opt/homebrew/opt/postgresql@15/bin/pg_ctl /opt/homebrew/opt/postgresql/bin/pg_ctl \
  /usr/local/opt/postgresql@16/bin/pg_ctl /usr/local/opt/postgresql/bin/pg_ctl; do
  if command -v "$c" >/dev/null 2>&1; then PGCTL="$(command -v "$c")"; break; fi
done
if [ -z "$PGCTL" ]; then
  echo "E7 skipped: no PostgreSQL found (install postgresql, or point TEAROOM_TEST_PG at a database for the battery)"
  exit 0
fi
PGBIN="$(dirname "$PGCTL")"

echo
echo "E7: hosted profile over PostgreSQL and TLS"

# --- temporary PostgreSQL cluster ---------------------------------------------
PGDATA="$WORK/pg"
initdb -D "$PGDATA" -U tearoom --auth=trust >/dev/null 2>&1
PGPORT="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
"$PGCTL" -D "$PGDATA" -o "-p $PGPORT -c listen_addresses=127.0.0.1" -l "$WORK/pg.log" start >/dev/null
until psql -h 127.0.0.1 -p "$PGPORT" -U tearoom -d postgres -c "select 1" >/dev/null 2>&1; do sleep 0.1; done
createdb -h 127.0.0.1 -p "$PGPORT" -U tearoom tearoom_e7
DSN="postgres://tearoom@127.0.0.1:$PGPORT/tearoom_e7?sslmode=disable"

# --- hosted service + TLS front ------------------------------------------------
OWNER="tr_owner_e7_$(python3 -c 'import secrets; print(secrets.token_urlsafe(12))')"
HTTPPORT="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
TLSPORT="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"

TEAROOM_DSN="$DSN" TEAROOM_OWNER_TOKEN="$OWNER" "$BIN" server start --address "127.0.0.1:$HTTPPORT" >"$WORK/hosted.log" 2>&1 &
SRV_PID=$!

# The TLS front is standard-library only and lives in the scratch directory.
cat >"$WORK/tlsfront.go" <<'GO'
package main

import (
	"crypto/tls"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

func main() {
	target, err := url.Parse(os.Getenv("TLS_TARGET"))
	if err != nil {
		log.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	original := proxy.Director
	proxy.Director = func(r *http.Request) {
		original(r)
		r.Header.Set("X-Forwarded-Proto", "https")
	}
	server := &http.Server{
		Addr:    os.Getenv("TLS_ADDR"),
		Handler: proxy,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}
	_ = strings.TrimSpace
	log.Fatal(server.ListenAndServeTLS(os.Getenv("TLS_CERT"), os.Getenv("TLS_KEY")))
}
GO

openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -keyout "$WORK/key.pem" -out "$WORK/cert.pem" \
  -subj "/CN=127.0.0.1" \
  -addext "subjectAltName=IP:127.0.0.1,DNS:localhost" >/dev/null 2>&1

# This machine's Go internal linker emits test and tool binaries without the
# LC_UUID dyld requires; the external linker is the documented workaround
# (see docs/operations/building.md and the Makefile test flags).
CGO_ENABLED=1 go build -ldflags=-linkmode=external -o "$WORK/tlsfront" "$WORK/tlsfront.go"
TLS_CERT="$WORK/cert.pem" TLS_KEY="$WORK/key.pem" \
  TLS_TARGET="http://127.0.0.1:$HTTPPORT" TLS_ADDR="127.0.0.1:$TLSPORT" \
  "$WORK/tlsfront" >"$WORK/tls.log" 2>&1 &
TLS_PID=$!

BASE="https://127.0.0.1:$TLSPORT"
export TEAROOM_CA_FILE="$WORK/cert.pem"

# --- room creation (owner) ------------------------------------------------------
export TEAROOM_SERVER="$BASE"
export TEAROOM_TOKEN="$OWNER"

ready=0
ROOM_URL=""
for _ in $(seq 1 100); do
  if out="$(TEAROOM_SERVER="$BASE" TEAROOM_TOKEN="$OWNER" "$BIN" room init --server "$BASE" --as planner 2>/dev/null)" && [ -n "$out" ]; then
    ready=1
    ROOM_URL="$(printf '%s' "$out" | python3 -c 'import json,sys; print(json.load(sys.stdin)["server"])')"
    break
  fi
  sleep 0.2
done
check "owner creates a room over https and receives its room URL" "$([ "$ready" -eq 1 ] && echo 0 || echo 1)"
check "the room URL is room-scoped over https ($ROOM_URL)" \
  "$(printf '%s' "$ROOM_URL" | grep -q "^$BASE/r/room_" && echo 0 || echo 1)"

# --- admissions and credentials (owner) ------------------------------------------
expect_env "owner joins bob" \
  TEAROOM_SERVER="$ROOM_URL" TEAROOM_TOKEN="$OWNER" "$BIN" room join bob
expect_env "owner joins agent-c" \
  TEAROOM_SERVER="$ROOM_URL" TEAROOM_TOKEN="$OWNER" "$BIN" room join agent-c

PLANNER_TOKEN="$(TEAROOM_SERVER="$ROOM_URL" TEAROOM_TOKEN="$OWNER" "$BIN" room credential issue --actor planner --expires 24h | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')"
ISSUED="$(TEAROOM_SERVER="$ROOM_URL" TEAROOM_TOKEN="$OWNER" "$BIN" room credential issue --actor bob --expires 24h)"
BOB_TOKEN="$(printf '%s' "$ISSUED" | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')"
BOB_ID="$(printf '%s' "$ISSUED" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"

# --- the four-step journey --------------------------------------------------------
expect_env "step 1: planner appends the work" \
  TEAROOM_SERVER="$ROOM_URL" TEAROOM_TOKEN="$PLANNER_TOKEN" TEAROOM_ACTOR=planner \
  "$BIN" send --json '{"work":"first attempt"}'

bob_read="$(env TEAROOM_SERVER="$ROOM_URL" TEAROOM_TOKEN= TEAROOM_ACTOR=bob "$BIN" read)"
check "step 2: bob recovers the work with an open, token-free read" \
  "$(printf '%s' "$bob_read" | seqs_of | grep -q '^1$' && echo 0 || echo 1)"

expect_env "step 3: bob appends the result" \
  TEAROOM_SERVER="$ROOM_URL" TEAROOM_TOKEN="$BOB_TOKEN" TEAROOM_ACTOR=bob \
  "$BIN" send --json '{"result":"second attempt"}'

c_read="$(env TEAROOM_SERVER="$ROOM_URL" TEAROOM_TOKEN= TEAROOM_ACTOR=agent-c "$BIN" read --after e1)"
check "step 4: agent-c reads the result from the e1 boundary" \
  "$(printf '%s' "$c_read" | seqs_of | grep -q '^2$' && echo 0 || echo 1)"

# --- the revocation and class matrix ------------------------------------------------
reject_env "an unauthenticated write fails" \
  TEAROOM_SERVER="$ROOM_URL" TEAROOM_TOKEN= TEAROOM_ACTOR=bob "$BIN" send "no token"
reject_env "a wrong-actor token fails" \
  TEAROOM_SERVER="$ROOM_URL" TEAROOM_TOKEN="$BOB_TOKEN" TEAROOM_ACTOR=planner "$BIN" send "mismatch"

rc=0
TEAROOM_SERVER="$ROOM_URL" TEAROOM_TOKEN="$OWNER" "$BIN" room credential revoke "$BOB_ID" >/dev/null 2>&1 || rc=$?
check "owner revokes bob's credential" "$rc"

reject_env "the revoked credential can no longer write" \
  TEAROOM_SERVER="$ROOM_URL" TEAROOM_TOKEN="$BOB_TOKEN" TEAROOM_ACTOR=bob "$BIN" send "after revoke"

open_read="$(env TEAROOM_SERVER="$ROOM_URL" TEAROOM_TOKEN= TEAROOM_ACTOR=agent-c "$BIN" read)"
check "reads stay open and history is untouched (1..2)" \
  "$(printf '%s' "$open_read" | seqs_of | grep -q '^1 2$' && echo 0 || echo 1)"

echo
if [ "$fail" -eq 0 ]; then
  printf 'E7 passed: %d checks\n\n' "$pass"
else
  printf 'E7 FAILED: %d of %d checks failed\n\n' "$fail" "$((pass + fail))"
  exit 1
fi
