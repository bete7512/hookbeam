package jobs_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	apievents "github.com/bete7512/hookbeam/api/events"
	"github.com/bete7512/hookbeam/api/jobs"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services/service_mocks"
	"github.com/bete7512/hookbeam/pkg/events"
	"github.com/bete7512/hookbeam/pkg/mailer/memory"
)

const linkBaseURL = "http://app.test"

// mocks are the service mocks behind a Jobs under test; mail records what handlers send.
type mocks struct {
	events *service_mocks.MockEventService
	users  *service_mocks.MockUserService
	auth   *service_mocks.MockAuthService
	mail   *memory.Mailer
}

// newJobs builds Jobs over mocks.
func newJobs(t *testing.T) (*jobs.Jobs, *mocks) {
	return newJobsWithTimeout(t, 0)
}

func newJobsWithTimeout(t *testing.T, timeout time.Duration) (*jobs.Jobs, *mocks) {
	t.Helper()
	ctrl := gomock.NewController(t)
	m := &mocks{
		events: service_mocks.NewMockEventService(ctrl),
		users:  service_mocks.NewMockUserService(ctrl),
		auth:   service_mocks.NewMockAuthService(ctrl),
		mail:   memory.New(),
	}
	j, err := jobs.New(jobs.Deps{Events: m.events, Users: m.users, Auth: m.auth, Mail: m.mail, LinkBaseURL: linkBaseURL, Logger: slog.New(slog.DiscardHandler), HandlerTimeout: timeout})
	require.NoError(t, err)
	return j, m
}

// verified is a user whose email is already confirmed, so Welcome sends no link.
func verified() *models.User {
	now := time.Now()
	return &models.User{ID: 1, Email: "ada@example.com", Name: "Ada", EmailVerifiedAt: &now}
}

func TestHandle(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name        string
		eventType   string
		fresh       bool
		userErr     error
		wantMails   int
		wantErr     error
		wantErrText string
	}{
		{name: "runs the handler for a new event", eventType: apievents.UserRegistered, fresh: true, wantMails: 1},
		{name: "skips the handler on a replay", eventType: apievents.UserRegistered, fresh: false},
		{name: "handler error is returned for redelivery", eventType: apievents.UserRegistered, fresh: true, userErr: boom, wantErr: boom},
		{name: "unknown type is retained without touching the service", eventType: "unknown", wantErrText: "no handler for event type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j, m := newJobs(t)
			if tt.eventType == apievents.UserRegistered {
				m.events.EXPECT().ProcessOnce(gomock.Any(), int64(7), gomock.Any()).DoAndReturn(
					func(ctx context.Context, _ int64, fn func(context.Context) error) (bool, error) {
						if !tt.fresh {
							return false, nil
						}
						if err := fn(ctx); err != nil {
							return false, err
						}
						return true, nil
					})
			}
			if tt.fresh {
				m.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(verified(), tt.userErr)
			}

			err := j.Handle(context.Background(), events.Event{ID: 7, Type: tt.eventType, Payload: []byte(`{"user_id":1}`)})

			require.Len(t, m.mail.Messages(), tt.wantMails)
			if tt.wantErrText != "" {
				require.ErrorContains(t, err, tt.wantErrText)
				return
			}
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestHandleInvalidEnvelope(t *testing.T) {
	for _, e := range []events.Event{
		{Type: apievents.UserRegistered},
		{ID: -1, Type: apievents.UserRegistered},
		{ID: 7},
	} {
		j, _ := newJobs(t)
		require.Error(t, j.Handle(context.Background(), e))
	}
}

func TestHandleTimeout(t *testing.T) {
	j, m := newJobsWithTimeout(t, 20*time.Millisecond)
	m.events.EXPECT().ProcessOnce(gomock.Any(), int64(7), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ int64, _ func(context.Context) error) (bool, error) {
			_, ok := ctx.Deadline()
			require.True(t, ok)
			<-ctx.Done()
			return false, ctx.Err()
		})
	err := j.Handle(context.Background(), events.Event{ID: 7, Type: apievents.UserRegistered})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
