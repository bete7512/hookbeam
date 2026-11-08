//go:build faults

// Package faults drives the compose stack through the specification's crash scenarios and reads the receiver's
// store to prove nothing accepted was lost and nothing was applied twice. Run with `make test-faults` against
// `make demo-up`; BASE_URL, RECEIVER_URL and COMPOSE override the defaults.
package faults

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var (
	baseURL     = envOr("BASE_URL", "http://localhost:3000")
	receiverURL = envOr("RECEIVER_URL", "http://localhost:8081")
	composeCmd  = strings.Fields(envOr("COMPOSE", "docker compose -f docker-compose.yml"))
	repoRoot    = filepath.Clean(filepath.Join(mustWd(), "..", ".."))
)

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func mustWd() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return wd
}

// compose runs one docker compose command from the repository root.
func compose(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command(composeCmd[0], append(composeCmd[1:], args...)...) //nolint:gosec // the test drives its own stack
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "docker compose %s: %s", strings.Join(args, " "), out)
	return string(out)
}

// client is a thin HTTP helper over the stack: it decodes JSON and fails the test on unexpected statuses.
type client struct {
	t     *testing.T
	token string
}

func (c *client) do(method, path string, body any, headers map[string]string, out any, want ...int) int {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(c.t, err)
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, baseURL+path, reader)
	require.NoError(c.t, err)
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(c.t, err)
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if len(want) > 0 {
		require.Contains(c.t, want, resp.StatusCode, "%s %s: %s", method, path, data)
	}
	if out != nil && len(data) > 0 {
		require.NoError(c.t, json.Unmarshal(data, out), "%s %s: %s", method, path, data)
	}
	return resp.StatusCode
}

// stack is one workspace, application, endpoint at the receiver and key, ready to submit.
type stack struct {
	c           *client
	key         *client
	application int64
	endpoint    int64
}

func setup(t *testing.T) *stack {
	t.Helper()
	stamp := fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
	email := "faults-" + stamp + "@example.com"
	password := "Passw0rd!long"
	c := &client{t: t}
	c.do(http.MethodPost, "/v1/auth/signup", map[string]string{"name": "Faults " + stamp, "email": email, "password": password}, nil, nil, 201, 200)
	var login struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	c.do(http.MethodPost, "/v1/auth/login", map[string]string{"email": email, "password": password}, nil, &login, 200)
	c.token = login.Data.AccessToken
	var org string
	require.Eventually(t, func() bool {
		var orgs struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		c.do(http.MethodGet, "/v1/auth/org/my", nil, nil, &orgs, 200)
		if len(orgs.Data) > 0 {
			org = orgs.Data[0].ID
			return true
		}
		return false
	}, 30*time.Second, time.Second, "goauth creates the workspace after signup")
	c.do(http.MethodPost, "/v1/auth/org/switch", map[string]string{"org_id": org}, nil, &login, 200)
	c.token = login.Data.AccessToken

	var app struct {
		ID int64 `json:"id"`
	}
	c.do(http.MethodPost, "/v1/applications", map[string]string{"name": "Faults"}, nil, &app, 201)
	var ep struct {
		Endpoint struct {
			ID int64 `json:"id"`
		} `json:"endpoint"`
		SigningSecret string `json:"signingSecret"`
	}
	c.do(http.MethodPost, fmt.Sprintf("/v1/applications/%d/endpoints", app.ID), map[string]any{"url": "http://receiver:8081/hooks", "eventTypes": []string{"order.shipped"}}, nil, &ep, 201)
	control(t, map[string]string{"secret": ep.SigningSecret, "scenario": "ok", "delay": "0"})
	resetReceiver(t)
	var key struct {
		Key string `json:"key"`
	}
	c.do(http.MethodPost, fmt.Sprintf("/v1/applications/%d/api-keys", app.ID), map[string]any{"name": "faults", "scopes": []string{"messages:write", "messages:read"}}, nil, &key, 201)
	return &stack{c: c, key: &client{t: t, token: key.Key}, application: app.ID, endpoint: ep.Endpoint.ID}
}

type accepted struct {
	MessageID int64  `json:"messageId"`
	WebhookID string `json:"webhookId"`
	Duplicate bool   `json:"duplicate"`
}

func (s *stack) submit(idempotencyKey string, want ...int) accepted {
	var a accepted
	s.key.do(http.MethodPost, fmt.Sprintf("/v1/applications/%d/messages", s.application), map[string]any{"type": "order.shipped", "external_id": idempotencyKey, "data": map[string]any{"n": 1}}, map[string]string{"Idempotency-Key": idempotencyKey}, &a, want...)
	return a
}

type message struct {
	Deliveries []struct {
		ID           int64  `json:"id"`
		State        string `json:"state"`
		AttemptCount int32  `json:"attemptCount"`
	} `json:"deliveries"`
}

func (s *stack) waitDelivered(messageID int64, timeout time.Duration) message {
	var m message
	require.Eventually(s.c.t, func() bool {
		s.key.do(http.MethodGet, fmt.Sprintf("/v1/messages/%d", messageID), nil, nil, &m, 200)
		return len(m.Deliveries) > 0 && m.Deliveries[0].State == "succeeded"
	}, timeout, time.Second, "message %d delivered", messageID)
	return m
}

// receiver state

type receiverState struct {
	Counter int `json:"counter"`
	Records []struct {
		WebhookID  string `json:"webhook_id"`
		Attempts   int    `json:"attempts"`
		Duplicates int    `json:"duplicates"`
		Applied    bool   `json:"applied"`
	} `json:"records"`
}

func receiver(t *testing.T) receiverState {
	t.Helper()
	resp, err := http.Get(receiverURL + "/state")
	require.NoError(t, err)
	defer resp.Body.Close()
	var st receiverState
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&st))
	return st
}

func (st receiverState) record(webhookID string) (attempts, duplicates int, applied bool) {
	for _, r := range st.Records {
		if r.WebhookID == webhookID {
			return r.Attempts, r.Duplicates, r.Applied
		}
	}
	return 0, 0, false
}

func control(t *testing.T, fields map[string]string) {
	t.Helper()
	form := url.Values{}
	for k, v := range fields {
		form.Set(k, v)
	}
	resp, err := http.PostForm(receiverURL+"/control", form)
	require.NoError(t, err)
	resp.Body.Close()
	require.Less(t, resp.StatusCode, 400)
}

func resetReceiver(t *testing.T) { control(t, map[string]string{"action": "reset"}) }

func waitReady(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		resp, err := http.Get(baseURL + "/readyz")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 90*time.Second, time.Second, "the API is ready again")
}

// psql runs one query in the control plane's database and returns the trimmed output.
func psql(t *testing.T, db, query string) string {
	t.Helper()
	out := compose(t, "exec", "-T", "postgres", "psql", "-U", envOr("POSTGRES_USER", "postgres"), "-d", db, "-tAc", query)
	return strings.TrimSpace(out)
}

// TestFaultAPIKilledBeforeResponse: an acceptance the sender never saw the response of is retried with the same
// key after the API comes back; the retry is the same message, delivered and applied once.
func TestFaultAPIKilledBeforeResponse(t *testing.T) {
	s := setup(t)
	first := s.submit("api-kill-1", 202)
	compose(t, "kill", "api")
	compose(t, "start", "api")
	waitReady(t)
	again := s.submit("api-kill-1", 202, 200)
	require.Equal(t, first.MessageID, again.MessageID, "the same message")
	require.True(t, again.Duplicate, "recognised as the retry of an accepted submission")
	s.waitDelivered(first.MessageID, 60*time.Second)
	require.Eventually(t, func() bool {
		attempts, duplicates, applied := receiver(t).record(first.WebhookID)
		return applied && attempts == 1 && duplicates == 0
	}, 30*time.Second, time.Second)
	require.Equal(t, 1, receiver(t).Counter, "applied exactly once")
}

// TestFaultWorkerKilledAfterReceiverProcessed: the receiver takes four seconds to answer; the worker is killed
// while it waits, after the receiver has applied the change but before the outcome is recorded. The sweep resends
// once the lease expires: one duplicate at the receiver, the counter unchanged, the delivery succeeded.
func TestFaultWorkerKilledAfterReceiverProcessed(t *testing.T) {
	s := setup(t)
	control(t, map[string]string{"delay": "4s"})
	defer control(t, map[string]string{"delay": "0"})
	a := s.submit("worker-kill-1", 202)
	require.Eventually(t, func() bool {
		attempts, _, _ := receiver(t).record(a.WebhookID)
		return attempts == 1
	}, 30*time.Second, 200*time.Millisecond, "the receiver got the request and is waiting")
	compose(t, "kill", "delivery-worker")
	time.Sleep(6 * time.Second) // the receiver finishes its wait and applies the change
	_, _, applied := receiver(t).record(a.WebhookID)
	require.True(t, applied, "the receiver applied the change the worker never recorded")
	require.Equal(t, 1, receiver(t).Counter)
	control(t, map[string]string{"delay": "0"})
	compose(t, "start", "delivery-worker")
	m := s.waitDelivered(a.MessageID, 4*time.Minute) // lease TTL (60 s) + sweep interval (30 s) + the resend
	attempts, duplicates, applied := receiver(t).record(a.WebhookID)
	require.True(t, applied)
	require.Equal(t, 2, attempts, "the resend reached the receiver")
	require.Equal(t, 1, duplicates, "exactly one duplicate, acknowledged without re-applying")
	require.Equal(t, 1, receiver(t).Counter, "the business change ran once")
	require.GreaterOrEqual(t, m.Deliveries[0].AttemptCount, int32(2), "the sweep recorded the lost attempt and the resend")
}

// TestFaultQueueOutage: with the broker down, submissions are accepted and delivered (the claim timer needs no
// wake-up) and the state events wait in the outbox; once the broker is back they drain and the control plane's
// health projection catches up.
func TestFaultQueueOutage(t *testing.T) {
	s := setup(t)
	// Pause rather than stop: LocalStack keeps no state across a restart, so a stopped broker would come back
	// without its subscriptions, which is LocalStack's behaviour, not the outage under test. A paused broker
	// refuses every publish and keeps its topology, as SNS and SQS do through a real outage.
	compose(t, "pause", "localstack")
	t.Cleanup(func() {
		_, _ = exec.Command(composeCmd[0], append(composeCmd[1:], "unpause", "localstack")...).CombinedOutput()
	}) //nolint:gosec // the test drives its own stack
	outageStart := time.Now().UTC()
	const n = 5
	ids := make([]accepted, 0, n)
	for i := range n {
		ids = append(ids, s.submit(fmt.Sprintf("outage-%d", i), 202))
	}
	for _, a := range ids {
		s.waitDelivered(a.MessageID, 90*time.Second)
	}
	require.Equal(t, n, receiver(t).Counter, "every message delivered during the outage")
	lag, _ := strconv.Atoi(psql(t, "hookbeam_delivery", "SELECT count(*) FROM outbox WHERE published_at IS NULL"))
	require.Positive(t, lag, "state events waited in the outbox")

	compose(t, "unpause", "localstack")
	require.Eventually(t, func() bool {
		waiting, _ := strconv.Atoi(psql(t, "hookbeam_delivery", "SELECT count(*) FROM outbox WHERE published_at IS NULL"))
		return waiting == 0
	}, 2*time.Minute, 2*time.Second, "the outbox drained after the broker returned")
	require.Eventually(t, func() bool {
		row := psql(t, "hookbeam", fmt.Sprintf("SELECT consecutive_failures || ' ' || extract(epoch FROM observed_at)::bigint FROM endpoint_health WHERE endpoint_id = %d", s.endpoint))
		parts := strings.Fields(row)
		if len(parts) != 2 {
			return false
		}
		observed, err := strconv.ParseInt(parts[1], 10, 64)
		return err == nil && parts[0] == "0" && observed >= outageStart.Unix()-1
	}, 2*time.Minute, 2*time.Second, "the control plane's projection observed the deliveries made during the outage")
}

// TestFaultOlderFailureAfterNewerSuccess publishes a failure event dated an hour ago for an endpoint whose
// projection already saw a newer success; the projection must keep the success.
func TestFaultOlderFailureAfterNewerSuccess(t *testing.T) {
	s := setup(t)
	a := s.submit("order-1", 202)
	s.waitDelivered(a.MessageID, 60*time.Second)
	var before string
	require.Eventually(t, func() bool {
		before = psql(t, "hookbeam", fmt.Sprintf("SELECT consecutive_failures || ' ' || observed_at FROM endpoint_health WHERE endpoint_id = %d", s.endpoint))
		return strings.HasPrefix(before, "0 ")
	}, 60*time.Second, time.Second, "the projection saw the success")

	reason := "http_500"
	payload, err := json.Marshal(map[string]any{
		"workspace_id": "ws_forged", "application_id": s.application, "endpoint_id": s.endpoint, "message_id": a.MessageID, "delivery_id": 0,
		"attempt": 1, "state": "failed", "reason": reason, "status_code": 500, "occurred_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano),
	})
	require.NoError(t, err)
	envelope, err := json.Marshal(map[string]any{"id": time.Now().UnixNano(), "type": "delivery.failed", "occurred_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano), "payload": json.RawMessage(payload)})
	require.NoError(t, err)
	compose(t, "exec", "-T", "localstack", "awslocal", "sns", "publish", "--topic-arn", "arn:aws:sns:us-east-1:000000000000:hookbeam-events",
		"--message", string(envelope), "--message-attributes", `{"type":{"DataType":"String","StringValue":"delivery.failed"}}`)
	time.Sleep(10 * time.Second)
	after := psql(t, "hookbeam", fmt.Sprintf("SELECT consecutive_failures || ' ' || observed_at FROM endpoint_health WHERE endpoint_id = %d", s.endpoint))
	require.Equal(t, before, after, "an older failure never overwrites a newer success")
}
