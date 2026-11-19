#!/usr/bin/env bash
# M5 break-it against REAL processes (the Go tests bypass config.Load).
#
# Starts two toysvc instances (users :9001, products :9003) and the gateway on
# :18080 with a temp copy of configs/routes.yaml pointed at localhost, then
# checks the auth behaviour over real HTTP:
#
#   - no token / wrong key / expired / alg none / no exp  -> 401
#   - forged X-User-* header, with and without a token    -> neutralised
#   - gateway startup with the secret unset or too short  -> refuses to start,
#     and never prints the secret
#
# Tokens are signed with openssl (HS256), so nothing here shares code with the
# gateway's own verifier.
#
# Run it with:  bash scripts/m5-experiments.sh

set -u
cd "$(dirname "$0")/.."

for tool in openssl curl go; do
  command -v "$tool" >/dev/null || { echo "missing tool: $tool" >&2; exit 2; }
done

SECRET="m5-experiment-secret-at-least-32-bytes-long"
OTHER_SECRET="a-different-secret-also-32-bytes-or-more!!"
ISS="http://auth.local"
AUD="api-gateway"
GW=http://localhost:18080

WORK=$(mktemp -d)
PIDS=()
PASS=0
FAIL=0

cleanup() {
  for pid in "${PIDS[@]:-}"; do
    [ -n "$pid" ] && kill "$pid" 2>/dev/null
  done
  wait 2>/dev/null
  rm -rf "$WORK"
}
trap cleanup EXIT

# --- token helpers -----------------------------------------------------------

b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }

# mint <secret> <payload-json> -> HS256 JWT
mint() {
  local h p sig
  h=$(printf '{"alg":"HS256","typ":"JWT"}' | b64url)
  p=$(printf '%s' "$2" | b64url)
  sig=$(printf '%s.%s' "$h" "$p" | openssl dgst -sha256 -hmac "$1" -binary | b64url)
  printf '%s.%s.%s' "$h" "$p" "$sig"
}

now=$(date +%s)
claims() { # claims <exp-or-empty>
  local exp=""
  [ -n "$1" ] && exp=",\"exp\":$1"
  printf '{"sub":"alice","iss":"%s","aud":"%s","roles":["admin","dev"]%s}' "$ISS" "$AUD" "$exp"
}

TOK_GOOD=$(mint "$SECRET" "$(claims $((now + 3600)))")
TOK_WRONG_KEY=$(mint "$OTHER_SECRET" "$(claims $((now + 3600)))")
TOK_EXPIRED=$(mint "$SECRET" "$(claims $((now - 3600)))")
TOK_NO_EXP=$(mint "$SECRET" "$(claims "")")
# alg none: header.payload. with an empty signature
TOK_NONE="$(printf '{"alg":"none","typ":"JWT"}' | b64url).$(claims $((now + 3600)) | b64url)."

# --- assertions --------------------------------------------------------------

# check <name> <expected-status> <curl args...>   (body left in $WORK/body, headers in $WORK/hdr)
check() {
  local name=$1 want=$2; shift 2
  local got
  got=$(curl -s -o "$WORK/body" -D "$WORK/hdr" -w '%{http_code}' "$@")
  if [ "$got" = "$want" ]; then
    PASS=$((PASS + 1)); printf '  ok    %-52s %s\n' "$name" "$got"
  else
    FAIL=$((FAIL + 1)); printf '  FAIL  %-52s got %s, want %s\n' "$name" "$got" "$want"
  fi
}

# expect <name> <grep -i pattern> [present|absent]: look in the last response body
expect() {
  local name=$1 pat=$2 mode=${3:-present}
  if grep -qiE -- "$pat" "$WORK/body"; then found=yes; else found=no; fi
  if { [ "$mode" = present ] && [ "$found" = yes ]; } || { [ "$mode" = absent ] && [ "$found" = no ]; }; then
    PASS=$((PASS + 1)); printf '  ok    %-52s (%s: %s)\n' "$name" "$mode" "$pat"
  else
    FAIL=$((FAIL + 1)); printf '  FAIL  %-52s (%s: %s)\n        body: %s\n' "$name" "$mode" "$pat" "$(head -c 400 "$WORK/body")"
  fi
}

expect_header() { # expect_header <name> <pattern>
  if grep -qiE -- "$2" "$WORK/hdr"; then
    PASS=$((PASS + 1)); printf '  ok    %-52s (%s)\n' "$1" "$2"
  else
    FAIL=$((FAIL + 1)); printf '  FAIL  %-52s (header missing: %s)\n' "$1" "$2"
  fi
}

# --- build and start ---------------------------------------------------------

echo "building..."
go build -o "$WORK/gateway" ./cmd/gateway || exit 1
go build -o "$WORK/toysvc" ./cmd/toysvc || exit 1

# Temp config: same routes, but the instances this script starts, on a spare port.
# The orders instances are never contacted, so their docker hostnames stay.
sed -e 's#http://users:9001#http://localhost:9001#' \
    -e 's#http://products:9003#http://localhost:9003#' \
    -e 's#listen: ":8080"#listen: ":18080"#' \
    -e 's#admin_listen: ":9090"#admin_listen: ":19090"#' \
    configs/routes.yaml > "$WORK/routes.yaml"

"$WORK/toysvc" -name users -port 9001 >/dev/null 2>&1 & PIDS+=($!)
"$WORK/toysvc" -name products -port 9003 >/dev/null 2>&1 & PIDS+=($!)

GATEWAY_JWT_SECRET="$SECRET" "$WORK/gateway" -config "$WORK/routes.yaml" >"$WORK/gateway.log" 2>&1 & PIDS+=($!)

for _ in $(seq 1 50); do
  curl -s -o /dev/null "$GW/api/products/x" && break
  sleep 0.1
done
if ! curl -s -o /dev/null "$GW/api/products/x"; then
  echo "gateway did not come up; log:" >&2; cat "$WORK/gateway.log" >&2; exit 1
fi

# --- 1. required route: /api/users -------------------------------------------

echo
echo "1. required route (/api/users)"
check "no token"                      401 "$GW/api/users/1"
expect_header "401 carries WWW-Authenticate: Bearer" '^www-authenticate: *bearer'
check "wrong-key token"               401 -H "Authorization: Bearer $TOK_WRONG_KEY" "$GW/api/users/1"
expect_header "wrong key: error=invalid_token" 'invalid_token'
check "expired token"                 401 -H "Authorization: Bearer $TOK_EXPIRED" "$GW/api/users/1"
check "alg none token"                401 -H "Authorization: Bearer $TOK_NONE" "$GW/api/users/1"
check "token with no exp"             401 -H "Authorization: Bearer $TOK_NO_EXP" "$GW/api/users/1"
check "garbage token"                 401 -H "Authorization: Bearer not.a.jwt" "$GW/api/users/1"
check "Basic scheme"                  401 -H "Authorization: Basic YWxpY2U6cHc=" "$GW/api/users/1"
check "valid token"                   200 -H "Authorization: Bearer $TOK_GOOD" "$GW/api/users/1"
expect "upstream sees X-User-ID alice"   '"?X-User-Id"?[^a-z]*alice'
expect "upstream sees roles admin,dev"   'admin,dev'

# --- 2. forged identity headers ----------------------------------------------

echo
echo "2. forged X-User-* headers"
check "forged headers, no token"      401 -H "X-User-ID: mallory" -H "X-User-Roles: superuser" "$GW/api/users/1"
check "forged headers + valid token"  200 -H "X-User-ID: mallory" -H "X-User-Roles: superuser" \
      -H "Authorization: Bearer $TOK_GOOD" "$GW/api/users/1"
expect "forged ID is gone"            'mallory' absent
expect "forged role is gone"          'superuser' absent
expect "identity comes from the token" '"?X-User-Id"?[^a-z]*alice'

# --- 3. auth: none route -----------------------------------------------------

echo
echo "3. auth: none route (/api/products)"
check "no token, forged headers"      200 -H "X-User-ID: mallory" -H "X-User-Roles: superuser" "$GW/api/products/1"
expect "upstream sees no forged ID"   'mallory' absent
expect "upstream sees no forged role" 'superuser' absent
check "garbage token is ignored"      200 -H "Authorization: Bearer garbage" "$GW/api/products/1"

# --- 4. startup failures -----------------------------------------------------

echo
echo "4. gateway startup"
start_fail() { # start_fail <name> <secret-or-UNSET>
  local out rc
  if [ "$2" = UNSET ]; then
    out=$(env -u GATEWAY_JWT_SECRET timeout 5 "$WORK/gateway" -config "$WORK/routes.yaml" 2>&1); rc=$?
  else
    out=$(GATEWAY_JWT_SECRET="$2" timeout 5 "$WORK/gateway" -config "$WORK/routes.yaml" 2>&1); rc=$?
  fi
  printf '%s' "$out" > "$WORK/body"
  if [ "$rc" -ne 0 ] && [ "$rc" -ne 124 ]; then
    PASS=$((PASS + 1)); printf '  ok    %-52s exit %s\n' "$1" "$rc"
  else
    FAIL=$((FAIL + 1)); printf '  FAIL  %-52s exit %s (should refuse to start)\n' "$1" "$rc"
  fi
}
start_fail "secret env var unset"      UNSET
expect "error names the variable"      'GATEWAY_JWT_SECRET'
start_fail "secret shorter than 32 bytes" "tooshort"
expect "short secret is NOT echoed"    'tooshort' absent
expect "error says how many bytes"     '8 bytes'

# --- summary -----------------------------------------------------------------

echo
echo "passed: $PASS  failed: $FAIL"
[ "$FAIL" -eq 0 ]
