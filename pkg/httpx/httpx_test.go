package httpx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/scaffold/pkg/apperr"
	"github.com/bete7512/scaffold/pkg/httpx"
	"github.com/bete7512/scaffold/pkg/logger"
)

// serve runs h behind the default middleware and returns the response and its single log line.
func serve(t *testing.T, h http.HandlerFunc, req *http.Request) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	l := logger.New(logger.Options{Level: "info", Format: "json", Writer: &buf})
	mux := http.NewServeMux()
	mux.HandleFunc("/things", h)
	rec := httptest.NewRecorder()
	httpx.Chain(mux, httpx.Default(l, 64)...).ServeHTTP(rec, req)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 1)
	var line map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &line))
	return rec, line
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) httpx.ErrorResponse {
	t.Helper()
	var body httpx.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

func TestWriteError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantLevel  string
	}{
		{"not found", apperr.NewNotFound("user", nil), http.StatusNotFound, apperr.CodeNotFound, "INFO"},
		{"validation", apperr.NewValidation(map[string]string{"email": "is required"}), http.StatusUnprocessableEntity, apperr.CodeValidation, "INFO"},
		{"plain error", errors.New("pg: secret detail"), http.StatusInternalServerError, apperr.CodeInternal, "ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := func(w http.ResponseWriter, r *http.Request) { httpx.WriteError(w, r, tt.err) }
			rec, line := serve(t, h, httptest.NewRequest(http.MethodGet, "/things", nil))

			require.Equal(t, tt.wantStatus, rec.Code)
			body := decodeError(t, rec)
			assert.Equal(t, tt.wantCode, body.Error)
			assert.Equal(t, rec.Header().Get(httpx.HeaderRequestID), body.RequestID)
			assert.NotContains(t, rec.Body.String(), "secret detail")

			assert.Equal(t, tt.wantLevel, line["level"])
			assert.Equal(t, tt.err.Error(), line["error"])
			assert.Equal(t, body.RequestID, line["request_id"])
		})
	}
}

func TestSuccessIsLoggedWithoutError(t *testing.T) {
	h := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	req := httptest.NewRequest(http.MethodGet, "/things", nil)
	req.Header.Set(httpx.HeaderRequestID, "req-123")

	rec, line := serve(t, h, req)

	assert.Equal(t, "req-123", rec.Header().Get(httpx.HeaderRequestID))
	assert.Equal(t, "INFO", line["level"])
	assert.InDelta(t, http.StatusNoContent, line["status"], 0)
	assert.NotContains(t, line, "error")
}

func TestPanicBecomes500(t *testing.T) {
	h := func(http.ResponseWriter, *http.Request) { panic("boom") }
	rec, line := serve(t, h, httptest.NewRequest(http.MethodGet, "/things", nil))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, apperr.CodeInternal, decodeError(t, rec).Error)
	assert.Equal(t, "ERROR", line["level"])
	assert.Contains(t, line["error"], "panic: boom")
}

func TestDecodeJSON(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"valid", `{"name":"ada"}`, http.StatusNoContent},
		{"unknown field", `{"name":"ada","x":1}`, http.StatusBadRequest},
		{"trailing data", `{"name":"ada"} {}`, http.StatusBadRequest},
		{"trailing bracket", `{"name":"ada"}]`, http.StatusBadRequest},
		{"trailing brace", `{"name":"ada"}}`, http.StatusBadRequest},
		{"trailing scalar", `{"name":"ada"} true`, http.StatusBadRequest},
		{"trailing garbage", `{"name":"ada"} xyz`, http.StatusBadRequest},
		{"trailing whitespace", "{\"name\":\"ada\"} \n\t", http.StatusNoContent},
		{"empty", ``, http.StatusBadRequest},
		{"oversized trailing whitespace", `{"name":"ada"}` + strings.Repeat(" ", 100), http.StatusRequestEntityTooLarge},
		{"too large", `{"name":"` + strings.Repeat("a", 100) + `"}`, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := func(w http.ResponseWriter, r *http.Request) {
				var v struct {
					Name string `json:"name"`
				}
				if err := httpx.DecodeJSON(r, &v); err != nil {
					httpx.WriteError(w, r, err)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}
			rec, _ := serve(t, h, httptest.NewRequest(http.MethodPost, "/things", strings.NewReader(tt.body)))
			assert.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
		})
	}
}

func TestServerGracefulShutdown(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = io.WriteString(w, "done")
	})
	addr := freeAddr(t)
	srv := httpx.NewServer(mux, httpx.Options{Addr: addr, Logger: slog.New(slog.DiscardHandler), ShutdownTimeout: 5 * time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Run(ctx) }()
	waitForServer(t, addr)

	type result struct {
		body string
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/slow")
		if err != nil {
			resCh <- result{err: err}
			return
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		resCh <- result{body: string(b), err: err}
	}()

	<-started
	cancel()
	close(release)

	res := <-resCh
	require.NoError(t, res.err)
	assert.Equal(t, "done", res.body)
	require.NoError(t, <-serveErr)
}

// freeAddr reserves a free local port and releases it for the server to bind.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}

// waitForServer blocks until addr accepts TCP connections.
func waitForServer(t *testing.T, addr string) {
	t.Helper()
	require.Eventually(t, func() bool {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, 2*time.Second, 10*time.Millisecond)
}
