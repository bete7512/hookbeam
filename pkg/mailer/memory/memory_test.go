package memory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/mailer"
	"github.com/bete7512/hookbeam/pkg/mailer/memory"
)

func TestMailer(t *testing.T) {
	ctx := context.Background()
	first := mailer.Message{To: "a@example.com", Subject: "one", Text: "1"}
	second := mailer.Message{To: "b@example.com", Subject: "two", HTML: "<b>2</b>"}

	t.Run("records messages in order", func(t *testing.T) {
		m := memory.New()
		require.NoError(t, m.Send(ctx, first))
		require.NoError(t, m.Send(ctx, second))
		assert.Equal(t, []mailer.Message{first, second}, m.Messages())
	})

	t.Run("messages returns a copy", func(t *testing.T) {
		m := memory.New()
		require.NoError(t, m.Send(ctx, first))
		got := m.Messages()
		got[0].Subject = "changed"
		assert.Equal(t, "one", m.Messages()[0].Subject)
	})

	t.Run("fail returns the error", func(t *testing.T) {
		m := memory.New()
		want := errors.New("boom")
		m.Fail = want
		require.ErrorIs(t, m.Send(ctx, first), want)
		assert.Empty(t, m.Messages())
	})

	t.Run("reset clears", func(t *testing.T) {
		m := memory.New()
		require.NoError(t, m.Send(ctx, first))
		m.Reset()
		assert.Empty(t, m.Messages())
	})
}
