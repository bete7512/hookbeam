package httpx

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"

	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/logger"
)

// HeaderRequestID carries the request id in and out.
const HeaderRequestID = "X-Request-ID"

// Middleware wraps an http.Handler.
type Middleware func(http.Handler) http.Handler

// Chain wraps h so the first middleware runs first.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// Default is the standard chain: request id, access log, panic recovery, body limit.
func Default(l *slog.Logger, maxBody int64) []Middleware {
	return []Middleware{RequestID(), Logger(l), Recover(), MaxBody(maxBody)}
}

// RequestID reuses the caller's X-Request-ID or generates one.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(HeaderRequestID)
			if id == "" || len(id) > 128 {
				id = uuid.NewString()
			}
			w.Header().Set(HeaderRequestID, id)
			next.ServeHTTP(w, r.WithContext(logger.WithRequestID(r.Context(), id)))
		})
	}
}

type logEntry struct{ err error }

type logEntryKey struct{}

func recordError(ctx context.Context, err error) {
	if e, ok := ctx.Value(logEntryKey{}).(*logEntry); ok {
		e.err = err
	}
}

// Logger writes one line per request: Info below 500, Error otherwise, with the error if any.
func Logger(l *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			entry := &logEntry{}
			ctx := context.WithValue(r.Context(), logEntryKey{}, entry)
			r = r.WithContext(ctx)
			rw := &responseRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rw, r)

			level := slog.LevelInfo
			if rw.status >= http.StatusInternalServerError {
				level = slog.LevelError
			}
			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("route", r.Pattern),
				slog.String("path", r.URL.Path),
				slog.Int("status", rw.status),
				slog.Float64("duration_ms", float64(time.Since(start))/float64(time.Millisecond)),
			}
			if entry.err != nil {
				attrs = append(attrs, slog.String("error", entry.err.Error()))
			}
			l.LogAttrs(ctx, level, "http request", attrs...)
		})
	}
}

// Recover turns a panic into a 500; the panic and stack end up in the request log.
func Recover() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				p := recover()
				if p == nil {
					return
				}
				if p == http.ErrAbortHandler { //nolint:errorlint // compared by identity, as net/http does
					panic(p)
				}
				WriteError(w, r, apperr.NewInternal(fmt.Errorf("panic: %v\n%s", p, debug.Stack())))
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// MaxBody limits request bodies to n bytes.
func MaxBody(n int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && r.Body != http.NoBody {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}

type responseRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (rw *responseRecorder) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.status = code
		rw.wroteHeader = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseRecorder) Write(b []byte) (int, error) {
	rw.wroteHeader = true
	return rw.ResponseWriter.Write(b)
}

func (rw *responseRecorder) Flush() {
	rw.wroteHeader = true
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (rw *responseRecorder) Unwrap() http.ResponseWriter { return rw.ResponseWriter }
