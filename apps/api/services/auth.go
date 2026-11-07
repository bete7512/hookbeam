package services

//go:generate mockgen -source=auth.go -destination=service_mocks/mock_auth.go -package=service_mocks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	apievents "github.com/bete7512/scaffold/apps/api/events"
	"github.com/bete7512/scaffold/apps/api/models"
	"github.com/bete7512/scaffold/apps/api/repos"
	"github.com/bete7512/scaffold/pkg/apperr"
	"github.com/bete7512/scaffold/pkg/auth"
	"github.com/bete7512/scaffold/pkg/db"
	"github.com/bete7512/scaffold/pkg/oidc"
	"github.com/bete7512/scaffold/pkg/ratelimit"
)

// AuthService logs users in and out, verifies tokens, and runs the email verification and password reset flows.
type AuthService interface {
	Login(ctx context.Context, email, password string) (*models.TokenPair, error)
	Refresh(ctx context.Context, refreshToken string) (*models.TokenPair, error)
	Logout(ctx context.Context, refreshToken string) error
	LogoutAll(ctx context.Context, userID int64) error
	Authenticate(ctx context.Context, accessToken string) (auth.Claims, error)
	// IssueActionToken returns the stable token for an event, or empty when its request is obsolete, used or expired.
	IssueActionToken(ctx context.Context, userID int64, purpose string, requestID int64) (string, error)
	VerifyEmail(ctx context.Context, token string) error
	// RequestVerification queues a verification email when the address belongs to an unverified account; otherwise it does nothing.
	RequestVerification(ctx context.Context, email string) error
	// ForgotPassword queues a reset email when the address belongs to an account; otherwise it does nothing.
	ForgotPassword(ctx context.Context, email string) error
	ResetPassword(ctx context.Context, token, newPassword string) error
	// LoginWithIdentity starts a session for the account linked to an OIDC identity, linking or creating it first.
	LoginWithIdentity(ctx context.Context, provider string, identity oidc.Identity) (*models.TokenPair, error)
}

// AuthServiceDeps are the AuthService dependencies; all are required.
type AuthServiceDeps struct {
	Users             repos.UserRepo
	Sessions          repos.SessionRepo
	AuthTokens        repos.AuthTokenRepo
	Identities        repos.UserIdentityRepo
	Outbox            repos.OutboxRepo
	Tx                db.Transactor
	Hasher            auth.PasswordHasher
	Tokens            *auth.Tokens
	Limiter           ratelimit.Limiter
	LoginLimit        int
	LoginWindow       time.Duration
	RefreshTokenTTL   time.Duration
	ActionTokenTTL    time.Duration
	ActionTokenKey    []byte
	RecipientCooldown time.Duration
	Logger            *slog.Logger
}

type authService struct {
	deps AuthServiceDeps
}

// NewAuthService returns an AuthService or an error if a dependency is missing.
func NewAuthService(deps AuthServiceDeps) (AuthService, error) {
	if deps.Users == nil || deps.Sessions == nil || deps.AuthTokens == nil || deps.Identities == nil || deps.Outbox == nil || deps.Tx == nil ||
		deps.Hasher == nil || deps.Tokens == nil || deps.Limiter == nil || deps.Logger == nil ||
		deps.LoginLimit <= 0 || deps.LoginWindow <= 0 || deps.RefreshTokenTTL <= 0 || deps.ActionTokenTTL <= 0 || len(deps.ActionTokenKey) != 32 || deps.RecipientCooldown <= 0 {
		return nil, errors.New("services: every AuthServiceDeps field is required")
	}
	deps.ActionTokenKey = bytes.Clone(deps.ActionTokenKey)
	return &authService{deps: deps}, nil
}

func (s *authService) Login(ctx context.Context, email, password string) (*models.TokenPair, error) {
	email = normalizeEmail(email)
	decision, err := s.deps.Limiter.Allow(ctx, "login:"+email, s.deps.LoginLimit, s.deps.LoginWindow)
	if err != nil {
		return nil, limiterError(err)
	}
	if !decision.Allowed {
		return nil, apperr.NewTooManyRequests(decision.RetryAfter)
	}

	user, err := s.deps.Users.GetUserByEmail(ctx, email)
	if errors.Is(err, apperr.ErrNotFound) {
		// Do the same argon2 work as a real check so response time does not reveal which emails exist.
		_, _ = s.deps.Hasher.Hash(password)
		return nil, invalidCredentials()
	}
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	if !user.HasPassword() {
		_, _ = s.deps.Hasher.Hash(password)
		return nil, invalidCredentials()
	}
	ok, err := s.deps.Hasher.Verify(password, *user.PasswordHash)
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	if !ok {
		return nil, invalidCredentials()
	}
	return s.startSession(ctx, user)
}

// startSession creates a session row and returns its tokens.
func (s *authService) startSession(ctx context.Context, user *models.User) (*models.TokenPair, error) {
	refreshToken, hash, err := auth.NewToken()
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	session := &models.Session{UserID: user.ID, TokenVersion: user.TokenVersion, RefreshTokenHash: hash, ExpiresAt: time.Now().Add(s.deps.RefreshTokenTTL)}
	if err := s.deps.Sessions.CreateSession(ctx, session); err != nil {
		if errors.Is(err, apperr.ErrNotFound) {
			return nil, invalidCredentials()
		}
		return nil, apperr.NewInternal(err)
	}
	return s.tokenPair(user, refreshToken)
}

func (s *authService) Refresh(ctx context.Context, refreshToken string) (*models.TokenPair, error) {
	hash := auth.HashToken(refreshToken)
	session, err := s.deps.Sessions.FindSessionByToken(ctx, hash)
	if errors.Is(err, apperr.ErrNotFound) {
		return nil, invalidToken(err)
	}
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	if session.RevokedAt != nil {
		return nil, invalidToken(nil)
	}
	if !bytes.Equal(session.RefreshTokenHash, hash) {
		// The token matched the previous hash: it was already used once.
		return nil, s.revokeReused(ctx, session.ID)
	}
	if time.Now().After(session.ExpiresAt) {
		return nil, invalidToken(nil)
	}

	user, err := s.deps.Users.GetUser(ctx, session.UserID)
	if errors.Is(err, apperr.ErrNotFound) {
		return nil, invalidToken(err)
	}
	if err != nil {
		return nil, apperr.NewInternal(err)
	}

	if session.TokenVersion < 0 || session.TokenVersion != user.TokenVersion {
		return nil, invalidToken(errors.New("session token version is stale"))
	}

	next, nextHash, err := auth.NewToken()
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	session.RefreshTokenHash = nextHash
	session.ExpiresAt = time.Now().Add(s.deps.RefreshTokenTTL)
	err = s.deps.Sessions.RotateRefreshToken(ctx, session, hash)
	if errors.Is(err, apperr.ErrNotFound) {
		return nil, s.revokeReused(ctx, session.ID)
	}
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	return s.tokenPair(user, next)
}

func (s *authService) Logout(ctx context.Context, refreshToken string) error {
	session, err := s.deps.Sessions.FindSessionByToken(ctx, auth.HashToken(refreshToken))
	if errors.Is(err, apperr.ErrNotFound) {
		return nil
	}
	if err != nil {
		return apperr.NewInternal(err)
	}
	if err := s.deps.Sessions.RevokeSession(ctx, session.ID); err != nil {
		return apperr.NewInternal(err)
	}
	return nil
}

func (s *authService) LogoutAll(ctx context.Context, userID int64) error {
	err := s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.deps.Users.WithTx(tx).BumpUserTokenVersion(ctx, userID); err != nil {
			return err
		}
		return s.deps.Sessions.WithTx(tx).RevokeUserSessions(ctx, userID)
	})
	if err != nil {
		return userError(err)
	}
	return nil
}

func (s *authService) Authenticate(ctx context.Context, accessToken string) (auth.Claims, error) {
	claims, err := s.deps.Tokens.Parse(accessToken)
	if err != nil {
		return auth.Claims{}, invalidToken(err)
	}
	user, err := s.deps.Users.GetUser(ctx, claims.UserID)
	if errors.Is(err, apperr.ErrNotFound) {
		return auth.Claims{}, invalidToken(err)
	}
	if err != nil {
		return auth.Claims{}, apperr.NewInternal(err)
	}
	if user.TokenVersion != claims.TokenVersion {
		return auth.Claims{}, invalidToken(errors.New("token version is stale"))
	}
	return claims, nil
}

// revokeReused ends a session whose refresh token was used twice, a sign it was stolen.
func (s *authService) revokeReused(ctx context.Context, sessionID int64) error {
	if err := s.deps.Sessions.RevokeSession(ctx, sessionID); err != nil {
		return apperr.NewInternal(err)
	}
	return invalidToken(errors.New("refresh token reused"))
}

func (s *authService) tokenPair(user *models.User, refreshToken string) (*models.TokenPair, error) {
	access, expiresAt, err := s.deps.Tokens.Issue(user.ID, user.TokenVersion)
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	return &models.TokenPair{AccessToken: access, AccessTokenExpiresAt: expiresAt, RefreshToken: refreshToken}, nil
}

func (s *authService) IssueActionToken(ctx context.Context, userID int64, purpose string, requestID int64) (string, error) {
	if userID <= 0 || requestID <= 0 || (purpose != models.TokenPurposeEmailVerification && purpose != models.TokenPurposePasswordReset) {
		return "", apperr.NewInternal(errors.New("invalid action token request"))
	}
	mac := hmac.New(sha256.New, s.deps.ActionTokenKey)
	_, _ = fmt.Fprintf(mac, "scaffold/action-token/v1:%d:%s:%d", userID, purpose, requestID)
	raw := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	token := &models.AuthToken{UserID: userID, RequestID: requestID, Purpose: purpose,
		TokenHash: auth.HashToken(raw), ExpiresAt: time.Now().Add(s.deps.ActionTokenTTL)}
	if err := s.deps.AuthTokens.CreateAuthToken(ctx, token); err != nil {
		return "", apperr.NewInternal(err)
	}
	if token.RequestID != requestID || token.UsedAt != nil || !token.ExpiresAt.After(time.Now()) || !bytes.Equal(token.TokenHash, auth.HashToken(raw)) {
		return "", nil
	}
	return raw, nil
}

func (s *authService) VerifyEmail(ctx context.Context, token string) error {
	err := s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		t, err := s.deps.AuthTokens.WithTx(tx).ConsumeAuthToken(ctx, auth.HashToken(token), models.TokenPurposeEmailVerification)
		if err != nil {
			return err
		}
		return s.deps.Users.WithTx(tx).MarkEmailVerified(ctx, t.UserID)
	})
	if errors.Is(err, apperr.ErrNotFound) {
		return invalidToken(err)
	}
	if err != nil {
		return apperr.NewInternal(err)
	}
	return nil
}

func (s *authService) RequestVerification(ctx context.Context, email string) error {
	email = normalizeEmail(email)
	allowed, err := s.allowRecovery(ctx, "verification:", email)
	if err != nil || !allowed {
		return err
	}
	user, err := s.deps.Users.GetUserByEmail(ctx, normalizeEmail(email))
	if errors.Is(err, apperr.ErrNotFound) || (err == nil && user.EmailVerifiedAt != nil) {
		return nil
	}
	if err != nil {
		return apperr.NewInternal(err)
	}
	return s.publish(ctx, apievents.UserVerificationRequested, apievents.UserVerificationRequestedPayload{UserID: user.ID})
}

func (s *authService) ForgotPassword(ctx context.Context, email string) error {
	email = normalizeEmail(email)
	allowed, err := s.allowRecovery(ctx, "reset:", email)
	if err != nil || !allowed {
		return err
	}
	user, err := s.deps.Users.GetUserByEmail(ctx, normalizeEmail(email))
	if errors.Is(err, apperr.ErrNotFound) {
		return nil
	}
	if err != nil {
		return apperr.NewInternal(err)
	}
	return s.publish(ctx, apievents.PasswordResetRequested, apievents.PasswordResetRequestedPayload{UserID: user.ID})
}

func (s *authService) ResetPassword(ctx context.Context, token, newPassword string) error {
	if msg := validatePassword(newPassword); msg != "" {
		return apperr.NewValidation(map[string]string{"newPassword": msg})
	}
	hash, err := s.deps.Hasher.Hash(newPassword)
	if err != nil {
		return apperr.NewInternal(err)
	}
	err = s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		t, err := s.deps.AuthTokens.WithTx(tx).ConsumeAuthToken(ctx, auth.HashToken(token), models.TokenPurposePasswordReset)
		if err != nil {
			return err
		}
		if err := s.deps.Users.WithTx(tx).UpdateUserPassword(ctx, t.UserID, hash); err != nil {
			return err
		}
		return s.deps.Sessions.WithTx(tx).RevokeUserSessions(ctx, t.UserID)
	})
	if errors.Is(err, apperr.ErrNotFound) {
		return invalidToken(err)
	}
	if err != nil {
		return apperr.NewInternal(err)
	}
	return nil
}

// publish writes one event to the outbox in its own transaction.
func (s *authService) publish(ctx context.Context, eventType string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return apperr.NewInternal(err)
	}
	err = s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return s.deps.Outbox.WithTx(tx).CreateOutboxEvent(ctx, &models.OutboxEvent{Type: eventType, Payload: body})
	})
	if err != nil {
		return apperr.NewInternal(err)
	}
	return nil
}

func (s *authService) LoginWithIdentity(ctx context.Context, provider string, identity oidc.Identity) (*models.TokenPair, error) {
	if identity.Subject == "" || provider != "google" {
		return nil, apperr.NewUnauthorized("invalid identity", nil)
	}
	var user *models.User
	err := s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		user, err = s.linkOrCreate(ctx, tx, provider, identity)
		return err
	})
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(err, &appErr) {
			return nil, err
		}
		return nil, apperr.NewInternal(err)
	}
	return s.startSession(ctx, user)
}

// linkOrCreate uses the stable subject first and requires provider authority before trusting an email.
func (s *authService) linkOrCreate(ctx context.Context, tx pgx.Tx, provider string, identity oidc.Identity) (*models.User, error) {
	users, identities := s.deps.Users.WithTx(tx), s.deps.Identities.WithTx(tx)
	email := normalizeEmail(identity.Email)

	id, err := identities.GetUserIdentity(ctx, provider, identity.Subject)
	if err == nil {
		return users.GetUser(ctx, id.UserID)
	}
	if !errors.Is(err, apperr.ErrNotFound) {
		return nil, err
	}

	if !identity.EmailVerified || validateEmail(email) != "" || (!strings.HasSuffix(email, "@gmail.com") && strings.TrimSpace(identity.HostedDomain) == "") {
		return nil, apperr.NewUnauthorized("email requires explicit account verification before linking", nil)
	}

	user, err := users.GetUserByEmail(ctx, email)
	switch {
	case errors.Is(err, apperr.ErrNotFound):
		user = &models.User{Email: email, Name: identity.Name}
		if err := users.CreateUser(ctx, user); err != nil {
			return nil, err
		}
		if err := users.MarkEmailVerified(ctx, user.ID); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	case user.EmailVerifiedAt == nil:
		// Someone registered this email without proving it; the provider just did. Take the account away from the password.
		if err := users.ClearUserPassword(ctx, user.ID); err != nil {
			return nil, err
		}
		if err := s.deps.Sessions.WithTx(tx).RevokeUserSessions(ctx, user.ID); err != nil {
			return nil, err
		}
		if err := users.MarkEmailVerified(ctx, user.ID); err != nil {
			return nil, err
		}
	}
	if err := identities.CreateUserIdentity(ctx, &models.UserIdentity{UserID: user.ID, Provider: provider, Subject: identity.Subject, Email: email}); err != nil {
		return nil, err
	}
	return users.GetUser(ctx, user.ID)
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func invalidCredentials() error {
	return apperr.NewUnauthorized("invalid email or password", nil)
}

func invalidToken(err error) error {
	return apperr.NewUnauthorized("invalid or expired token", err)
}

// allowRecovery coalesces requests before looking up the recipient, including unknown addresses.
func (s *authService) allowRecovery(ctx context.Context, purpose, email string) (bool, error) {
	decision, err := s.deps.Limiter.Allow(ctx, "recovery:"+purpose+email, 1, s.deps.RecipientCooldown)
	if err != nil {
		return false, limiterError(err)
	}
	return decision.Allowed, nil
}

func limiterError(err error) error {
	if errors.Is(err, ratelimit.ErrUnavailable) {
		return apperr.NewUnavailable("rate limiter unavailable", err)
	}
	return apperr.NewInternal(err)
}
