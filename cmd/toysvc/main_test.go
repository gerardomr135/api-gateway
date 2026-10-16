// Command toysvc is a configurable fake backend for the gateway lab (M0).
//
// One binary, many personalities: run it several times with different flags
// to get "users", "orders-1", "orders-2", ... Each knob exists so you can
// trigger a specific gateway failure mode on demand:
//
//	-latency    -> timeouts and 504s (M4), slow-upstream degradation (M9)
//	-fail-rate  -> retries (M8), passive health checks (M7), circuit breaker (M8)
//	-unhealthy  -> active health checks and ejection (M7)
//
// Endpoints to implement:
//
//	GET /health  -> 200 {"status":"ok"}, or 503 when -unhealthy is set
//	ANY /        -> 200 JSON echo: service name, port, method, path, query,
//	                and ALL received headers (so you can see exactly what the
//	                gateway forwarded, stripped, or added)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func Test_parseFlags(t *testing.T) {
	// Save the original global state.
	oldArgs := os.Args
	oldFlagSet := flag.CommandLine

	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldFlagSet
	})

	// Provide the command-line arguments that parseFlags() expects.
	os.Args = []string{
		"toysvc",
		"-name=toysvc",
		"-port=9001",
		"-latency=100ms",
		"-fail-rate=0.5",
		"-unhealthy=true",
	}

	// Create a fresh FlagSet so the test doesn't conflict with
	// flags registered by the test binary or other tests.
	flag.CommandLine = flag.NewFlagSet("toysvc", flag.ContinueOnError)

	config := parseFlags()

	if !reflect.DeepEqual(config, options{
		name:      "toysvc",
		port:      9001,
		latency:   100 * time.Millisecond,
		failRate:  0.5,
		unhealthy: true,
	}) {
		t.Errorf("parseFlags() = %+#v, want %+#v", config, options{
			name:      "toysvc",
			port:      9001,
			latency:   100 * time.Millisecond,
			failRate:  0.5,
			unhealthy: true,
		})
	}
}

func Test_healthHandler(t *testing.T) {
	t.Run("GET /health returns 200 when healthy", func(t *testing.T) {
		opts := options{unhealthy: false}
		handler := healthHandler(opts)

		req := httptest.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}
	})

	t.Run("GET /health returns 503 when unhealthy", func(t *testing.T) {
		opts := options{unhealthy: true}
		handler := healthHandler(opts)

		req := httptest.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("expected status 503, got %d", resp.StatusCode)
		}
	})
}

func Test_echoHandler(t *testing.T) {
	tests := []struct {
		name              string
		method            string
		target            string
		headers           http.Header
		opts              options
		expectedStatus    int
		expectEchoBody    bool
		expectedBody      string
		expectedRequestID string
		minLatency        time.Duration
		cancelRequest     bool
	}{
		{
			name:           "returns 200 OK for GET",
			method:         http.MethodGet,
			target:         "/test-path?query=1",
			opts:           options{name: "toysvc", port: 9001},
			expectedStatus: http.StatusOK,
			expectEchoBody: true,
		},
		{
			name:           "returns 200 OK for POST",
			method:         http.MethodPost,
			target:         "/test-path?query=1",
			opts:           options{name: "toysvc", port: 9001},
			expectedStatus: http.StatusOK,
			expectEchoBody: true,
		},
		{
			name:   "returns 200 OK and respects latency",
			method: http.MethodGet,
			target: "/test-path",
			opts: options{
				name:    "toysvc",
				port:    9001,
				latency: 50 * time.Millisecond,
			},
			expectedStatus: http.StatusOK,
			expectEchoBody: true,
			minLatency:     50 * time.Millisecond,
		},
		{
			name:   "returns 500 when fail rate is 100 percent",
			method: http.MethodGet,
			target: "/test-path",
			opts: options{
				name:     "toysvc",
				port:     9001,
				failRate: 1.0,
			},
			expectedStatus: http.StatusInternalServerError,
			expectedBody:   "request failed\n",
		},
		{
			name:   "returns request ID in log when present",
			method: http.MethodGet,
			target: "/test-path",
			headers: http.Header{
				"X-Request-ID": {"abc-123"},
			},
			opts:              options{name: "toysvc", port: 9001},
			expectedStatus:    http.StatusOK,
			expectEchoBody:    true,
			expectedRequestID: "abc-123",
		},
		{
			name:           "returns 408 when request is cancelled",
			method:         http.MethodGet,
			target:         "/test-path",
			opts:           options{name: "toysvc", port: 9001, latency: time.Second},
			expectedStatus: http.StatusRequestTimeout,
			expectedBody:   "request cancelled\n",
			cancelRequest:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fresh logger for every test so log entries
			// from one subtest cannot leak into another.
			loggerBuffer := &Logger{}
			logger := slog.New(slog.NewJSONHandler(loggerBuffer, nil))

			req := httptest.NewRequest(tt.method, tt.target, nil)
			req.Header = tt.headers.Clone()

			if tt.cancelRequest {
				ctx, cancel := context.WithCancel(req.Context())
				req = req.WithContext(ctx)
				cancel()
			}

			recorder := httptest.NewRecorder()

			start := time.Now()

			handler := echoHandler(tt.opts, logger)
			handler.ServeHTTP(recorder, req)

			elapsed := time.Since(start)

			// Verify latency behavior.
			if tt.minLatency > 0 && elapsed < tt.minLatency {
				t.Errorf(
					"echoHandler() returned too quickly: got %v, want at least %v",
					elapsed,
					tt.minLatency,
				)
			}

			// Verify HTTP status.
			result := recorder.Result()
			defer result.Body.Close()

			if result.StatusCode != tt.expectedStatus {
				t.Errorf(
					"echoHandler() returned status %d, want %d",
					result.StatusCode,
					tt.expectedStatus,
				)
			}

			// Read response body once.
			bodyBytes, err := io.ReadAll(result.Body)
			if err != nil {
				t.Fatalf("failed to read response body: %v", err)
			}

			body := string(bodyBytes)

			// Verify expected error body.
			if tt.expectedBody != "" && body != tt.expectedBody {
				t.Errorf(
					"echoHandler() returned body %q, want %q",
					body,
					tt.expectedBody,
				)
			}

			// Successful requests should return the request headers as JSON.
			if tt.expectEchoBody {
				if len(bodyBytes) == 0 {
					t.Fatal("echoHandler() returned an empty body, want JSON")
				}

				actualBody := make(http.Header)

				if err := json.Unmarshal(bodyBytes, &actualBody); err != nil {
					t.Fatalf("echoHandler() returned invalid JSON: %v", err)
				}

				if !maps.EqualFunc(actualBody, req.Header, func(a, b []string) bool {
					return slices.Equal(a, b)
				}) {
					t.Errorf(
						"echoHandler() returned headers %v, want %v",
						actualBody,
						req.Header,
					)
				}

				contentType := result.Header.Get("Content-Type")
				if !strings.HasPrefix(contentType, "application/json") {
					t.Errorf(
						"Content-Type = %q, want application/json",
						contentType,
					)
				}
			}

			assertEchoHandlerLog(
				t,
				loggerBuffer.log,
				req.Method,
				req.URL.Path,
				tt.expectedStatus,
				tt.expectedRequestID,
			)
		})
	}
}

func assertEchoHandlerLog(
	t *testing.T,
	logData string,
	expectedMethod string,
	expectedPath string,
	expectedStatus int,
	expectedRequestID string,
) {
	t.Helper()

	if logData == "" {
		t.Fatal("expected one log entry, got empty log")
	}

	actualLog := make(map[string]any)

	if err := json.Unmarshal([]byte(logData), &actualLog); err != nil {
		t.Fatalf("failed to unmarshal log JSON: %v", err)
	}

	// Only verify fields owned by echoHandler.
	if actualLog["method"] != expectedMethod {
		t.Errorf(
			"log method = %v, want %q",
			actualLog["method"],
			expectedMethod,
		)
	}

	if actualLog["path"] != expectedPath {
		t.Errorf(
			"log path = %v, want %q",
			actualLog["path"],
			expectedPath,
		)
	}

	// JSON numbers are decoded into float64 when using map[string]any.
	actualStatus, ok := actualLog["status"].(float64)
	if !ok {
		t.Fatalf("log status has unexpected type: %T", actualLog["status"])
	}

	if int(actualStatus) != expectedStatus {
		t.Errorf(
			"log status = %v, want %d",
			actualStatus,
			expectedStatus,
		)
	}

	if expectedRequestID == "" {
		if requestID, ok := actualLog["request_id"]; ok && requestID != nil && requestID != "" {
			t.Errorf(
				"log request_id = %v, want empty or absent",
				requestID,
			)
		}
		return
	}
}

type Logger struct {
	log string
}

func (l *Logger) Write(p []byte) (n int, err error) {
	l.log = string(p)
	return len(p), nil
}
