package logger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/logger"
)

func TestLogger(t *testing.T) {
	withID := logger.WithRequestID(context.Background(), "req-1")
	tests := []struct {
		name  string
		opts  logger.Options
		write func(l *slog.Logger)
		check func(t *testing.T, line map[string]any)
	}{
		{
			name:  "base attributes and utc timestamp",
			opts:  logger.Options{Service: "api", Version: "v1", Env: "dev"},
			write: func(l *slog.Logger) { l.InfoContext(context.Background(), "hi") },
			check: func(t *testing.T, line map[string]any) {
				assert.Equal(t, "api", line["service"])
				assert.Equal(t, "v1", line["version"])
				assert.Equal(t, "dev", line["env"])
				assert.True(t, strings.HasSuffix(line["ts"].(string), "Z"))
				assert.NotContains(t, line, "request_id")
				assert.NotContains(t, line, "source")
			},
		},
		{
			name:  "request id from context",
			write: func(l *slog.Logger) { l.InfoContext(withID, "hi") },
			check: func(t *testing.T, line map[string]any) { assert.Equal(t, "req-1", line["request_id"]) },
		},
		{
			name:  "request id survives With",
			write: func(l *slog.Logger) { l.With("k", "v").InfoContext(withID, "hi") },
			check: func(t *testing.T, line map[string]any) {
				assert.Equal(t, "req-1", line["request_id"])
				assert.Equal(t, "v", line["k"])
			},
		},
		{
			name:  "request id survives WithGroup",
			write: func(l *slog.Logger) { l.WithGroup("g").InfoContext(withID, "hi") },
			check: func(t *testing.T, line map[string]any) {
				group, ok := line["g"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "req-1", group["request_id"])
			},
		},
		{
			name:  "debug adds source",
			opts:  logger.Options{Level: "debug"},
			write: func(l *slog.Logger) { l.DebugContext(context.Background(), "hi") },
			check: func(t *testing.T, line map[string]any) { assert.Contains(t, line, "source") },
		},
		{
			name:  "invalid level falls back to info",
			opts:  logger.Options{Level: "loud"},
			write: func(l *slog.Logger) { l.InfoContext(context.Background(), "hi") },
			check: func(t *testing.T, line map[string]any) { assert.Equal(t, "INFO", line["level"]) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tt.opts.Writer = &buf
			tt.write(logger.New(tt.opts))

			var line map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &line), buf.String())
			tt.check(t, line)
		})
	}
}

func TestLevelFiltersRecords(t *testing.T) {
	var buf bytes.Buffer
	logger.New(logger.Options{Level: "warn", Writer: &buf}).InfoContext(context.Background(), "hidden")
	assert.Empty(t, buf.String())
}

func TestTextFormat(t *testing.T) {
	var buf bytes.Buffer
	l := logger.New(logger.Options{Format: "text", Writer: &buf})
	l.InfoContext(logger.WithRequestID(context.Background(), "req-1"), "hi")
	assert.Contains(t, buf.String(), "request_id=req-1")
	assert.False(t, strings.HasPrefix(buf.String(), "{"))
}

func TestRequestIDFrom(t *testing.T) {
	assert.Empty(t, logger.RequestIDFrom(context.Background()))
	assert.Equal(t, "req-1", logger.RequestIDFrom(logger.WithRequestID(context.Background(), "req-1")))
}
