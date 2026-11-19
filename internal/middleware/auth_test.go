package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var testDeps = AuthDependencies{
	Keys: StaticKey("secret"), Algorithm: "HS256", Issuer: "iss", Audience: "aud",
}

func mkToken(t *testing.T, key string, exp time.Time, method jwt.SigningMethod) string {
	t.Helper()
	claims := UserClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "u1", Issuer: "iss", Audience: jwt.ClaimStrings{"aud"},
			ExpiresAt: jwt.NewNumericDate(exp),
		},
		Roles: []string{"a", "b"},
	}
	s, err := jwt.NewWithClaims(method, claims).SignedString([]byte(key))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAuth(t *testing.T) {
	good := mkToken(t, "secret", time.Now().Add(time.Hour), jwt.SigningMethodHS256)
	tests := []struct {
		name, policy, header string
		wantStatus           int
		wantID               string
	}{
		{"required no header", "required", "", 401, ""},
		{"required valid", "required", "Bearer " + good, 200, "u1"},
		{"required lowercase scheme", "required", "bearer " + good, 200, "u1"},
		{"required wrong key", "required", "Bearer " + mkToken(t, "other", time.Now().Add(time.Hour), jwt.SigningMethodHS256), 401, ""},
		{"required expired", "required", "Bearer " + mkToken(t, "secret", time.Now().Add(-time.Hour), jwt.SigningMethodHS256), 401, ""},
		{"required wrong alg", "required", "Bearer " + mkToken(t, "secret", time.Now().Add(time.Hour), jwt.SigningMethodHS512), 401, ""},
		{"required not bearer", "required", "Basic abc", 401, ""},
		{"scheme only as substring", "required", "X Bearer " + good, 401, ""},
		{"optional no header", "optional", "", 200, ""},
		{"optional valid", "optional", "Bearer " + good, 200, "u1"},
		{"optional invalid token", "optional", "Bearer garbage", 401, ""},
		{"none ignores header", "none", "Bearer garbage", 200, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotID string
			h := Auth(testDeps, tc.policy)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotID = r.Header.Get(HeaderUserID)
			}))
			req := httptest.NewRequest("GET", "/", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if gotID != tc.wantID {
				t.Errorf("X-User-ID = %q, want %q", gotID, tc.wantID)
			}
			if tc.wantStatus == 401 && !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "Bearer") {
				t.Errorf("missing WWW-Authenticate: %q", rec.Header().Get("WWW-Authenticate"))
			}
		})
	}
}
