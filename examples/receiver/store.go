package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Scenario names what the receiver answers.
type Scenario string

// The scenarios the control page offers.
const (
	ScenarioOK           Scenario = "ok"            // 204, apply once
	ScenarioFailTwice    Scenario = "fail_twice"    // 503 to the first two attempts of a message, then 204
	ScenarioFail         Scenario = "fail"          // 400 for good
	ScenarioTimeout      Scenario = "timeout"       // sleep past the sender's timeout
	ScenarioBadSignature Scenario = "bad_signature" // verify against a wrong secret: 401
)

// Scenarios lists them in display order with what each shows.
var Scenarios = []struct {
	Name  Scenario
	Label string
}{
	{ScenarioOK, "answer 204 and apply the change once"},
	{ScenarioFailTwice, "fail the first two attempts with 503, then 204: shows the retry schedule"},
	{ScenarioFail, "answer 400 for good: a failed delivery you can replay"},
	{ScenarioTimeout, "answer after the sender gave up: a timeout, retried"},
	{ScenarioBadSignature, "verify with a wrong secret and reject: 401"},
}

func validScenario(s Scenario) bool {
	for _, x := range Scenarios {
		if x.Name == s {
			return true
		}
	}
	return false
}

// Record is what the receiver remembers about one webhook-id.
type Record struct {
	WebhookID  string    `json:"webhook_id"`
	Type       string    `json:"type"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
	Attempts   int       `json:"attempts"`   // requests received, verified or not
	Duplicates int       `json:"duplicates"` // verified requests after the change was applied
	Verified   bool      `json:"verified"`   // the last request carried a valid signature
	Applied    bool      `json:"applied"`    // the business change ran (exactly once)
	LastStatus int       `json:"last_status"`
}

// state is the persisted shape.
type state struct {
	Secret   string             `json:"secret"` //nolint:gosec // the receiver's own state file keeps its signing secret on purpose
	Scenario Scenario           `json:"scenario"`
	Delay    time.Duration      `json:"delay"`   // how long every verified request waits before it is answered
	Counter  int                `json:"counter"` // the sample business change: incremented once per applied message
	Records  map[string]*Record `json:"records"`
}

// Store keeps the receiver's state in memory and, when a path is set, in a JSON file written on every change,
// so a demonstration survives a restart of the receiver.
type Store struct {
	mu   sync.Mutex
	path string
	s    state
}

func openStore(path string) (*Store, error) {
	st := &Store{path: path, s: state{Scenario: ScenarioOK, Records: map[string]*Record{}}}
	if path == "" {
		return st, nil
	}
	data, err := os.ReadFile(path) //nolint:gosec // the operator names the file
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &st.s); err != nil {
		return nil, err
	}
	if st.s.Records == nil {
		st.s.Records = map[string]*Record{}
	}
	if !validScenario(st.s.Scenario) {
		st.s.Scenario = ScenarioOK
	}
	return st, nil
}

// Path is where the state is written, or empty.
func (st *Store) Path() string { return st.path }

func (st *Store) Secret() string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.Secret
}

func (st *Store) SetSecret(secret string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.Secret = secret
	st.flush()
}

func (st *Store) Scenario() Scenario {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.Scenario
}

func (st *Store) SetScenario(s Scenario) bool {
	if !validScenario(s) {
		return false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.Scenario = s
	st.flush()
	return true
}

// Delay is how long every verified request waits before the answer; the fault suite uses it to land a crash
// between the receiver applying a message and the worker recording the outcome.
func (st *Store) Delay() time.Duration {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.Delay
}

func (st *Store) SetDelay(d time.Duration) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.Delay = d
	st.flush()
}

// Counter is the sample business change's value: how many distinct messages were applied.
func (st *Store) Counter() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.Counter
}

// Reset forgets every record and the counter; the secret and scenario stay.
func (st *Store) Reset() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.Records = map[string]*Record{}
	st.s.Counter = 0
	st.flush()
}

// Records returns every record, newest first.
func (st *Store) Records() []Record {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]Record, 0, len(st.s.Records))
	for _, r := range st.s.Records {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out
}

// Touch records one more request for the id and returns the record as it is now (before any outcome).
func (st *Store) Touch(id, eventType string, verified bool, now time.Time) Record {
	st.mu.Lock()
	defer st.mu.Unlock()
	r := st.s.Records[id]
	if r == nil {
		r = &Record{WebhookID: id, FirstSeen: now}
		st.s.Records[id] = r
	}
	r.Attempts++
	r.LastSeen = now
	r.Verified = verified
	if eventType != "" {
		r.Type = eventType
	}
	st.flush()
	return *r
}

// Outcome records what the request was answered and, when applied, runs the business change exactly once:
// a verified message already applied counts as a duplicate and leaves the counter alone.
func (st *Store) Outcome(id string, status int, apply bool) (applied, duplicate bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	r := st.s.Records[id]
	if r == nil {
		return false, false
	}
	r.LastStatus = status
	if apply {
		if r.Applied {
			r.Duplicates++
			duplicate = true
		} else {
			r.Applied = true
			st.s.Counter++
			applied = true
		}
	}
	st.flush()
	return applied, duplicate
}

// flush writes the state to the file through a rename, so a crash never leaves a half-written file. Callers hold mu.
func (st *Store) flush() {
	if st.path == "" {
		return
	}
	data, err := json.MarshalIndent(st.s, "", "  ") //nolint:gosec // the receiver's own state file keeps its secret on purpose
	if err != nil {
		return
	}
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil { //nolint:gosec // the operator names the file
		return
	}
	_ = os.Rename(tmp, filepath.Clean(st.path)) //nolint:gosec // the operator names the file
}
