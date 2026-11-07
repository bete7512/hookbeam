// Package memory is a Mailer that records messages for tests and local runs.
package memory

import (
	"context"
	"slices"
	"sync"

	"github.com/bete7512/hookbeam/pkg/mailer"
)

// Mailer records every valid message it is asked to send.
type Mailer struct {
	// Fail, when set, is returned by Send after validation.
	Fail error

	mu   sync.Mutex
	msgs []mailer.Message
}

// New returns an empty Mailer.
func New() *Mailer { return &Mailer{} }

// Send validates m and records it.
func (m *Mailer) Send(_ context.Context, msg mailer.Message) error {
	if err := msg.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail != nil {
		return m.Fail
	}
	m.msgs = append(m.msgs, msg)
	return nil
}

// Messages returns a copy of the recorded messages in send order.
func (m *Mailer) Messages() []mailer.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.msgs)
}

// Reset drops the recorded messages.
func (m *Mailer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = nil
}
