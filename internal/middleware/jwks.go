package middleware

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// maxJWKSBody caps how much of the identity provider's response we will read.
const maxJWKSBody = 1 << 20 // 1 MiB

var errUnknownKid = errors.New("unknown kid")

type JWKS struct {
	url         string
	client      *http.Client
	minInterval time.Duration

	fetchMu   *sync.Mutex
	lastFetch time.Time

	mu   *sync.RWMutex
	keys map[string]*rsa.PublicKey

	now func() time.Time
}

func NewJWKS(url string, minInterval time.Duration) *JWKS {
	return &JWKS{
		url:         url,
		minInterval: minInterval,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		now:     time.Now,
		mu:      &sync.RWMutex{},
		fetchMu: &sync.Mutex{},
		keys:    make(map[string]*rsa.PublicKey),
	}
}

func (j *JWKS) Key(t *jwt.Token) (any, error) {
	if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
		return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
	}

	kid, ok := t.Header["kid"].(string)
	if !ok || kid == "" {
		return nil, errors.New("token has no usable kid")
	}

	if k := j.lookup(kid); k != nil {
		return k, nil
	}

	return j.refresh(kid)
}

func (j *JWKS) lookup(kid string) *rsa.PublicKey {
	j.mu.RLock()
	defer j.mu.RUnlock()

	if key, ok := j.keys[kid]; ok {
		return key
	}

	return nil
}

func (j *JWKS) refresh(kid string) (any, error) {
	j.fetchMu.Lock()
	defer j.fetchMu.Unlock()

	if k := j.lookup(kid); k != nil {
		return k, nil
	}

	if j.now().Sub(j.lastFetch) < j.minInterval {
		return nil, errUnknownKid
	}
	keys, err := j.fetchAndStore()
	if err != nil {
		return nil, err
	}

	if k := keys[kid]; k != nil {
		return k, nil
	}

	return nil, errUnknownKid
}

// fetchAndStore fetches the JWKS and, only if that worked, swaps it in as the
// new cache. The caller must hold fetchMu. lastFetch is stamped BEFORE the
// fetch so that failures count against the rate limit too; on failure the old
// cache is left untouched (an identity-provider outage must not log out
// users whose keys we already have).
func (j *JWKS) fetchAndStore() (map[string]*rsa.PublicKey, error) {
	j.lastFetch = j.now()

	keys, err := j.fetch()
	if err != nil {
		return nil, err
	}

	j.mu.Lock()
	j.keys = keys
	j.mu.Unlock()
	return keys, nil
}

// Prime fetches the JWKS once. main calls it at startup so that a wrong URL or
// an unreachable identity provider is found when the gateway boots, not when
// the first user gets a 401.
func (j *JWKS) Prime() error {
	j.fetchMu.Lock()
	defer j.fetchMu.Unlock()
	_, err := j.fetchAndStore()
	return err
}

// fetch downloads and parses the JWKS. It touches no shared state and holds
// no lock other than fetchMu (held by the caller).
func (j *JWKS) fetch() (map[string]*rsa.PublicKey, error) {
	resp, err := j.client.Get(j.url)
	if err != nil {
		return nil, fmt.Errorf("error talking to identity provider: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("identity provider returned %s", resp.Status)
	}

	var doc struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJWKSBody)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("invalid identity provider response: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		pub, err := parseRSAKey(k.N, k.E)
		if err != nil {
			continue // one bad entry must not poison the others
		}
		keys[k.Kid] = pub
	}

	// An empty result would wipe the cache and lock everyone out. Treat it as
	// a failed fetch instead.
	if len(keys) == 0 {
		return nil, errors.New("identity provider returned no usable RSA keys")
	}
	return keys, nil
}

// parseRSAKey builds a public key from the base64url (unpadded) n and e of a JWK.
func parseRSAKey(nStr, eStr string) (*rsa.PublicKey, error) {
	nb, err := base64.RawURLEncoding.DecodeString(nStr)
	if err != nil {
		return nil, fmt.Errorf("bad modulus: %w", err)
	}
	eb, err := base64.RawURLEncoding.DecodeString(eStr)
	if err != nil {
		return nil, fmt.Errorf("bad exponent: %w", err)
	}
	if len(nb) == 0 || len(eb) == 0 {
		return nil, errors.New("empty modulus or exponent")
	}
	e := new(big.Int).SetBytes(eb)
	if !e.IsInt64() || e.Int64() < 3 || e.Int64() > 1<<31-1 {
		return nil, errors.New("unsupported exponent")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: int(e.Int64())}, nil
}
