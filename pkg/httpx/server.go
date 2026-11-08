// Package httpx provides the HTTP server, middleware, and JSON response helpers.
package httpx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Options configures NewServer. Zero timeouts get safe defaults.
type Options struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	Logger            *slog.Logger
}

// Server is an http.Server that shuts down gracefully when its context ends.
type Server struct {
	srv  *http.Server
	opts Options
}

// NewServer returns a Server for h.
func NewServer(h http.Handler, o Options) *Server {
	orDefault(&o.ReadHeaderTimeout, 5*time.Second)
	orDefault(&o.ReadTimeout, 15*time.Second)
	orDefault(&o.WriteTimeout, 30*time.Second)
	orDefault(&o.IdleTimeout, 2*time.Minute)
	orDefault(&o.ShutdownTimeout, 30*time.Second)
	if o.Logger == nil {
		o.Logger = slog.Default()
	}

	return &Server{
		opts: o,
		srv: &http.Server{
			Addr:              o.Addr,
			Handler:           h,
			ReadHeaderTimeout: o.ReadHeaderTimeout,
			ReadTimeout:       o.ReadTimeout,
			WriteTimeout:      o.WriteTimeout,
			IdleTimeout:       o.IdleTimeout,
			MaxHeaderBytes:    1 << 20,
			ErrorLog:          slog.NewLogLogger(o.Logger.Handler(), slog.LevelWarn),
		},
	}
}

// Handler is the server's handler, for tests.
func (s *Server) Handler() http.Handler { return s.srv.Handler }

// Run listens on Addr and serves until ctx is cancelled, then waits up to ShutdownTimeout for in-flight requests.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.opts.Addr)
	if err != nil {
		return fmt.Errorf("httpx: listen %s: %w", s.opts.Addr, err)
	}
	addr := slog.String("addr", ln.Addr().String())
	s.opts.Logger.InfoContext(ctx, "http server listening", addr)

	serveErr := make(chan error, 1)
	go func() { serveErr <- s.srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("httpx: serve: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.opts.ShutdownTimeout)
	defer cancel()
	err = s.srv.Shutdown(shutdownCtx)
	<-serveErr
	s.opts.Logger.InfoContext(ctx, "http server stopped", addr)
	if err != nil {
		return fmt.Errorf("httpx: shutdown: %w", err)
	}
	return nil
}

func orDefault(d *time.Duration, v time.Duration) {
	if *d <= 0 {
		*d = v
	}
}
