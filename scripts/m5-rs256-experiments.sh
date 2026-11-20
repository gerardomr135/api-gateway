#!/usr/bin/env bash
# M5 step 6 experiments: the gateway verifying RS256 tokens against a real
# authsvc's JWKS, as separate processes, over real HTTP.
#
# Starts authsvc, toysvc and the gateway (RS256, jwks_refresh 3s), then checks:
#   - a token minted by authsvc is accepted, and the upstream sees X-User-ID
#   - key rotation: the new kid is picked up, but only after jwks_refresh
#     (a valid token is refused inside that window: the documented trade-off)
#   - a retired key keeps working for tokens already issued
#   - drop_old: once the gateway refreshes, tokens of the dropped key fail
#   - an HS256 token and an unknown kid are refused
#   - authsvc going down: cached keys keep working, the gateway keeps serving
#   - the gateway refuses to start when the JWKS cannot be fetched
#
# Usage: scripts/m5-rs256-experiments.sh      (needs go and curl)
set -u
cd "$(dirname "$0")/.."

for tool in curl go; do
  command -v "$tool" >/dev/null || { echo "missing tool: $tool" >&2; exit 2; }
done

GW=http://localhost:18081
AUTH=http://localhost:9190
REFRESH=3   # seconds; must match jwks_refresh below

WORK=$(mktemp -d)
PIDS=()
PASS=0
FAIL=0
cleanup() {
  for pid in "${PIDS[@]:-}"; do [ -n "$pid" ] && kill "$pid" 2>/dev/null; done
  wait 2>/dev/null
  rm -rf "$WORK"
}
trap cleanup EXIT

check() { # check <name> <want-status> <curl args...>   (body in $WORK/body)
  local name=$1 want=$2; shift 2
  local got
  got=$(curl -s -o "$WORK/body" -w '%{http_code}' "$@")
  if [ "$got" = "$want" ]; then
    PASS=$((PASS + 1)); printf '  ok    %-58s %s\n' "$name" "$got"
  else
    FAIL=$((FAIL + 1)); printf '  FAIL  %-58s got %s, want %s\n' "$name" "$got" "$want"
  fi
}
expect() { # expect <name> <grep -i pattern> [present|absent]  (last body)
  local name=$1 pat=$2 mode=${3:-present} found=no
  grep -qiE -- "$pat" "$WORK/body" && found=yes
  if { [ "$mode" = present ] && [ "$found" = yes ]; } || { [ "$mode" = absent ] && [ "$found" = no ]; }; then
    PASS=$((PASS + 1)); printf '  ok    %-58s (%s: %s)\n' "$name" "$mode" "$pat"
  else
    FAIL=$((FAIL + 1)); printf '  FAIL  %-58s (%s: %s)\n' "$name" "$mode" "$pat"
  fi
}

# token [sub] -> a fresh RS256 token from authsvc (signed with its CURRENT key)
token() {
  curl -s -X POST "$AUTH/token?sub=${1:-alice}&roles=admin,dev" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p'
}
bearer() { printf 'Authorization: Bearer %s' "$1"; }

echo "building..."
go build -o "$WORK/gateway" ./cmd/gateway || exit 1
go build -o "$WORK/toysvc" ./cmd/toysvc   || exit 1
go build -o "$WORK/authsvc" ./cmd/authsvc || exit 1

sed -e 's#http://users:9001#http://localhost:9201#' \
    -e 's#http://products:9003#http://localhost:9203#' \
    -e 's#listen: ":8080"#listen: ":18081"#' \
    -e 's#admin_listen: ":9090"#admin_listen: ":19091"#' \
    -e "s#^  jwks_url:.*#  jwks_url: $AUTH/.well-known/jwks.json#" \
    -e "s#^  jwks_refresh:.*#  jwks_refresh: ${REFRESH}s#" \
    configs/routes.yaml > "$WORK/routes.yaml"
grep -q '^  algorithm: RS256' "$WORK/routes.yaml" || { echo "configs/routes.yaml is not RS256" >&2; exit 1; }

"$WORK/authsvc" -port 9190 -retire-after 60s >"$WORK/authsvc.log" 2>&1 & AUTH_PID=$!; PIDS+=($AUTH_PID)
"$WORK/toysvc" -name users -port 9201 >/dev/null 2>&1 & PIDS+=($!)
for _ in $(seq 1 50); do curl -s -o /dev/null "$AUTH/.well-known/jwks.json" && break; sleep 0.1; done

"$WORK/gateway" -config "$WORK/routes.yaml" >"$WORK/gateway.log" 2>&1 & PIDS+=($!)
for _ in $(seq 1 50); do curl -s -o /dev/null "$GW/api/products/x" && break; sleep 0.1; done
curl -s -o /dev/null "$GW/api/products/x" || { echo "gateway did not come up; log:" >&2; cat "$WORK/gateway.log" >&2; exit 1; }

echo
echo "1. a token from authsvc"
T1=$(token alice)
check "no token"                         401 "$GW/api/users/1"
check "valid RS256 token"                200 -H "$(bearer "$T1")" "$GW/api/users/1"
expect "upstream sees X-User-ID alice"   '"?X-User-Id"?[^a-z]*alice'
expect "upstream sees roles admin,dev"   'admin,dev'
check "garbage token"                    401 -H "$(bearer not.a.jwt)" "$GW/api/users/1"

echo
echo "2. wrong algorithm"
# header {"alg":"HS256","kid":"x"} . payload {"sub":"alice"} . fake signature
HS_TOKEN="eyJhbGciOiJIUzI1NiIsImtpZCI6IngifQ.eyJzdWIiOiJhbGljZSJ9.c2ln"
check "HS256 token is refused"           401 -H "$(bearer "$HS_TOKEN")" "$GW/api/users/1"

echo
echo "3. rotation (jwks_refresh=${REFRESH}s)"
curl -s -X POST "$AUTH/rotate" >/dev/null
T2=$(token bob)
check "new kid inside the refresh window is refused"     401 -H "$(bearer "$T2")" "$GW/api/users/1"
check "old token still works (key still cached)"         200 -H "$(bearer "$T1")" "$GW/api/users/1"
sleep $((REFRESH + 1))
check "new kid accepted after the window"                200 -H "$(bearer "$T2")" "$GW/api/users/1"
check "old token still works (key retired, still published)" 200 -H "$(bearer "$T1")" "$GW/api/users/1"

echo
echo "4. drop_old rotation"
curl -s -X POST "$AUTH/rotate?drop_old=true" >/dev/null
T3=$(token carol)
sleep $((REFRESH + 1))
check "token of the newest key triggers a refresh"       200 -H "$(bearer "$T3")" "$GW/api/users/1"
check "tokens of the dropped keys now fail"              401 -H "$(bearer "$T1")" "$GW/api/users/1"
check "(second dropped key as well)"                     401 -H "$(bearer "$T2")" "$GW/api/users/1"

echo
echo "5. authsvc goes down"
kill "$AUTH_PID" 2>/dev/null; wait "$AUTH_PID" 2>/dev/null
check "cached key still verifies"                        200 -H "$(bearer "$T3")" "$GW/api/users/1"
check "no token is still 401, gateway still serving"     401 "$GW/api/users/1"

echo
echo "6. gateway startup with the identity provider unreachable"
out=$(timeout 10 "$WORK/gateway" -config "$WORK/routes.yaml" 2>&1); rc=$?
printf '%s' "$out" > "$WORK/body"
if [ "$rc" -ne 0 ] && [ "$rc" -ne 124 ]; then
  PASS=$((PASS + 1)); printf '  ok    %-58s exit %s\n' "refuses to start" "$rc"
else
  FAIL=$((FAIL + 1)); printf '  FAIL  %-58s exit %s\n' "refuses to start" "$rc"
fi
expect "error mentions the initial JWKS fetch"           'initial JWKS fetch'

echo
echo "passed: $PASS  failed: $FAIL"
[ "$FAIL" -eq 0 ]
