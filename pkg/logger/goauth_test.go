package logger_test

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bete7512/hookbeam/pkg/logger"
)

func TestGoAuth(t *testing.T) {
	var buf bytes.Buffer
	l := logger.NewGoAuth(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	l.WithFields(map[string]interface{}{"module": "core"}).Infof("user %s", "ada")
	l.Fatalf("boom %d", 1)
	l.Trace("trace line")

	out := buf.String()
	assert.Contains(t, out, `"msg":"user ada"`)
	assert.Contains(t, out, `"module":"core"`)
	assert.Contains(t, out, `"msg":"boom 1"`)
	assert.Contains(t, out, `"fatal":true`)
	assert.Contains(t, out, `"level":"DEBUG","msg":"trace line"`)
}
