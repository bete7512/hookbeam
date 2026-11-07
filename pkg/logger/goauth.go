package logger

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bete7512/goauth/pkg/types"
)

// GoAuth adapts a slog.Logger to goauth's logger interface.
// Fatalf logs at error level and never exits the process.
type GoAuth struct {
	log *slog.Logger
}

var _ types.Logger = GoAuth{}

// NewGoAuth wraps log for goauth.
func NewGoAuth(log *slog.Logger) GoAuth { return GoAuth{log: log} }

// Info logs at info level.
func (g GoAuth) Info(msg string, args ...interface{}) { g.log.Info(msg, args...) }

// Error logs at error level.
func (g GoAuth) Error(msg string, args ...interface{}) { g.log.Error(msg, args...) }

// Debug logs at debug level.
func (g GoAuth) Debug(msg string, args ...interface{}) { g.log.Debug(msg, args...) }

// Warn logs at warn level.
func (g GoAuth) Warn(msg string, args ...interface{}) { g.log.Warn(msg, args...) }

// Trace logs at debug level; slog has no trace level.
func (g GoAuth) Trace(msg string, args ...interface{}) { g.log.Debug(msg, args...) }

// Infof formats and logs at info level.
func (g GoAuth) Infof(format string, args ...interface{}) { g.log.Info(fmt.Sprintf(format, args...)) }

// Tracef formats and logs at debug level.
func (g GoAuth) Tracef(format string, args ...interface{}) { g.log.Debug(fmt.Sprintf(format, args...)) }

// Errorf formats and logs at error level.
func (g GoAuth) Errorf(format string, args ...interface{}) { g.log.Error(fmt.Sprintf(format, args...)) }

// Debugf formats and logs at debug level.
func (g GoAuth) Debugf(format string, args ...interface{}) { g.log.Debug(fmt.Sprintf(format, args...)) }

// Warnf formats and logs at warn level.
func (g GoAuth) Warnf(format string, args ...interface{}) { g.log.Warn(fmt.Sprintf(format, args...)) }

// Fatalf logs at error level with fatal=true; a library must not exit the process.
func (g GoAuth) Fatalf(format string, args ...interface{}) {
	g.log.Log(context.Background(), slog.LevelError, fmt.Sprintf(format, args...), "fatal", true)
}

// WithFields returns a logger that attaches fields to every line.
func (g GoAuth) WithFields(fields map[string]interface{}) types.Logger {
	args := make([]any, 0, 2*len(fields))
	for k, v := range fields {
		args = append(args, k, v)
	}
	return GoAuth{log: g.log.With(args...)}
}
