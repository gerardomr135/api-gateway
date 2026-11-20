package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func jwkJSON(kid string, k *rsa.PublicKey) map[string]string {
	return map[string]string{
		"kty": "RSA", "kid": kid,
		"n": base64.RawURLEncoding.EncodeToString(k.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.E)).Bytes()),
	}
}

func tokenFor(kid string) *jwt.Token {
	t := jwt.New(jwt.SigningMethodRS256)
	t.Header["kid"] = kid
	return t
}

type idp struct {
	srv   *httptest.Server
	hits  atomic.Int32
	mu    sync.Mutex
	keys  map[string]*rsa.PublicKey
	state int // 0 = ok, 1 = 500
}

func newIDP(t *testing.T, keys map[string]*rsa.PublicKey) *idp {
	p := &idp{keys: keys}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.hits.Add(1)
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.state == 1 {
			http.Error(w, "boom", 500)
			return
		}
		var list []map[string]string
		for kid, k := range p.keys {
			list = append(list, jwkJSON(kid, k))
		}
		json.NewEncoder(w).Encode(map[string]any{"keys": list})
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func genKey(t *testing.T) *rsa.PublicKey {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return &k.PublicKey
}

func TestJWKS_FetchCacheAndRotation(t *testing.T) {
	k1, k2 := genKey(t), genKey(t)
	p := newIDP(t, map[string]*rsa.PublicKey{"a": k1})
	now := time.Now()
	j := NewJWKS(p.srv.URL, time.Minute)
	j.now = func() time.Time { return now }

	got, err := j.Key(tokenFor("a"))
	if err != nil || got.(*rsa.PublicKey).N.Cmp(k1.N) != 0 {
		t.Fatalf("first key: %v %v", got, err)
	}
	j.Key(tokenFor("a"))
	if p.hits.Load() != 1 {
		t.Fatalf("cache miss on second call, hits=%d", p.hits.Load())
	}

	// rotation: new kid appears, but inside minInterval -> no fetch
	p.mu.Lock()
	p.keys["b"] = k2
	p.mu.Unlock()
	if _, err := j.Key(tokenFor("b")); err == nil || p.hits.Load() != 1 {
		t.Fatalf("expected rate-limited unknown kid, err=%v hits=%d", err, p.hits.Load())
	}
	now = now.Add(2 * time.Minute)
	if _, err := j.Key(tokenFor("b")); err != nil {
		t.Fatalf("after interval: %v", err)
	}
}

func TestJWKS_FloodOfRandomKids(t *testing.T) {
	p := newIDP(t, map[string]*rsa.PublicKey{"a": genKey(t)})
	j := NewJWKS(p.srv.URL, time.Minute)
	for i := 0; i < 50; i++ {
		j.Key(tokenFor("nope" + string(rune('a'+i%20))))
	}
	if p.hits.Load() != 1 {
		t.Fatalf("flood caused %d fetches, want 1", p.hits.Load())
	}
}

func TestJWKS_Stampede(t *testing.T) {
	p := newIDP(t, map[string]*rsa.PublicKey{"a": genKey(t)})
	j := NewJWKS(p.srv.URL, time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := j.Key(tokenFor("a")); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if p.hits.Load() != 1 {
		t.Fatalf("stampede: %d fetches, want 1", p.hits.Load())
	}
}

func TestJWKS_OutageKeepsCache(t *testing.T) {
	p := newIDP(t, map[string]*rsa.PublicKey{"a": genKey(t)})
	now := time.Now()
	j := NewJWKS(p.srv.URL, time.Minute)
	j.now = func() time.Time { return now }
	if _, err := j.Key(tokenFor("a")); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.state = 1
	p.mu.Unlock()
	now = now.Add(2 * time.Minute)
	if _, err := j.Key(tokenFor("zzz")); err == nil {
		t.Fatal("unknown kid during outage must fail")
	}
	if _, err := j.Key(tokenFor("a")); err != nil {
		t.Fatalf("cached key must survive outage: %v", err)
	}
}

func TestJWKS_RejectsNonRSAAndMissingKid(t *testing.T) {
	j := NewJWKS("http://127.0.0.1:1", time.Minute)
	hs := jwt.New(jwt.SigningMethodHS256)
	hs.Header["kid"] = "a"
	if _, err := j.Key(hs); err == nil {
		t.Fatal("HS256 must be rejected")
	}
	if _, err := j.Key(jwt.New(jwt.SigningMethodRS256)); err == nil {
		t.Fatal("missing kid must be rejected")
	}
}

func TestJWKS_Prime(t *testing.T) {
	t.Run("loads the keys so the first request needs no fetch", func(t *testing.T) {
		p := newIDP(t, map[string]*rsa.PublicKey{"a": genKey(t)})
		j := NewJWKS(p.srv.URL, time.Minute)
		if err := j.Prime(); err != nil {
			t.Fatal(err)
		}
		if _, err := j.Key(tokenFor("a")); err != nil {
			t.Fatal(err)
		}
		if p.hits.Load() != 1 {
			t.Fatalf("hits = %d, want 1 (only the startup fetch)", p.hits.Load())
		}
	})

	t.Run("reports an unreachable or broken provider", func(t *testing.T) {
		p := newIDP(t, map[string]*rsa.PublicKey{"a": genKey(t)})
		p.state = 1
		if err := NewJWKS(p.srv.URL, time.Minute).Prime(); err == nil {
			t.Fatal("Prime() = nil, want an error for a 500")
		}
		if err := NewJWKS("http://127.0.0.1:1", time.Minute).Prime(); err == nil {
			t.Fatal("Prime() = nil, want an error when nothing is listening")
		}
	})
}
