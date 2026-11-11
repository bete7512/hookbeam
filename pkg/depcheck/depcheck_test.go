package depcheck_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/depcheck"
)

type limits struct{ Max int }

func (l limits) Valid() bool { return l.Max > 0 }

type retention struct{ Payload time.Duration }

type deps struct {
	Store    any
	Logger   *slog.Logger
	Handlers map[string]func()
	Name     string
	Batch    int
	Timeout  time.Duration
	Ratio    float64
	Limits   limits
	Window   retention
	Clock    func() time.Time `depcheck:"optional"`
	Metrics  *int             `depcheck:"optional"`
	Verbose  bool
	_        int
}

func complete() deps {
	return deps{Store: 1, Logger: slog.New(slog.DiscardHandler), Handlers: map[string]func(){}, Name: "n", Batch: 1, Timeout: time.Second, Ratio: 0.1, Limits: limits{Max: 1}, Window: retention{Payload: time.Hour}}
}

func TestRequired(t *testing.T) {
	tests := []struct {
		name    string
		deps    func(d *deps)
		wantErr string
	}{
		{name: "complete", deps: func(*deps) {}},
		{name: "optional and bool fields may be zero", deps: func(d *deps) { d.Clock = nil; d.Metrics = nil; d.Verbose = false }},
		{name: "nil interface", deps: func(d *deps) { d.Store = nil }, wantErr: "services: Store is required"},
		{name: "nil pointer", deps: func(d *deps) { d.Logger = nil }, wantErr: "services: Logger is required"},
		{name: "nil map", deps: func(d *deps) { d.Handlers = nil }, wantErr: "services: Handlers is required"},
		{name: "empty string", deps: func(d *deps) { d.Name = "" }, wantErr: "services: Name is required"},
		{name: "zero int", deps: func(d *deps) { d.Batch = 0 }, wantErr: "services: positive Batch is required"},
		{name: "negative duration", deps: func(d *deps) { d.Timeout = -time.Second }, wantErr: "services: positive Timeout is required"},
		{name: "zero float", deps: func(d *deps) { d.Ratio = 0 }, wantErr: "services: positive Ratio is required"},
		{name: "invalid struct with Valid", deps: func(d *deps) { d.Limits = limits{} }, wantErr: "services: valid Limits is required"},
		{name: "zero struct without Valid", deps: func(d *deps) { d.Window = retention{} }, wantErr: "services: Window is required"},
		{name: "every missing field is named", deps: func(d *deps) { d.Store = nil; d.Batch = 0; d.Logger = nil }, wantErr: "services: Store, Logger, positive Batch are required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := complete()
			tt.deps(&d)

			err := depcheck.Required("services", d)

			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRequiredAcceptsPointerAndRejectsOthers(t *testing.T) {
	d := complete()
	require.NoError(t, depcheck.Required("x", &d))
	var nilDeps *deps
	require.EqualError(t, depcheck.Required("x", nilDeps), "x: deps are required")
	require.EqualError(t, depcheck.Required("x", 42), "x: deps must be a struct, got int")
}
