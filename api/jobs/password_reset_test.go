package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	apievents "github.com/bete7512/hookbeam/api/events"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/events"
)

func TestPasswordReset(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		setup    func(m *mocks)
		wantErr  error
		skipMail bool
	}{
		{
			name: "sends the reset link", payload: `{"user_id":1}`,
			setup: func(m *mocks) {
				m.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(verified(), nil)
				m.auth.EXPECT().IssueActionToken(gomock.Any(), int64(1), models.TokenPurposePasswordReset, int64(7)).Return("token", nil)
			},
		},
		{
			name: "obsolete request is acknowledged without mail", payload: `{"user_id":1}`, skipMail: true,
			setup: func(m *mocks) {
				m.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(verified(), nil)
				m.auth.EXPECT().IssueActionToken(gomock.Any(), int64(1), models.TokenPurposePasswordReset, int64(7)).Return("", nil)
			},
		},
		{
			name: "user deleted", payload: `{"user_id":1}`,
			setup: func(m *mocks) {
				m.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(nil, apperr.NewNotFound("user", apperr.ErrNotFound))
			},
			wantErr: apperr.ErrNotFound,
		},
		{
			name: "token failure is retried", payload: `{"user_id":1}`,
			setup: func(m *mocks) {
				m.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(verified(), nil)
				m.auth.EXPECT().IssueActionToken(gomock.Any(), int64(1), models.TokenPurposePasswordReset, int64(7)).Return("", errAny)
			},
			wantErr: errAny,
		},
		{name: "malformed payload", payload: `nope`, setup: func(*mocks) {}, wantErr: errAny},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j, m := newJobs(t)
			tt.setup(m)

			err := j.PasswordReset(context.Background(), events.Event{ID: 7, Type: apievents.PasswordResetRequested, Payload: json.RawMessage(tt.payload)})

			if tt.wantErr != nil {
				require.Error(t, err)
				if !errors.Is(tt.wantErr, errAny) {
					require.ErrorIs(t, err, tt.wantErr)
				}
				require.Empty(t, m.mail.Messages())
				return
			}
			require.NoError(t, err)
			if tt.skipMail {
				require.Empty(t, m.mail.Messages())
				return
			}
			require.Len(t, m.mail.Messages(), 1)
			assert.Equal(t, "ada@example.com", m.mail.Messages()[0].To)
			assert.Contains(t, m.mail.Messages()[0].Text, linkBaseURL+"/reset-password?token=token")
		})
	}
}
