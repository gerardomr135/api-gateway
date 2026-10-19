package middleware_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yourname/api-gateway/internal/config"
	"github.com/yourname/api-gateway/internal/middleware"
	"github.com/yourname/api-gateway/internal/router"
)

// External test package: router imports middleware, so a test inside package
// middleware could not import router without an import cycle.
func TestAccessLogRouteName(t *testing.T) {
	routes := []config.Route{
		{Name: "users", Prefix: "/api/users", Methods: []string{"GET"}},
	}
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hi"))
	})
	rt, err := router.New(routes, map[string]http.Handler{"users": ok})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name, method, path string
		wantRoute          string
		wantStatus         int
	}{
		{"matched route", "GET", "/api/users/42", "users", 200},
		{"not found", "GET", "/nope", "-", 404},
		{"method not allowed", "DELETE", "/api/users/42", "-", 405},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))
			h := middleware.Chain(rt, middleware.AccessLog(logger))

			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(tt.method, tt.path, nil))

			var line map[string]any
			if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
				t.Fatalf("log line is not JSON: %v\n%s", err, buf.String())
			}
			if line["route"] != tt.wantRoute {
				t.Errorf("route = %v, want %q", line["route"], tt.wantRoute)
			}
			if got := int(line["status"].(float64)); got != tt.wantStatus {
				t.Errorf("status = %d, want %d", got, tt.wantStatus)
			}
		})
	}
}

func TestRequestIDHeaders(t *testing.T) {
	const valid = "123e4567-e89b-42d3-a456-426614174000"

	tests := []struct {
		name, inbound string
		wantSame      bool
	}{
		{"reuses valid inbound ID", valid, true},
		{"generates when missing", "", false},
		{"replaces invalid inbound ID", "not a uuid\n", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen string
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen = r.Header.Get(middleware.HeaderRequestID)
				if got := middleware.RequestIDFrom(r.Context()); got != seen {
					t.Errorf("context ID %q != request header %q", got, seen)
				}
			})
			req := httptest.NewRequest("GET", "/", nil)
			if tt.inbound != "" {
				req.Header["X-Request-Id"] = []string{tt.inbound}
			}
			rec := httptest.NewRecorder()
			middleware.RequestID()(next).ServeHTTP(rec, req)

			got := rec.Header().Get(middleware.HeaderRequestID)
			if got == "" || got != seen {
				t.Errorf("response ID %q, upstream-bound ID %q: want equal and non-empty", got, seen)
			}
			if (got == tt.inbound) != tt.wantSame {
				t.Errorf("ID %q vs inbound %q: wantSame=%v", got, tt.inbound, tt.wantSame)
			}
		})
	}
}

// Every error the gateway generates itself must be JSON with the request ID.
func TestErrorResponsesAreJSON(t *testing.T) {
	routes := []config.Route{{Name: "users", Prefix: "/api/users", Methods: []string{"GET"}}}
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	rt, err := router.New(routes, map[string]http.Handler{"users": ok})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	h := middleware.Chain(rt, middleware.Recovery(logger), middleware.RequestID(), middleware.AccessLog(logger))

	tests := []struct {
		name, method, path string
		wantStatus         int
		wantCode           string
		wantAllow          string
	}{
		{"not found", "GET", "/nope", 404, "not_found", ""},
		{"method not allowed", "DELETE", "/api/users/1", 405, "method_not_allowed", "GET"},
		{"dot segment", "GET", "/api/users/%2e%2e/x", 400, "bad_request", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			if got := rec.Header().Get("Allow"); got != tt.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tt.wantAllow)
			}
			var body struct {
				Error     string `json:"error"`
				Message   string `json:"message"`
				RequestID string `json:"request_id"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body is not JSON: %v\n%s", err, rec.Body.String())
			}
			if body.Error != tt.wantCode {
				t.Errorf("error = %q, want %q", body.Error, tt.wantCode)
			}
			if body.Message == "" {
				t.Error("message is empty")
			}
			if body.RequestID == "" || body.RequestID != rec.Header().Get(middleware.HeaderRequestID) {
				t.Errorf("body request_id %q != header %q", body.RequestID, rec.Header().Get(middleware.HeaderRequestID))
			}
		})
	}
}
