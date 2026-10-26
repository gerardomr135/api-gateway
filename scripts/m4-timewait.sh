#!/usr/bin/env bash
# M4 break-it, part 2: MaxIdleConnsPerHost 2 (stdlib default) vs 100.
#
# Sends the same load through the gateway to the products upstream twice, once
# with the idle pool capped at 2 connections per host and once at 100, and
# counts the TIME_WAIT sockets the gateway leaves behind plus the throughput.
#
# With a pool of 2, 200 concurrent requests can only reuse 2 connections; the
# rest dial a fresh TCP connection and close it afterwards, and every closed
# connection sits in TIME_WAIT on the side that closed first (the gateway).
#
# Needs the lab running (make lab-up). Restores internal/proxy/transport.go and
# the lab containers on exit, even on Ctrl+C.
#
# Run it with:  bash scripts/m4-timewait.sh

# Treat unset variables as errors. No `set -e`: the script must keep going to
# its cleanup even if one measurement fails.
set -u

# Work from the repo root no matter where the script is launched from.
cd "$(dirname "$0")/.."

# Shorthand for the lab's compose file (same one `make lab-up` uses).
C="docker compose -f deploy/docker-compose.yaml"
# The source file this script edits, and a temp file to back it up in.
SRC=internal/proxy/transport.go
BACKUP=$(mktemp)
# Save the real source now; cleanup() copies it back at the end.
cp "$SRC" "$BACKUP"

# Runs on any exit (finish, error or Ctrl+C) so the repo is never left edited.
cleanup() {
  echo
  echo "== restoring transport.go and the lab =="
  # Put the original source back (a copy, so uncommitted edits survive).
  cp "$BACKUP" "$SRC"
  # Rebuild and recreate the gateway from the restored source.
  $C up -d --build --force-recreate gateway >/dev/null 2>&1
  # Delete the temp file.
  rm -f "$BACKUP"
}
# Register cleanup to run when the script exits for any reason.
trap cleanup EXIT

# Prints how many sockets the gateway has in TIME_WAIT towards the products
# upstream (port 9003).
timewait() {
  # Start a throwaway netshoot container inside the gateway container's
  # network namespace (the gateway image has no shell or ss of its own).
  # `ss -tan state time-wait '( dport = :9003 )'` lists closed connections
  # waiting out the 60s TIME_WAIT period; `tail -n +2` drops the header line
  # and `wc -l` counts the rest.
  docker run --rm --net "container:$($C ps -q gateway)" nicolaka/netshoot \
    sh -c "ss -tan state time-wait '( dport = :9003 )' | tail -n +2 | wc -l"
}

# Waits up to 30s for the freshly recreated gateway to answer requests.
wait_gateway() {
  # Try once per second, 30 times.
  for _ in $(seq 1 30); do
    # Success (curl exit 0, any HTTP answer) means the gateway is up.
    curl -m 1 -s -o /dev/null localhost:8080/api/products/1 && return 0
    sleep 1
  done
  # Never came up: show why and fail.
  echo "gateway did not come up; last logs:"; $C logs --tail=10 gateway
  return 1
}

# usage: run_case <title> <MaxIdleConnsPerHost value>
run_case() {
  local title=$1 idle=$2
  echo
  echo "=================================================="
  echo "== $title"
  echo "=================================================="
  # Start from the original source, then set the idle-per-host pool size.
  # (The `[0-9]*` matches whatever number is there now.)
  cp "$BACKUP" "$SRC"
  sed -i "s/MaxIdleConnsPerHost: *[0-9]*,/MaxIdleConnsPerHost:   $idle,/" "$SRC"
  # Show the line actually in effect, so the run can't silently test the wrong value.
  echo "in effect: $(grep -n 'MaxIdleConnsPerHost:' "$SRC" | head -1)"
  # Rebuild the gateway image with that source and recreate the container. A
  # new container is a new network namespace, so TIME_WAIT starts at zero.
  $C up -d --build --force-recreate gateway >/dev/null 2>&1
  # Don't start the load until the gateway is ready; abort if it never is.
  wait_gateway || exit 1
  echo "before load: TIME_WAIT -> $(timewait)"
  # 20000 requests (-n) from 200 concurrent clients (-c) through the gateway to
  # the fast products upstream; keep only hey's headline numbers.
  docker run --rm --network deploy_default williamyeh/hey \
    -n 20000 -c 200 http://gateway:8080/api/products/1 2>&1 \
    | grep -E 'Requests/sec|Average:|Slowest:|\[200\]|Error|error'
  # The number that matters: sockets left in TIME_WAIT by the gateway.
  echo "after load:  TIME_WAIT -> $(timewait)"
}

# Download the two helper images now so the pull time doesn't distort the
# first measurement. -q = quiet.
echo "pulling helper images (first run only)..."
docker pull -q williamyeh/hey >/dev/null
docker pull -q nicolaka/netshoot >/dev/null

# Case 1: the stdlib default of 2 idle connections per host.
run_case "1. MaxIdleConnsPerHost = 2 (stdlib default)" 2
# Case 2: the tuned value the gateway actually ships with.
run_case "2. MaxIdleConnsPerHost = 100 (tuned)" 100

echo
echo "Compare TIME_WAIT counts and Requests/sec, then paste the output into NOTES.md (M4 experiments)."
