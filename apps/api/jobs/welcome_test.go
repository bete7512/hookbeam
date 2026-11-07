package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	apievents "github.com/bete7512/scaffold/apps/api/events"
	"github.com/bete7512/scaffold/apps/api/models"
	"github.com/bete7512/scaffold/pkg/apperr"
	"github.com/bete7512/scaffold/pkg/events"
)

var errAny = errors.New("any error")

func TestWelcome(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		setup    func(m *mocks)
		wantErr  error
		wantLink bool
		skipMail bool
	}{
		{
			name: "unverified user gets the verification link", payload: `{"user_id":1}`,
			setup: func(m *mocks) {
				m.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(&models.User{ID: 1, Email: "ada@example.com", Name: "Ada"}, nil)
				m.auth.EXPECT().IssueActionToken(gomock.Any(), int64(1), models.TokenPurposeEmailVerification, int64(7)).Return("tok/en", nil)
			},
			wantLink: true,
		},
		{
			name: "obsolete request sends no expired link", payload: `{"user_id":1}`, skipMail: true,
			setup: func(m *mocks) {
				m.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(&models.User{ID: 1}, nil)
				m.auth.EXPECT().IssueActionToken(gomock.Any(), int64(1), models.TokenPurposeEmailVerification, int64(7)).Return("", nil)
			},
		},
		{
			name: "verified user gets no link", payload: `{"user_id":1}`,
			setup: func(m *mocks) {
				m.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(verified(), nil)
			},
		},
		{
			name: "user deleted before the job ran", payload: `{"user_id":1}`,
			setup: func(m *mocks) {
				m.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(nil, apperr.NewNotFound("user", apperr.ErrNotFound))
			},
			wantErr: apperr.ErrNotFound,
		},
		{
			name: "token failure is retried", payload: `{"user_id":1}`,
			setup: func(m *mocks) {
				m.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(&models.User{ID: 1, Email: "ada@example.com", Name: "Ada"}, nil)
				m.auth.EXPECT().IssueActionToken(gomock.Any(), int64(1), models.TokenPurposeEmailVerification, int64(7)).Return("", errAny)
			},
			wantErr: errAny,
		},
		{name: "malformed payload", payload: `nope`, setup: func(*mocks) {}, wantErr: errAny},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j, m := newJobs(t)
			tt.setup(m)

			err := j.Welcome(context.Background(), events.Event{ID: 7, Type: apievents.UserRegistered, Payload: json.RawMessage(tt.payload)})

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
			msg := m.mail.Messages()[0]
			assert.Equal(t, "ada@example.com", msg.To)
			assert.Contains(t, msg.Text, "Ada")
			if tt.wantLink {
				assert.Contains(t, msg.Text, linkBaseURL+"/verify-email?token=tok%2Fen")
			} else {
				assert.NotContains(t, msg.Text, "verify-email")
			}
		})
	}
}
