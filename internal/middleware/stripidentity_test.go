package middleware

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestStripIdentity(t *testing.T) {
	// headers are added with Header.Add, which canonicalizes names like a real
	// server does. expected is compared as a literal map, so ITS keys must use
	// the canonical spelling (X-Request-Id, not X-Request-ID; X_user_id).
	tests := []struct {
		name     string
		headers  http.Header
		expected http.Header
	}{
		// --- must be removed ---
		{
			name:    "X-User-Id",
			headers: http.Header{"X-User-Id": {"admin"}},
		},
		{
			name:    "roles",
			headers: http.Header{"x-user-roles": {"admin", "staff"}},
		},
		{
			name:    "unlisted identity header",
			headers: http.Header{"x-user-email": {"a@b.c"}},
		},
		{
			name:    "multiple values",
			headers: http.Header{"X-User-Id": {"a", "b", "c"}},
		},
		{
			name: "several at once",
			headers: http.Header{
				"X-User-Id":    {"admin"},
				"x-user-email": {"a@b.c"},
			},
		},
		{
			name:    "underscore variant",
			headers: http.Header{"x_user_id": {"admin"}},
		},
		{
			name:    "mixed dash and underscore",
			headers: http.Header{"X-User_id": {"admin"}},
		},
		{
			name:    "empty value is still removed",
			headers: http.Header{"X-User-Id": {""}},
		},
		{
			name:    "bare prefix",
			headers: http.Header{"X-User-": {"x"}},
		},

		// --- must survive: without these, clear(r.Header) would pass ---
		{
			name: "unrelated headers untouched",
			headers: http.Header{
				"X-Request-Id":    {"abc"},
				"Authorization":   {"Bearer t"},
				"X-Forwarded-For": {"1.2.3.4"},
				"Content-Type":    {"application/json"},
			},
			expected: http.Header{
				"X-Request-Id":    {"abc"},
				"Authorization":   {"Bearer t"},
				"X-Forwarded-For": {"1.2.3.4"},
				"Content-Type":    {"application/json"},
			},
		},
		{
			name:     "X-Users is not an identity header",
			headers:  http.Header{"X-Users": {"x"}},
			expected: http.Header{"X-Users": {"x"}},
		},
		{
			name: "prefix must be at the start of the name",
			// Foo-X-User-Id contains "x-user-" in the middle: a Contains
			// check would wrongly strip it, a prefix check must not.
			headers: http.Header{
				"Foo-X-User-Id": {"x"},
				"Y-User-Id":     {"y"},
			},
			expected: http.Header{
				"Foo-X-User-Id": {"x"},
				"Y-User-Id":     {"y"},
			},
		},
		{
			name:     "only the name is matched, not the value",
			headers:  http.Header{"X-Note": {"X-User-Id"}},
			expected: http.Header{"X-Note": {"X-User-Id"}},
		},
		{
			// Pins current behavior: the contract is X-User-* (with the dash).
			// If you decide bare X-User must go too, flip this row.
			name:     "bare X-User is outside the X-User-* contract",
			headers:  http.Header{"X-User": {"x"}},
			expected: http.Header{"X-User": {"x"}},
		},
		{
			name: "mixed: only the forged headers go",
			headers: http.Header{
				"X-User-Id":     {"admin"},
				"X_user_roles":  {"admin"},
				"X-Request-Id":  {"abc"},
				"Authorization": {"Bearer t"},
			},
			expected: http.Header{
				"X-Request-Id":  {"abc"},
				"Authorization": {"Bearer t"},
			},
		},
		{
			name: "no headers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)

			for k, v := range tt.headers {
				for _, val := range v {
					req.Header.Add(k, val)
				}
			}

			var got http.Header
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				got = r.Header.Clone()
			})

			StripIdentityHeaders()(next).ServeHTTP(httptest.NewRecorder(), req)

			if !called {
				t.Fatal("next handler was not called")
			}

			if !maps.EqualFunc(tt.expected, got, slices.Equal) {
				t.Errorf("expected: %+v, got: %+v", tt.expected, got)
			}
		})
	}
}

// A later layer must still be able to set the real identity: stripping only
// removes what the CLIENT sent. This is exactly how Auth will use it.
func TestStripIdentityLeavesRoomForLaterLayers(t *testing.T) {
	setVerifiedIdentity := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set(HeaderUserID, "42")
			next.ServeHTTP(w, r)
		})
	}

	var got []string
	recorder := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Values(HeaderUserID)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderUserID, "admin") // forged by the client

	Chain(recorder, StripIdentityHeaders(), setVerifiedIdentity).
		ServeHTTP(httptest.NewRecorder(), req)

	if !slices.Equal(got, []string{"42"}) {
		t.Fatalf("%s = %q, want only the gateway's value [42]", HeaderUserID, got)
	}
}

// Method, path and body must reach next untouched.
func TestStripIdentityDoesNotTouchTheRestOfTheRequest(t *testing.T) {
	var method, path, body string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		b := make([]byte, 16)
		n, _ := r.Body.Read(b)
		body = string(b[:n])
	})

	req := httptest.NewRequest(http.MethodPost, "/api/users/42", strings.NewReader("payload"))
	req.Header.Set("X-User-Id", "admin")

	StripIdentityHeaders()(next).ServeHTTP(httptest.NewRecorder(), req)

	if method != http.MethodPost || path != "/api/users/42" || body != "payload" {
		t.Fatalf("got %s %s %q, want POST /api/users/42 \"payload\"", method, path, body)
	}
}

// The table above relies on net/http canonicalizing header names before our
// middleware sees them (X_User_ID stays an underscore name, x-user-id becomes
// X-User-Id). This proves it over a real connection, with names written on the
// wire exactly as an attacker would: the Go client sends map keys as given.
func TestStripIdentityOverARealConnection(t *testing.T) {
	var seen http.Header
	srv := httptest.NewServer(StripIdentityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
	})))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header["x-user-id"] = []string{"admin"}
	req.Header["X_User_ID"] = []string{"admin"}
	req.Header["X-USER-ROLES"] = []string{"admin"}
	req.Header["X-Request-Id"] = []string{"keep-me"}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	for name := range seen {
		// Written out here on purpose, not shared with the implementation, so
		// a bug in the filter cannot hide itself from its own test.
		if strings.HasPrefix(strings.ToLower(strings.ReplaceAll(name, "_", "-")), "x-user-") {
			t.Errorf("identity header %q reached the handler: %v", name, seen[name])
		}
	}
	if seen.Get("X-Request-Id") != "keep-me" {
		t.Errorf("X-Request-Id = %q, want keep-me (the test must be able to see headers at all)", seen.Get("X-Request-Id"))
	}
}
