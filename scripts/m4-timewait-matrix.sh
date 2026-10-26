#!/usr/bin/env bash
# M4 break-it, part 2 (full matrix): MaxIdleConnsPerHost 2 vs 100 vs 200.
#
# Repeats the TIME_WAIT experiment 5 times per pool size, with a FRESH gateway
# container for every run, so the numbers show the spread and not one lucky run.
# Each run sends 20000 requests from 200 concurrent clients to the products
# upstream and prints requests/sec, p99 latency and the TIME_WAIT sockets the
# gateway is left holding.
#
# Needs the lab running (make lab-up). Restores internal/proxy/transport.go and
# the gateway on exit, even on Ctrl+C. Takes a few minutes (it rebuilds the
# gateway image once per pool size).
#
# Run it with:  bash scripts/m4-timewait-matrix.sh

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
  echo "== restoring transport.go and the gateway =="
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

# Download the two helper images now so the pull time doesn't distort the
# first measurement. -q = quiet.
echo "pulling helper images (first run only)..."
docker pull -q williamyeh/hey >/dev/null
docker pull -q nicolaka/netshoot >/dev/null

# One block of 5 runs per pool size: the stdlib default, the shipped value, and
# a pool as large as the number of concurrent clients.
for idle in 2 100 200; do
  # Start from the original source, then set the idle-per-host pool size.
  # (The `[0-9]*` matches whatever number is there now.)
  cp "$BACKUP" "$SRC"
  sed -i "s/MaxIdleConnsPerHost: *[0-9]*,/MaxIdleConnsPerHost:   $idle,/" "$SRC"
  # Rebuild the gateway image once for this pool size.
  $C up -d --build --force-recreate gateway >/dev/null 2>&1
  for run in 1 2 3 4 5; do
    # Recreate the container from the SAME image (no rebuild). A new container
    # is a new network namespace, so TIME_WAIT starts at 0 for every run.
    $C up -d --force-recreate gateway >/dev/null 2>&1
    # Wait up to 30s for the gateway to answer before starting the load.
    for _ in $(seq 30); do
      curl -m 1 -s -o /dev/null localhost:8080/api/products/1 && break
      sleep 1
    done
    # 20000 requests (-n) from 200 concurrent clients (-c) through the gateway
    # to the fast products upstream; keep hey's whole report in $out.
    out=$(docker run --rm --network deploy_default williamyeh/hey \
      -n 20000 -c 200 http://gateway:8080/api/products/1 2>&1)
    # Pull the requests/sec number out of hey's report (second field).
    rps=$(echo "$out" | awk '/Requests\/sec/{print $2}')
    # Pull the 99th percentile latency, in seconds (third field of "99% in X secs").
    p99=$(echo "$out" | awk '/99% in/{print $3}')
    # One line per run; the TIME_WAIT count is read right after the load ends.
    echo "pool=$idle run=$run rps=$rps p99=${p99}s timewait=$(timewait)"
  done
done

echo
echo "Average the rps per pool size, then paste the lines into NOTES.md (M4 experiments)."
