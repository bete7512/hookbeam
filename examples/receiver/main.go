// Command receiver is a demonstration webhook receiver: it verifies Standard Webhooks signatures, deduplicates by
// webhook-id with a file-backed store, applies a sample "business change" once per message, and lets a person
// pick what it answers, so the delivery loop can be watched end to end on one machine.
//
//	RECEIVER_SECRET=whsec_... go run ./examples/receiver
//
// The control page at / sets the scenario and the secret; /state answers the store as JSON. Query parameters
// on /hooks steer one endpoint: ?scenario=fail_twice, ?status=503 answers that status, ?delay=15s sleeps first.
// Without a secret it answers 401 to every delivery and the attempt's response preview says what to do.
package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
)

func main() {
	addr := envOr("RECEIVER_ADDR", ":8081")
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	store, err := openStore(os.Getenv("RECEIVER_STATE_FILE"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "receiver:", err)
		os.Exit(1)
	}
	if secret := os.Getenv("RECEIVER_SECRET"); secret != "" {
		store.SetSecret(secret)
	}
	if store.Secret() == "" {
		log.Warn("no secret yet: every delivery is answered 401 until the endpoint's whsec_ secret is set on the control page or RECEIVER_SECRET")
	}
	r := newReceiver(store, log, envDuration("RECEIVER_TIMEOUT_DELAY", 15*time.Second))
	log.Info("receiver listening", "addr", addr, "hooks", "/hooks", "control", "/", "state_file", store.Path())
	srv := &http.Server{Addr: addr, Handler: r.routes(), ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "receiver:", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if d, err := time.ParseDuration(os.Getenv(key)); err == nil && d > 0 {
		return d
	}
	return fallback
}
