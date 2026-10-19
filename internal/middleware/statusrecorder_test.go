package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Internal test: statusRecorder is unexported.
func TestStatusRecorder(t *testing.T) {
	tests := []struct {
		name       string
		handler    func(w http.ResponseWriter)
		wantStatus int
		wantBytes  int
	}{
		{
			name:       "never writes anything: defaults to 200",
			handler:    func(w http.ResponseWriter) {},
			wantStatus: 200,
		},
		{
			name:       "write only: implicit 200, bytes counted",
			handler:    func(w http.ResponseWriter) { w.Write([]byte("hello")) },
			wantStatus: 200,
			wantBytes:  5,
		},
		{
			name: "explicit status then body",
			handler: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusCreated)
				w.Write([]byte("ab"))
			},
			wantStatus: 201,
			wantBytes:  2,
		},
		{
			name: "second WriteHeader is ignored",
			handler: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusNotFound)
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantStatus: 404,
		},
		{
			name: "WriteHeader after Write is ignored",
			handler: func(w http.ResponseWriter) {
				w.Write([]byte("x"))
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantStatus: 200,
			wantBytes:  1,
		},
		{
			name: "bytes accumulate across writes",
			handler: func(w http.ResponseWriter) {
				w.Write([]byte("abc"))
				w.Write([]byte("de"))
			},
			wantStatus: 200,
			wantBytes:  5,
		},
		{
			name: "1xx informational is not the final status",
			handler: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusEarlyHints)
				w.WriteHeader(http.StatusAccepted)
			},
			wantStatus: 202,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &statusRecorder{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}
			tt.handler(rec)
			if rec.status != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.status, tt.wantStatus)
			}
			if rec.bytes != tt.wantBytes {
				t.Errorf("bytes = %d, want %d", rec.bytes, tt.wantBytes)
			}
		})
	}
}

// Flush must reach the underlying writer, otherwise SSE is buffered.
func TestStatusRecorderFlushAndUnwrap(t *testing.T) {
	under := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: under, status: http.StatusOK}

	rec.Write([]byte("data: 1\n\n"))
	rec.Flush()
	if !under.Flushed {
		t.Error("Flush was not delegated to the underlying writer")
	}

	if rec.Unwrap() != http.ResponseWriter(under) {
		t.Error("Unwrap did not return the original writer")
	}
	// ResponseController should reach Flush through Unwrap alone.
	under.Flushed = false
	if err := http.NewResponseController(rec).Flush(); err != nil || !under.Flushed {
		t.Errorf("ResponseController.Flush: err=%v flushed=%v", err, under.Flushed)
	}
}

// Hijack must reach the underlying connection through the wrapper.
func TestStatusRecorderHijack(t *testing.T) {
	done := make(chan bool, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		var h http.Hijacker = rec
		conn, _, err := h.Hijack()
		if err != nil {
			t.Errorf("Hijack: %v", err)
			done <- false
			return
		}
		conn.Close()
		done <- rec.status == http.StatusSwitchingProtocols
	}))
	defer srv.Close()
	if resp, err := http.Get(srv.URL); err == nil {
		resp.Body.Close()
	}
	if !<-done {
		t.Error("hijack failed or status not recorded as 101")
	}
}
