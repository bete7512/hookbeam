package services_test

import (
	"context"
	"errors"
	"net/http"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/auth"
	"github.com/bete7512/hookbeam/pkg/oidc"
)

func (s *AuthServiceSuite) TestIssueActionToken() {
	tests := []struct {
		name       string
		requestID  int64
		mutate     func(*models.AuthToken)
		repoErr    error
		wantStatus int
		wantEmpty  bool
	}{
		{name: "stores only the hash", requestID: 7},
		{name: "invalid event id", requestID: 0, wantStatus: http.StatusInternalServerError, wantEmpty: true},
		{name: "newer request already issued", requestID: 7, mutate: func(t *models.AuthToken) { t.RequestID = 8 }, wantEmpty: true},
		{name: "used request is never resurrected", requestID: 7, mutate: func(t *models.AuthToken) { now := time.Now(); t.UsedAt = &now }, wantEmpty: true},
		{name: "expired request is never renewed", requestID: 7, mutate: func(t *models.AuthToken) { t.ExpiresAt = time.Now().Add(-time.Second) }, wantEmpty: true},
		{name: "old key does not replace the existing hash", requestID: 7, mutate: func(t *models.AuthToken) { t.TokenHash = []byte("old-key-hash") }, wantEmpty: true},
		{name: "repository failure", requestID: 7, repoErr: errors.New("down"), wantStatus: http.StatusInternalServerError, wantEmpty: true},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			var stored []byte
			if tt.requestID > 0 {
				s.authTokens.EXPECT().CreateAuthToken(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, token *models.AuthToken) error {
					s.Equal(int64(1), token.UserID)
					s.Equal(tt.requestID, token.RequestID)
					stored = token.TokenHash
					if tt.mutate != nil {
						tt.mutate(token)
					}
					return tt.repoErr
				})
			}
			raw, err := s.svc.IssueActionToken(context.Background(), 1, models.TokenPurposeEmailVerification, tt.requestID)
			s.requireStatus(err, tt.wantStatus)
			if tt.wantEmpty {
				s.Empty(raw)
			} else {
				s.NotEmpty(raw)
				s.Equal(stored, auth.HashToken(raw))
			}
		})
	}
}

func (s *AuthServiceSuite) TestVerifyEmail() {
	hash := auth.HashToken("raw")
	tests := []struct {
		name       string
		setup      func()
		wantStatus int
	}{
		{
			name: "valid token marks the email verified", setup: func() {
				s.authTokens.EXPECT().ConsumeAuthToken(gomock.Any(), hash, models.TokenPurposeEmailVerification).Return(&models.AuthToken{UserID: 1}, nil)
				s.users.EXPECT().MarkEmailVerified(gomock.Any(), int64(1)).Return(nil)
			},
		},
		{
			name: "unknown, used or expired token", wantStatus: http.StatusUnauthorized, setup: func() {
				s.authTokens.EXPECT().ConsumeAuthToken(gomock.Any(), hash, models.TokenPurposeEmailVerification).Return(nil, apperr.ErrNotFound)
			},
		},
		{
			name: "repo failure", wantStatus: http.StatusInternalServerError, setup: func() {
				s.authTokens.EXPECT().ConsumeAuthToken(gomock.Any(), hash, models.TokenPurposeEmailVerification).Return(nil, errors.New("down"))
			},
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			tt.setup()

			err := s.svc.VerifyEmail(context.Background(), "raw")

			s.requireStatus(err, tt.wantStatus)
		})
	}
}

func (s *AuthServiceSuite) TestRequestVerification() {
	now := time.Now()
	tests := []struct {
		name       string
		setup      func()
		wantStatus int
	}{
		{
			name: "unverified account queues the email", setup: func() {
				s.users.EXPECT().GetUserByEmail(gomock.Any(), "ada@example.com").Return(&models.User{ID: 1}, nil)
				s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Cond(func(e *models.OutboxEvent) bool {
					return e.Type == "user.verification_requested" && string(e.Payload) == `{"user_id":1}`
				})).Return(nil)
			},
		},
		{
			name: "verified account does nothing", setup: func() {
				s.users.EXPECT().GetUserByEmail(gomock.Any(), "ada@example.com").Return(&models.User{ID: 1, EmailVerifiedAt: &now}, nil)
			},
		},
		{
			name: "unknown email does nothing", setup: func() {
				s.users.EXPECT().GetUserByEmail(gomock.Any(), "ada@example.com").Return(nil, apperr.ErrNotFound)
			},
		},
		{
			name: "repo failure", wantStatus: http.StatusInternalServerError, setup: func() {
				s.users.EXPECT().GetUserByEmail(gomock.Any(), "ada@example.com").Return(nil, errors.New("down"))
			},
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			tt.setup()

			err := s.svc.RequestVerification(context.Background(), " Ada@Example.com ")

			s.requireStatus(err, tt.wantStatus)
		})
	}
}

func (s *AuthServiceSuite) TestForgotPassword() {
	tests := []struct {
		name       string
		setup      func()
		wantStatus int
	}{
		{
			name: "known email queues the reset", setup: func() {
				s.users.EXPECT().GetUserByEmail(gomock.Any(), "ada@example.com").Return(&models.User{ID: 1}, nil)
				s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Cond(func(e *models.OutboxEvent) bool {
					return e.Type == "password.reset_requested" && string(e.Payload) == `{"user_id":1}`
				})).Return(nil)
			},
		},
		{
			name: "unknown email does nothing", setup: func() {
				s.users.EXPECT().GetUserByEmail(gomock.Any(), "ada@example.com").Return(nil, apperr.ErrNotFound)
			},
		},
		{
			name: "outbox failure", wantStatus: http.StatusInternalServerError, setup: func() {
				s.users.EXPECT().GetUserByEmail(gomock.Any(), "ada@example.com").Return(&models.User{ID: 1}, nil)
				s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).Return(errors.New("down"))
			},
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			tt.setup()

			err := s.svc.ForgotPassword(context.Background(), "ada@example.com")

			s.requireStatus(err, tt.wantStatus)
		})
	}
}

func (s *AuthServiceSuite) TestResetPassword() {
	hash := auth.HashToken("raw")
	tests := []struct {
		name        string
		newPassword string
		setup       func()
		wantStatus  int
	}{
		{
			name: "valid token sets the password and ends sessions", newPassword: password, setup: func() {
				s.authTokens.EXPECT().ConsumeAuthToken(gomock.Any(), hash, models.TokenPurposePasswordReset).Return(&models.AuthToken{UserID: 1}, nil)
				s.users.EXPECT().UpdateUserPassword(gomock.Any(), int64(1), gomock.Cond(func(h string) bool {
					ok, err := s.hasher.Verify(password, h)
					return err == nil && ok
				})).Return(nil)
				s.sessions.EXPECT().RevokeUserSessions(gomock.Any(), int64(1)).Return(nil)
			},
		},
		{name: "weak password", newPassword: "short", wantStatus: http.StatusUnprocessableEntity, setup: func() {}},
		{
			name: "unknown, used or expired token", newPassword: password, wantStatus: http.StatusUnauthorized, setup: func() {
				s.authTokens.EXPECT().ConsumeAuthToken(gomock.Any(), hash, models.TokenPurposePasswordReset).Return(nil, apperr.ErrNotFound)
			},
		},
		{
			name: "repo failure", newPassword: password, wantStatus: http.StatusInternalServerError, setup: func() {
				s.authTokens.EXPECT().ConsumeAuthToken(gomock.Any(), hash, models.TokenPurposePasswordReset).Return(&models.AuthToken{UserID: 1}, nil)
				s.users.EXPECT().UpdateUserPassword(gomock.Any(), int64(1), gomock.Any()).Return(errors.New("down"))
			},
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			tt.setup()

			err := s.svc.ResetPassword(context.Background(), "raw", tt.newPassword)

			s.requireStatus(err, tt.wantStatus)
		})
	}
}

func (s *AuthServiceSuite) TestLoginWithIdentity() {
	now := time.Now()
	identity := oidc.Identity{Subject: "sub-1", Email: " Ada@Example.com ", EmailVerified: true, HostedDomain: "example.com", Name: "Ada"}
	linked := &models.UserIdentity{UserID: 1, Provider: "google", Subject: "sub-1"}
	expectLink := func() {
		s.identities.EXPECT().CreateUserIdentity(gomock.Any(), gomock.Cond(func(id *models.UserIdentity) bool {
			return id.UserID == 1 && id.Provider == "google" && id.Subject == "sub-1" && id.Email == "ada@example.com"
		})).Return(nil)
	}
	tests := []struct {
		name       string
		identity   oidc.Identity
		setup      func()
		wantStatus int
	}{
		{
			name: "known identity logs in", identity: identity, setup: func() {
				s.identities.EXPECT().GetUserIdentity(gomock.Any(), "google", "sub-1").Return(linked, nil)
				s.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(&models.User{ID: 1}, nil)
				s.sessions.EXPECT().CreateSession(gomock.Any(), gomock.Any()).Return(nil)
			},
		},
		{
			name: "verified account with the same email is linked", identity: identity, setup: func() {
				s.identities.EXPECT().GetUserIdentity(gomock.Any(), "google", "sub-1").Return(nil, apperr.ErrNotFound)
				s.users.EXPECT().GetUserByEmail(gomock.Any(), "ada@example.com").Return(&models.User{ID: 1, EmailVerifiedAt: &now}, nil)
				expectLink()
				s.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(&models.User{ID: 1}, nil)
				s.sessions.EXPECT().CreateSession(gomock.Any(), gomock.Any()).Return(nil)
			},
		},
		{
			name: "unverified account loses its password before linking", identity: identity, setup: func() {
				s.identities.EXPECT().GetUserIdentity(gomock.Any(), "google", "sub-1").Return(nil, apperr.ErrNotFound)
				s.users.EXPECT().GetUserByEmail(gomock.Any(), "ada@example.com").Return(&models.User{ID: 1}, nil)
				s.users.EXPECT().ClearUserPassword(gomock.Any(), int64(1)).Return(nil)
				s.sessions.EXPECT().RevokeUserSessions(gomock.Any(), int64(1)).Return(nil)
				s.users.EXPECT().MarkEmailVerified(gomock.Any(), int64(1)).Return(nil)
				expectLink()
				s.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(&models.User{ID: 1}, nil)
				s.sessions.EXPECT().CreateSession(gomock.Any(), gomock.Any()).Return(nil)
			},
		},
		{
			name: "unknown email creates a verified account without a password", identity: identity, setup: func() {
				s.identities.EXPECT().GetUserIdentity(gomock.Any(), "google", "sub-1").Return(nil, apperr.ErrNotFound)
				s.users.EXPECT().GetUserByEmail(gomock.Any(), "ada@example.com").Return(nil, apperr.ErrNotFound)
				s.users.EXPECT().CreateUser(gomock.Any(), gomock.Cond(func(u *models.User) bool {
					return u.Email == "ada@example.com" && u.Name == "Ada" && u.PasswordHash == nil
				})).DoAndReturn(func(_ context.Context, u *models.User) error { u.ID = 1; return nil })
				s.users.EXPECT().MarkEmailVerified(gomock.Any(), int64(1)).Return(nil)
				expectLink()
				s.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(&models.User{ID: 1}, nil)
				s.sessions.EXPECT().CreateSession(gomock.Any(), gomock.Any()).Return(nil)
			},
		},
		{
			name: "provider did not verify the email", identity: oidc.Identity{Subject: "sub-1", Email: "ada@example.com"}, wantStatus: http.StatusUnauthorized, setup: func() {
				s.identities.EXPECT().GetUserIdentity(gomock.Any(), "google", "sub-1").Return(nil, apperr.ErrNotFound)
			},
		},
		{
			name: "third-party verified email cannot claim an account", identity: oidc.Identity{Subject: "sub-1", Email: "ada@example.com", EmailVerified: true}, wantStatus: http.StatusUnauthorized, setup: func() {
				s.identities.EXPECT().GetUserIdentity(gomock.Any(), "google", "sub-1").Return(nil, apperr.ErrNotFound)
			},
		},
		{
			name: "existing subject remains authoritative after email changes", identity: oidc.Identity{Subject: "sub-1", Email: "new@example.net"}, setup: func() {
				s.identities.EXPECT().GetUserIdentity(gomock.Any(), "google", "sub-1").Return(linked, nil)
				s.users.EXPECT().GetUser(gomock.Any(), int64(1)).Return(&models.User{ID: 1}, nil)
				s.sessions.EXPECT().CreateSession(gomock.Any(), gomock.Any()).Return(nil)
			},
		},
		{
			name: "repo failure", identity: identity, wantStatus: http.StatusInternalServerError, setup: func() {
				s.identities.EXPECT().GetUserIdentity(gomock.Any(), "google", "sub-1").Return(nil, errors.New("down"))
			},
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			tt.setup()

			pair, err := s.svc.LoginWithIdentity(context.Background(), "google", tt.identity)

			s.requireStatus(err, tt.wantStatus)
			if tt.wantStatus == 0 {
				s.NotEmpty(pair.AccessToken)
			}
		})
	}
}

func (s *AuthServiceSuite) TestRecoveryCooldown() {
	for _, operation := range []string{"reset", "verification"} {
		s.Run(operation, func() {
			s.users.EXPECT().GetUserByEmail(gomock.Any(), "ada@example.com").Return(&models.User{ID: 1}, nil).Times(1)
			s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			request := s.svc.ForgotPassword
			if operation == "verification" {
				request = s.svc.RequestVerification
			}
			for range 3 {
				s.Require().NoError(request(context.Background(), " Ada@Example.com "))
			}
		})
	}
}
