package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/bete7512/hookbeam/pkg/webhooks"
)

const tolerance = 5 * time.Minute

// wrongSecret is what the bad_signature scenario verifies with: a receiver configured with the wrong key.
const wrongSecret = "whsec_d3Jvbmcgc2VjcmV0IGZvciB0aGUgZGVtbw==" //nolint:gosec // a deliberately wrong demo key, not a credential

type receiver struct {
	store        *Store
	log          *slog.Logger
	timeoutDelay time.Duration
}

func newReceiver(store *Store, log *slog.Logger, timeoutDelay time.Duration) *receiver {
	return &receiver{store: store, log: log, timeoutDelay: timeoutDelay}
}

func (r *receiver) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /hooks", r.hooks)
	mux.HandleFunc("GET /{$}", r.page)
	mux.HandleFunc("POST /control", r.control)
	mux.HandleFunc("GET /state", r.state)
	return mux
}

// hooks is the webhook endpoint: verify, pick the scenario, record the outcome.
func (r *receiver) hooks(w http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(io.LimitReader(req.Body, 1<<20))
	if err != nil {
		http.Error(w, "unreadable body", http.StatusBadRequest)
		return
	}
	id := req.Header.Get(webhooks.HeaderID)
	now := time.Now()
	secret := r.store.Secret()
	if secret == "" {
		r.store.Touch(id, eventType(body), false, now)
		r.store.Outcome(id, http.StatusUnauthorized, false)
		r.log.Warn("rejected: no secret configured", "webhook_id", id)
		http.Error(w, "receiver has no secret yet: set the endpoint's signing secret on the receiver's control page", http.StatusUnauthorized)
		return
	}
	scenario := r.store.Scenario()
	if s := Scenario(req.URL.Query().Get("scenario")); validScenario(s) {
		scenario = s
	}
	if scenario == ScenarioBadSignature {
		secret = wrongSecret
	}
	if err := webhooks.Verify([]string{secret}, req.Header, body, now, tolerance); err != nil {
		r.store.Touch(id, eventType(body), false, now)
		r.store.Outcome(id, http.StatusUnauthorized, false)
		r.log.Warn("rejected", "webhook_id", id, "reason", err)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	rec := r.store.Touch(id, eventType(body), true, now)
	if d, err := time.ParseDuration(req.URL.Query().Get("delay")); err == nil && d > 0 {
		time.Sleep(d)
	}
	status := http.StatusNoContent
	switch {
	case scenario == ScenarioFail:
		status = http.StatusBadRequest
	case scenario == ScenarioFailTwice && rec.Attempts <= 2:
		status = http.StatusServiceUnavailable
	case scenario == ScenarioTimeout:
		time.Sleep(r.timeoutDelay)
	}
	if s, err := strconv.Atoi(req.URL.Query().Get("status")); err == nil && s >= 300 {
		status = s
	}
	if status >= 300 {
		r.store.Outcome(id, status, false)
		r.log.Info("verified, answering as told", "webhook_id", id, "status", status, "attempt", rec.Attempts)
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(status)
		return
	}
	applied, duplicate := r.store.Outcome(id, status, true)
	switch {
	case duplicate:
		r.log.Info("verified duplicate, acknowledged without re-applying", "webhook_id", id, "first_seen", rec.FirstSeen.Format(time.RFC3339))
	case applied:
		r.log.Info("verified and applied", "webhook_id", id, "timestamp", req.Header.Get(webhooks.HeaderTimestamp), "bytes", len(body), "body", string(body))
	}
	w.WriteHeader(status)
}

// control sets the scenario or the secret, or resets the records.
func (r *receiver) control(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	switch req.Form.Get("action") {
	case "reset":
		r.store.Reset()
	default:
		if s := Scenario(req.Form.Get("scenario")); s != "" {
			if !r.store.SetScenario(s) {
				http.Error(w, "unknown scenario", http.StatusBadRequest)
				return
			}
		}
		if secret := req.Form.Get("secret"); secret != "" {
			if _, err := webhooks.Key(secret); err != nil {
				http.Error(w, "the secret must be the whsec_ value the console showed", http.StatusBadRequest)
				return
			}
			r.store.SetSecret(secret)
		}
	}
	if req.Header.Get("Accept") == "application/json" {
		r.state(w, req)
		return
	}
	http.Redirect(w, req, "/", http.StatusSeeOther)
}

// state answers the store as JSON, for scripts and the fault suite; the secret is never included.
func (r *receiver) state(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"scenario":   r.store.Scenario(),
		"has_secret": r.store.Secret() != "",
		"counter":    r.store.Counter(),
		"records":    r.store.Records(),
	})
}

func (r *receiver) page(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pageTemplate.Execute(w, map[string]any{
		"Scenario":  r.store.Scenario(),
		"Scenarios": Scenarios,
		"HasSecret": r.store.Secret() != "",
		"Counter":   r.store.Counter(),
		"Records":   r.store.Records(),
	})
}

func eventType(body []byte) string {
	var env struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(body, &env)
	return env.Type
}

var pageTemplate = template.Must(template.New("page").Funcs(template.FuncMap{
	"when": func(t time.Time) string { return t.Format("15:04:05") },
	"yesno": func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	},
	"status": func(s int) string {
		if s == 0 {
			return "—"
		}
		return fmt.Sprint(s)
	},
}).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Hookbeam demo receiver</title>
<style>
  :root{color-scheme:dark light;font-family:ui-sans-serif,system-ui,sans-serif}
  body{margin:0;background:#0f1117;color:#e6e8ef;padding:32px;max-width:1100px}
  h1{font-size:20px;margin:0 0 4px}p.sub{color:#9aa3b2;margin:0 0 24px}
  section{border:1px solid #262b36;border-radius:12px;padding:20px;margin-bottom:20px;background:#151925}
  h2{font-size:14px;text-transform:uppercase;letter-spacing:.08em;color:#9aa3b2;margin:0 0 12px}
  label{display:block;padding:6px 0;cursor:pointer}label small{color:#9aa3b2;margin-left:6px}
  input[type=password]{background:#0f1117;border:1px solid #262b36;color:#e6e8ef;border-radius:8px;padding:8px 10px;width:420px;max-width:100%}
  button{background:linear-gradient(135deg,#7c6cff,#22d3ee);color:#fff;border:0;border-radius:8px;padding:8px 14px;font-weight:600;cursor:pointer}
  button.ghost{background:#262b36}
  table{width:100%;border-collapse:collapse;font-size:14px}th{text-align:left;color:#9aa3b2;font-weight:500;padding:6px 8px;border-bottom:1px solid #262b36}
  td{padding:6px 8px;border-bottom:1px solid #1d2230;font-variant-numeric:tabular-nums}code{font-family:ui-monospace,monospace;font-size:12px}
  .pill{display:inline-block;border-radius:999px;padding:2px 8px;font-size:12px}.ok{background:#10b98126;color:#6ee7b7}.bad{background:#ef444426;color:#fca5a5}.muted{color:#9aa3b2}
  .big{font-size:28px;font-weight:600}
</style></head><body>
<h1>Hookbeam demo receiver</h1>
<p class="sub">Verifies Standard Webhooks signatures, applies each message once, and answers what you choose.</p>
<section>
  <h2>Scenario</h2>
  <form method="post" action="/control">
    {{range .Scenarios}}<label><input type="radio" name="scenario" value="{{.Name}}" {{if eq .Name $.Scenario}}checked{{end}}> <code>{{.Name}}</code><small>{{.Label}}</small></label>{{end}}
    <p><button type="submit">Use this scenario</button> <span class="muted">An endpoint URL can also carry <code>?scenario=fail_twice</code>, <code>?status=503</code> or <code>?delay=15s</code>.</span></p>
  </form>
</section>
<section>
  <h2>Signing secret</h2>
  <form method="post" action="/control">
    <p>{{if .HasSecret}}<span class="pill ok">secret set</span>{{else}}<span class="pill bad">no secret: every delivery is answered 401</span>{{end}}</p>
    <input type="password" name="secret" placeholder="whsec_… from the endpoint page" autocomplete="off"> <button type="submit">Save secret</button>
  </form>
</section>
<section>
  <h2>Received</h2>
  <p><span class="big" data-testid="counter">{{.Counter}}</span> <span class="muted">business counter: one per message applied, never twice</span>
  <form method="post" action="/control" style="display:inline;margin-left:16px"><input type="hidden" name="action" value="reset"><button class="ghost" type="submit">Reset records</button></form></p>
  {{if .Records}}<table><thead><tr><th>webhook-id</th><th>type</th><th>attempts</th><th>verified</th><th>applied</th><th>duplicates</th><th>last status</th><th>last seen</th></tr></thead><tbody>
  {{range .Records}}<tr><td><code>{{.WebhookID}}</code></td><td>{{.Type}}</td><td>{{.Attempts}}</td><td>{{yesno .Verified}}</td><td>{{yesno .Applied}}</td><td>{{.Duplicates}}</td><td>{{status .LastStatus}}</td><td>{{when .LastSeen}}</td></tr>{{end}}
  </tbody></table>{{else}}<p class="muted">Nothing received yet. Send a test delivery from the console.</p>{{end}}
</section>
</body></html>`))
