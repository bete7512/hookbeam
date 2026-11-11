package identity

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bete7512/goauth/pkg/types"
)

// slogLogger adapts a slog.Logger to goauth's logger interface; Fatalf logs at error level and never exits.
type slogLogger struct {
	log *slog.Logger
}

var _ types.Logger = slogLogger{}

func (g slogLogger) Info(msg string, args ...interface{})  { g.log.Info(msg, args...) }
func (g slogLogger) Error(msg string, args ...interface{}) { g.log.Error(msg, args...) }
func (g slogLogger) Debug(msg string, args ...interface{}) { g.log.Debug(msg, args...) }
func (g slogLogger) Warn(msg string, args ...interface{})  { g.log.Warn(msg, args...) }
func (g slogLogger) Trace(msg string, args ...interface{}) { g.log.Debug(msg, args...) }

func (g slogLogger) Infof(format string, args ...interface{}) {
	g.log.Info(fmt.Sprintf(format, args...))
}
func (g slogLogger) Tracef(format string, args ...interface{}) {
	g.log.Debug(fmt.Sprintf(format, args...))
}
func (g slogLogger) Errorf(format string, args ...interface{}) {
	g.log.Error(fmt.Sprintf(format, args...))
}
func (g slogLogger) Debugf(format string, args ...interface{}) {
	g.log.Debug(fmt.Sprintf(format, args...))
}
func (g slogLogger) Warnf(format string, args ...interface{}) {
	g.log.Warn(fmt.Sprintf(format, args...))
}

func (g slogLogger) Fatalf(format string, args ...interface{}) {
	g.log.Log(context.Background(), slog.LevelError, fmt.Sprintf(format, args...), "fatal", true)
}

func (g slogLogger) WithFields(fields map[string]interface{}) types.Logger {
	args := make([]any, 0, 2*len(fields))
	for k, v := range fields {
		args = append(args, k, v)
	}
	return slogLogger{log: g.log.With(args...)}
}
