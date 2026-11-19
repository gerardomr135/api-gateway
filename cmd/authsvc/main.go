// Command authsvc is a toy identity provider for the gateway lab (M5).
//
// It exists so the gateway's RS256 + JWKS path has something real to talk to:
//
//	GET  /.well-known/jwks.json  public keys (current + recently retired), as a JWKS
//	POST /token?sub=alice&roles=admin,dev[&ttl=10m]
//	                             mint an RS256 token signed with the CURRENT key
//	POST /rotate                 make a new current key; the old one stays in the
//	                             JWKS for -retire-after so live tokens keep working
//	POST /rotate?drop_old=true   same, but retired keys vanish at once (to test
//	                             what the gateway does with a token whose key is gone)
//
// It is NOT a real auth service: it hands a token to anyone who asks, and keeps
// its keys in memory (a restart = new keys, same as a rotation with drop_old).
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type options struct {
	port        int
	issuer      string
	audience    string
	defaultTTL  time.Duration
	retireAfter time.Duration
}

func parseFlags() options {
	var o options
	flag.IntVar(&o.port, "port", 9100, "port to listen on")
	flag.StringVar(&o.issuer, "issuer", "http://auth.local", "iss claim; must match the gateway's auth.issuer")
	flag.StringVar(&o.audience, "audience", "api-gateway", "aud claim; must match the gateway's auth.audience")
	flag.DurationVar(&o.defaultTTL, "ttl", 15*time.Minute, "default token lifetime")
	flag.DurationVar(&o.retireAfter, "retire-after", 10*time.Minute, "how long a rotated-out key stays in the JWKS")
	flag.Parse()
	return o
}

// signingKey is one RSA key pair and the "kid" that names it.
type signingKey struct {
	kid       string
	priv      *rsa.PrivateKey
	retiredAt time.Time // zero while it is the current key
}

// keyring holds the current key plus keys that were rotated out recently.
// Everything is behind one mutex: /token, /rotate and the JWKS handler all
// touch it, and rotation must be atomic from their point of view.
type keyring struct {
	mu          sync.Mutex
	current     *signingKey
	retired     []*signingKey
	retireAfter time.Duration
}

func newKeyring(retireAfter time.Duration) (*keyring, error) {
	k, err := newSigningKey()
	if err != nil {
		return nil, err
	}
	return &keyring{current: k, retireAfter: retireAfter}, nil
}

func newSigningKey() (*signingKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	// A random kid: it only has to be unique among the keys the gateway might see.
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	return &signingKey{kid: hex.EncodeToString(id), priv: priv}, nil
}

// rotate makes a fresh key current. The old current key is kept for retireAfter
// (so tokens already in flight still verify), unless dropOld is set.
func (kr *keyring) rotate(dropOld bool, now time.Time) (*signingKey, error) {
	next, err := newSigningKey()
	if err != nil {
		return nil, err
	}
	kr.mu.Lock()
	defer kr.mu.Unlock()
	if dropOld {
		kr.retired = nil
	} else {
		kr.current.retiredAt = now
		kr.retired = append(kr.retired, kr.current)
	}
	kr.current = next
	return next, nil
}

// signer returns the current key. Tokens are always signed with it.
func (kr *keyring) signer() *signingKey {
	kr.mu.Lock()
	defer kr.mu.Unlock()
	return kr.current
}

// published returns the keys that belong in the JWKS right now: the current one
// plus retired keys still inside their grace period. Expired ones are dropped here.
func (kr *keyring) published(now time.Time) []*signingKey {
	kr.mu.Lock()
	defer kr.mu.Unlock()
	live := kr.retired[:0]
	for _, k := range kr.retired {
		if now.Sub(k.retiredAt) < kr.retireAfter {
			live = append(live, k)
		}
	}
	kr.retired = live
	return append([]*signingKey{kr.current}, live...)
}

// jwk is one key in RFC 7517 form. For RSA, n and e are the big-endian bytes of
// the modulus and exponent, base64url-encoded WITHOUT padding.
type jwk struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func toJWK(k *signingKey) jwk {
	pub := &k.priv.PublicKey
	// Minimal big-endian bytes of the exponent: 65537 -> 01 00 01 -> "AQAB".
	e := big.NewInt(int64(pub.E)).Bytes()
	return jwk{
		Kty: "RSA",
		Use: "sig",
		Alg: "RS256",
		Kid: k.kid,
		N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(e),
	}
}

type server struct {
	opts   options
	keys   *keyring
	logger *slog.Logger
}

func (s *server) handleJWKS(w http.ResponseWriter, r *http.Request) {
	published := s.keys.published(time.Now())
	set := struct {
		Keys []jwk `json:"keys"`
	}{Keys: make([]jwk, 0, len(published))}
	for _, k := range published {
		set.Keys = append(set.Keys, toJWK(k))
	}
	w.Header().Set("Content-Type", "application/json")
	// A hint for caches; the gateway decides its own refresh policy.
	w.Header().Set("Cache-Control", "public, max-age=60")
	_ = json.NewEncoder(w).Encode(set)
}

func (s *server) handleToken(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sub := q.Get("sub")
	if sub == "" {
		http.Error(w, `{"error":"sub is required"}`, http.StatusBadRequest)
		return
	}
	var roles []string
	if v := q.Get("roles"); v != "" {
		roles = strings.Split(v, ",")
	}
	ttl := s.opts.defaultTTL
	if v := q.Get("ttl"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			http.Error(w, `{"error":"bad ttl"}`, http.StatusBadRequest)
			return
		}
		// Negative ttl is allowed on purpose: it mints an already-expired token for tests.
		ttl = d
	}

	now := time.Now()
	key := s.keys.signer()
	claims := jwt.MapClaims{
		"sub":   sub,
		"iss":   s.opts.issuer,
		"aud":   s.opts.audience,
		"iat":   now.Unix(),
		"exp":   now.Add(ttl).Unix(),
		"roles": roles,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = key.kid
	signed, err := tok.SignedString(key.priv)
	if err != nil {
		s.logger.Error("signing failed", "err", err)
		http.Error(w, `{"error":"signing failed"}`, http.StatusInternalServerError)
		return
	}
	s.logger.Info("token issued", "sub", sub, "kid", key.kid, "ttl", ttl.String())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"token": signed, "kid": key.kid, "expires_in": ttl.Seconds()})
}

func (s *server) handleRotate(w http.ResponseWriter, r *http.Request) {
	dropOld := r.URL.Query().Get("drop_old") == "true"
	next, err := s.keys.rotate(dropOld, time.Now())
	if err != nil {
		s.logger.Error("rotation failed", "err", err)
		http.Error(w, `{"error":"rotation failed"}`, http.StatusInternalServerError)
		return
	}
	s.logger.Info("key rotated", "new_kid", next.kid, "drop_old", dropOld)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"kid": next.kid, "drop_old": dropOld})
}

func main() {
	opts := parseFlags()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "authsvc")

	keys, err := newKeyring(opts.retireAfter)
	if err != nil {
		logger.Error("generating the first key failed", "err", err)
		os.Exit(1)
	}
	s := &server{opts: opts, keys: keys, logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/jwks.json", s.handleJWKS)
	mux.HandleFunc("POST /token", s.handleToken)
	mux.HandleFunc("POST /rotate", s.handleRotate)

	addr := fmt.Sprintf(":%d", opts.port)
	logger.Info("listening", "addr", addr, "issuer", opts.issuer, "audience", opts.audience, "kid", keys.signer().kid)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
