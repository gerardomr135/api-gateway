#!/usr/bin/env bash
# M4 break-it: why timeouts matter.
#
# Points a very slow orders upstream (60s latency) at a gateway limited to 256
# file descriptors, and sends it 50 requests/second for 30s, twice:
#   1. route/Transport timeouts effectively off  -> the gateway starves
#   2. route timeout 1s                          -> damage is bounded
# During each run it samples the gateway's open upstream connections and a
# request to a HEALTHY route (products).
#
# Needs the lab running (make lab-up). Restores configs/routes.yaml and the
# lab containers on exit, even on Ctrl+C.
#
# The load is OPEN-LOOP (vegeta, fixed requests/second): clients keep arriving
# whether or not earlier requests finished, like real traffic. A closed-loop
# generator (hey -c N) always keeps N requests in flight, which no timeout can
# reduce, so it can't show what a timeout buys you.
#
# Run it with:  bash scripts/m4-experiments.sh

# Treat unset variables as errors. No `set -e`: some checks (curl to a
# starved gateway) are SUPPOSED to fail and the script must keep going.
set -u

# Work from the repo root no matter where the script is launched from.
# $0 is this script; dirname gives scripts/, and .. is the repo root.
cd "$(dirname "$0")/.."

# Shorthand for the lab's compose file (same one `make lab-up` uses).
C="docker compose -f deploy/docker-compose.yaml"
# Temp file that will hold the compose override (slow orders + fd limit).
OVERRIDE=$(mktemp --suffix=.yaml)
# The gateway config this script edits, and a temp file to back it up in.
ROUTES=configs/routes.yaml
BACKUP=$(mktemp)
# Name of the throwaway load-generator container, so it can be killed by name.
LOAD_NAME=m4-load
# Save the real config now; cleanup() copies it back at the end.
cp "$ROUTES" "$BACKUP"

# Compose override file, merged on top of the lab's compose file:
#  - all three orders instances sleep 60s before answering (-latency=60s)
#  - the gateway container gets a 256 open-files limit (a tiny `ulimit -n`),
#    so descriptor exhaustion shows up within seconds instead of hours
cat > "$OVERRIDE" <<'E'
services:
  orders-1: { command: ["-name=orders-1","-port=9002","-latency=60s"] }
  orders-2: { command: ["-name=orders-2","-port=9002","-latency=60s"] }
  orders-3: { command: ["-name=orders-3","-port=9002","-latency=60s"] }
  gateway:  { ulimits: { nofile: { soft: 256, hard: 256 } } }
E
# Same compose command, now with the override layered on top.
L="$C -f $OVERRIDE"

# Runs on any exit (finish, error or Ctrl+C) so the lab is never left broken.
cleanup() {
  echo
  echo "== restoring routes.yaml and the lab =="
  # Stop the load generator if it is still running; ignore "no such container".
  docker rm -f "$LOAD_NAME" >/dev/null 2>&1
  # Put the original config back (a copy, not `git checkout`, so your
  # uncommitted edits to routes.yaml survive).
  cp "$BACKUP" "$ROUTES"
  # Recreate every container from the normal compose file: latency back to 0,
  # no fd limit, original config.
  $C up -d --force-recreate >/dev/null 2>&1
  # Delete the temp files.
  rm -f "$OVERRIDE" "$BACKUP"
}
# Register cleanup to run when the script exits for any reason.
trap cleanup EXIT

# Prints how many established TCP connections the gateway has open to the
# orders instances (port 9002). This is the "requests stuck waiting" count.
conns() {
  # Start a throwaway netshoot container inside the gateway container's
  # network namespace (the gateway image has no shell or ss of its own).
  # `ss -tan state established '( dport = :9002 )'` lists connections whose
  # destination port is 9002; `tail -n +2` drops the header line and
  # `wc -l` counts what's left.
  docker run --rm --net "container:$($C ps -q gateway)" nicolaka/netshoot \
    sh -c "ss -tan state established '( dport = :9002 )' | tail -n +2 | wc -l"
}

# Prints status and latency of a request to the HEALTHY products route.
# curl gives up after 3s (-m 3); -s silences progress, -o /dev/null drops the
# body, -w prints the status code and total time. A code of 000 means the
# gateway never answered at all.
products() {
  curl -m 3 -s -o /dev/null -w '%{http_code} in %{time_total}s' localhost:8080/api/products/1
}

# Waits up to 30s for the freshly recreated gateway to answer requests.
wait_gateway() {
  # Try once per second, 30 times.
  for _ in $(seq 1 30); do
    # Success (curl exit 0, any HTTP answer) means the gateway is up.
    curl -m 1 -s -o /dev/null localhost:8080/api/products/1 && return 0
    sleep 1
  done
  # Never came up (e.g. config validation error): show why and fail.
  echo "gateway did not come up; last logs:"; $C logs --tail=10 gateway
  return 1
}

# usage: phase <title> <vegeta client timeout, e.g. 0 or 30s>
# Runs one load test and prints the samples.
phase() {
  # First argument is the title, second is vegeta's own per-request timeout.
  local title=$1 client_timeout=$2
  echo
  echo "=================================================="
  echo "== $title"
  echo "=================================================="
  # (Re)create the slow orders instances and the gateway so they pick up the
  # override and the current routes.yaml. Output hidden for readability.
  $L up -d --force-recreate orders-1 orders-2 orders-3 gateway >/dev/null 2>&1
  # Don't start the load until the gateway is ready; abort if it never is.
  wait_gateway || exit 1
  # Baseline reading with no load: products should be 200, conns should be 0.
  echo "before load: products -> $(products), orders conns -> $(conns)"

  # Start the load generator in the BACKGROUND (-d) on the lab network. The
  # shell pipeline inside the container is: one GET line -> `vegeta attack`
  # at a fixed 50 requests/second for 30s (-rate/-duration) -> `vegeta report`
  # (prints status codes at the end). -timeout is vegeta's own give-up time;
  # 0 means never, so only the gateway can end a request.
  docker run -d --name "$LOAD_NAME" --network deploy_default --entrypoint sh peterevans/vegeta \
    -c "echo 'GET http://gateway:8080/api/orders/1' | vegeta attack -rate 50 -duration 30s -timeout $client_timeout | vegeta report" >/dev/null
  # Sample at 8s and 18s into the load.
  for t in 8 18; do
    # Sleep only the time since the previous sample (prev is empty on the
    # first pass, so it defaults to 0).
    sleep $((t - ${prev:-0})); prev=$t
    # The two numbers that matter: connections held open to orders, and
    # whether the unrelated healthy route still answers.
    echo "t=${t}s: orders conns -> $(conns), healthy products route -> $(products)"
  done
  # Reset the sampling clock for the next phase.
  unset prev
  # Count gateway log lines showing it ran out of file descriptors.
  echo "gateway 'too many open files' log lines: $($C logs gateway 2>&1 | grep -ci 'too many open files')"
  # Only phase 2 waits for vegeta to finish and prints its report; in phase 1
  # it would wait the full 60s upstream latency, so it is just killed.
  if [ "${WAIT_LOAD:-0}" = 1 ]; then
    # Block until the load generator exits.
    docker wait "$LOAD_NAME" >/dev/null
    # Print vegeta's report (success ratio, latencies, status code counts).
    docker logs "$LOAD_NAME" 2>&1
  fi
  # Remove the load generator (kills it if it is still running).
  docker rm -f "$LOAD_NAME" >/dev/null 2>&1
}

# Download the two helper images now so the pull time doesn't distort the
# first measurement. -q = quiet.
echo "pulling helper images (first run only)..."
docker pull -q peterevans/vegeta >/dev/null
docker pull -q nicolaka/netshoot >/dev/null

# Phase 1: timeouts effectively off.
# Raise the orders route timeout from 3s to 1h: the sed range runs from the
# line "name: orders" to its first "timeout:" line and edits only that one.
sed -i '/name: orders/,/timeout:/ s/timeout: 3s/timeout: 1h/' "$ROUTES"
# write_timeout must stay above the route timeout or config validation refuses
# to start; response_header_timeout is raised so the Transport doesn't cut in.
sed -i 's/write_timeout: .*/write_timeout: 2h/; s/response_header_timeout: .*/response_header_timeout: 2h/' "$ROUTES"
# Vegeta timeout 0 = the client never gives up, so nothing but the gateway can end a request.
phase "1. NO effective timeouts (route 1h): expect conns to pile up and products to fail" 0

# Phase 2: the normal setup.
# Start again from the original config (undoes the phase 1 edits)...
cp "$BACKUP" "$ROUTES"
# ...and set the orders route timeout to 1s whatever its current value is.
sed -i '/name: orders/,/timeout:/ s/timeout: [0-9a-z]*/timeout: 1s/' "$ROUTES"
# WAIT_LOAD=1 (set for this call only) makes the phase print vegeta's report.
WAIT_LOAD=1 phase "2. route timeout 1s: expect bounded conns, products still 200, orders 504" 30s

echo
echo "Compare the two phases, then paste the output into NOTES.md (M4 experiments)."
