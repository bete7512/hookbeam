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

func TestVerification(t *testing.T) {
	tests := []struct {
		name      string
		payload   string
		setup     func(m *mocks)
		wantErr   error
		wantMails int
	}{
		{
			name: "sends the link", payload: `{"user_id":1}`,
			setup: func(m *mocks) {
				m.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(&models.User{ID: 1, Email: "ada@example.com", Name: "Ada"}, nil)
				m.auth.EXPECT().IssueActionToken(gomock.Any(), int64(1), models.TokenPurposeEmailVerification, int64(7)).Return("token", nil)
			},
			wantMails: 1,
		},
		{
			name: "obsolete request sends nothing", payload: `{"user_id":1}`,
			setup: func(m *mocks) {
				m.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(&models.User{ID: 1}, nil)
				m.auth.EXPECT().IssueActionToken(gomock.Any(), int64(1), models.TokenPurposeEmailVerification, int64(7)).Return("", nil)
			},
		},
		{
			name: "already verified sends nothing", payload: `{"user_id":1}`,
			setup: func(m *mocks) {
				m.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(verified(), nil)
			},
		},
		{
			name: "user deleted", payload: `{"user_id":1}`,
			setup: func(m *mocks) {
				m.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(nil, apperr.NewNotFound("user", apperr.ErrNotFound))
			},
			wantErr: apperr.ErrNotFound,
		},
		{name: "malformed payload", payload: `nope`, setup: func(*mocks) {}, wantErr: errAny},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j, m := newJobs(t)
			tt.setup(m)

			err := j.Verification(context.Background(), events.Event{ID: 7, Type: apievents.UserVerificationRequested, Payload: json.RawMessage(tt.payload)})

			if tt.wantErr != nil {
				require.Error(t, err)
				if !errors.Is(tt.wantErr, errAny) {
					require.ErrorIs(t, err, tt.wantErr)
				}
				return
			}
			require.NoError(t, err)
			require.Len(t, m.mail.Messages(), tt.wantMails)
			if tt.wantMails > 0 {
				assert.Equal(t, "ada@example.com", m.mail.Messages()[0].To)
				assert.Contains(t, m.mail.Messages()[0].Text, linkBaseURL+"/verify-email?token=token")
			}
		})
	}
}
