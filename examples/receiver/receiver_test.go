package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/webhooks"
)

const secret = "whsec_c2VjcmV0IGZvciB0aGUgcmVjZWl2ZXIgdGVzdHM="

func newTestReceiver(t *testing.T, path string) (*receiver, *httptest.Server) {
	t.Helper()
	store, err := openStore(path)
	require.NoError(t, err)
	store.SetSecret(secret)
	r := newReceiver(store, slog.New(slog.DiscardHandler), 50*time.Millisecond)
	srv := httptest.NewServer(r.routes())
	t.Cleanup(srv.Close)
	return r, srv
}

// signed posts one signed delivery and returns the status answered.
func signed(t *testing.T, srv *httptest.Server, id, query string, sec string) int {
	t.Helper()
	body := []byte(`{"type":"order.shipped","data":{"order_id":"817"}}`)
	now := time.Now()
	sig, err := webhooks.Sign(sec, id, now, body)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/hooks"+query, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set(webhooks.HeaderID, id)
	req.Header.Set(webhooks.HeaderTimestamp, strconv.FormatInt(now.Unix(), 10))
	req.Header.Set(webhooks.HeaderSignature, sig)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func TestScenarios(t *testing.T) {
	tests := []struct {
		name       string
		scenario   Scenario
		query      string
		sendSecret string
		statuses   []int
		counter    int
	}{
		{"ok applies once and acknowledges duplicates", ScenarioOK, "", secret, []int{204, 204, 204}, 1},
		{"fail twice then succeed", ScenarioFailTwice, "", secret, []int{503, 503, 204, 204}, 1},
		{"fail for good", ScenarioFail, "", secret, []int{400, 400}, 0},
		{"timeout still answers, late", ScenarioTimeout, "", secret, []int{204}, 1},
		{"bad signature rejects a correctly signed request", ScenarioBadSignature, "", secret, []int{401, 401}, 0},
		{"a wrong signature is always rejected", ScenarioOK, "", "whsec_d3JvbmcgZm9yIHRoZSB0ZXN0cyBvbmx5", []int{401}, 0},
		{"the query steers one endpoint over the page", ScenarioOK, "?scenario=fail_twice", secret, []int{503, 503, 204}, 1},
		{"status and delay keep working", ScenarioOK, "?status=503&delay=10ms", secret, []int{503}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, srv := newTestReceiver(t, "")
			require.True(t, r.store.SetScenario(tt.scenario))
			for i, want := range tt.statuses {
				require.Equal(t, want, signed(t, srv, "msg_1", tt.query, tt.sendSecret), "attempt %d", i+1)
			}
			require.Equal(t, tt.counter, r.store.Counter())
		})
	}
}

func TestStoreSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	r, srv := newTestReceiver(t, path)
	require.Equal(t, 204, signed(t, srv, "msg_a", "", secret))
	require.Equal(t, 1, r.store.Counter())
	srv.Close()

	again, err := openStore(path)
	require.NoError(t, err)
	require.Equal(t, 1, again.Counter(), "the counter is read back")
	require.Equal(t, secret, again.Secret())
	r2 := newReceiver(again, slog.New(slog.DiscardHandler), time.Millisecond)
	srv2 := httptest.NewServer(r2.routes())
	defer srv2.Close()
	require.Equal(t, 204, signed(t, srv2, "msg_a", "", secret), "a resend after the restart is a duplicate")
	require.Equal(t, 1, again.Counter(), "the business change ran once across the restart")
	rec := again.Records()[0]
	require.Equal(t, 1, rec.Duplicates)
	require.True(t, rec.Applied)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), `"counter": 1`)
}

func TestControlAndState(t *testing.T) {
	r, srv := newTestReceiver(t, "")
	r.store.SetSecret("")
	resp, err := http.PostForm(srv.URL+"/control", url.Values{"scenario": {"fail"}, "secret": {secret}})
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "the redirect to the page was followed")
	require.Equal(t, ScenarioFail, r.store.Scenario())
	require.Equal(t, secret, r.store.Secret())

	resp, err = http.PostForm(srv.URL+"/control", url.Values{"scenario": {"nope"}})
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	resp, err = http.PostForm(srv.URL+"/control", url.Values{"secret": {"not-a-secret"}})
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	resp, err = http.Get(srv.URL + "/state")
	require.NoError(t, err)
	defer resp.Body.Close()
	var st map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&st))
	require.Equal(t, "fail", st["scenario"])
	require.Equal(t, true, st["has_secret"])
	require.NotContains(t, st, "secret")

	page, err := http.Get(srv.URL + "/")
	require.NoError(t, err)
	defer page.Body.Close()
	html, _ := io.ReadAll(page.Body)
	require.Contains(t, string(html), "Hookbeam demo receiver")
	require.Contains(t, string(html), "secret set")
}
