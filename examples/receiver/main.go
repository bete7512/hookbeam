// Command receiver is a demonstration webhook receiver: it verifies Standard Webhooks signatures, deduplicates by
// webhook-id and can be told to fail, so the delivery loop can be watched end to end on one machine.
//
//	RECEIVER_SECRET=whsec_... go run ./examples/receiver
//
// Query parameters steer the response: ?status=503 answers that status; ?delay=15s sleeps first.
package main

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/bete7512/hookbeam/pkg/webhooks"
)

const tolerance = 5 * time.Minute

func main() {
	addr := envOr("RECEIVER_ADDR", ":8081")
	secret := os.Getenv("RECEIVER_SECRET")
	if secret == "" {
		fmt.Fprintln(os.Stderr, "receiver: RECEIVER_SECRET (the endpoint's whsec_ secret) is required")
		os.Exit(1)
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	r := &receiver{secret: secret, seen: map[string]time.Time{}, log: log}
	log.Info("receiver listening", "addr", addr, "path", "/hooks")
	srv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "receiver:", err)
		os.Exit(1)
	}
}

type receiver struct {
	secret string
	mu     sync.Mutex
	seen   map[string]time.Time // webhook-id → first receipt: a real receiver keeps this in its database
	log    *slog.Logger
}

func (r *receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(io.LimitReader(req.Body, 1<<20))
	if err != nil {
		http.Error(w, "unreadable body", http.StatusBadRequest)
		return
	}
	id := req.Header.Get(webhooks.HeaderID)
	if err := webhooks.Verify([]string{r.secret}, req.Header, body, time.Now(), tolerance); err != nil {
		r.log.Warn("rejected", "webhook_id", id, "reason", err)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	if d, err := time.ParseDuration(req.URL.Query().Get("delay")); err == nil && d > 0 {
		time.Sleep(d)
	}
	if status, err := strconv.Atoi(req.URL.Query().Get("status")); err == nil && status >= 300 {
		r.log.Info("verified, answering as told", "webhook_id", id, "status", status)
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(status)
		return
	}
	r.mu.Lock()
	first, duplicate := r.seen[id]
	if !duplicate {
		r.seen[id] = time.Now()
	}
	r.mu.Unlock()
	if duplicate {
		r.log.Info("verified duplicate, acknowledged without re-applying", "webhook_id", id, "first_seen", first.Format(time.RFC3339))
	} else {
		r.log.Info("verified and applied", "webhook_id", id, "timestamp", req.Header.Get(webhooks.HeaderTimestamp), "bytes", len(body), "body", string(body))
	}
	w.WriteHeader(http.StatusNoContent)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
