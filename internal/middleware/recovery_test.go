package middleware_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yourname/api-gateway/internal/middleware"
)

func TestRecoveryTurnsPanicInto500(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	boom := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	rec := httptest.NewRecorder()
	middleware.Recovery(logger)(boom).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if !strings.Contains(rec.Body.String(), `"error":"internal_error"`) {
		t.Errorf("body = %q, want JSON internal_error", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("response body leaks the panic value: %q", rec.Body.String())
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Errorf("panic not logged: %q", buf.String())
	}
}

func TestRecoveryRepanicsErrAbortHandler(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	abort := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	})

	defer func() {
		if got := recover(); got != http.ErrAbortHandler {
			t.Errorf("recovered %v, want http.ErrAbortHandler", got)
		}
		if buf.Len() != 0 {
			t.Errorf("ErrAbortHandler should not be logged as an error: %q", buf.String())
		}
	}()
	middleware.Recovery(logger)(abort).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	t.Error("expected a panic to propagate")
}

func TestRecoveryPassesThroughNormalResponse(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	rec := httptest.NewRecorder()
	middleware.Recovery(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))(ok).
		ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want 418", rec.Code)
	}
}
