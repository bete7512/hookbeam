//go:build integration

package repos_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/bete7512/hookbeam/api/migrations"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/testutil"
)

type AuthTokenRepoTestSuite struct {
	suite.Suite
	db     *pgxpool.Pool
	users  repos.UserRepo
	tokens repos.AuthTokenRepo
}

func TestAuthTokenRepoTestSuite(t *testing.T) {
	suite.Run(t, new(AuthTokenRepoTestSuite))
}

func (s *AuthTokenRepoTestSuite) SetupSuite() {
	s.db = testutil.Postgres(s.T(), migrations.Up)
	var err error
	s.users, err = repos.NewUserRepo(s.db)
	s.Require().NoError(err)
	s.tokens, err = repos.NewAuthTokenRepo(s.db)
	s.Require().NoError(err)
}

func (s *AuthTokenRepoTestSuite) SetupSubTest() {
	testutil.Truncate(s.T(), s.db, "users", "auth_tokens")
}

func newAuthToken(userID int64, hash string, expiresAt time.Time) *models.AuthToken {
	return &models.AuthToken{UserID: userID, RequestID: 1, Purpose: models.TokenPurposeEmailVerification, TokenHash: []byte(hash), ExpiresAt: expiresAt}
}

// seed creates a user and stores each token for it, returning the user id and the tokens.
func (s *AuthTokenRepoTestSuite) seed(tokens ...*models.AuthToken) (int64, []*models.AuthToken) {
	ctx := context.Background()
	hash := "hash"
	user := &models.User{Email: "ada@example.com", Name: "Ada", PasswordHash: &hash}
	s.Require().NoError(s.users.CreateUser(ctx, user))

	for _, token := range tokens {
		token.UserID = user.ID
		s.Require().NoError(s.tokens.CreateAuthToken(ctx, token))
	}
	return user.ID, tokens
}

func (s *AuthTokenRepoTestSuite) TestCreateAuthToken() {
	tests := []struct {
		name     string
		seedUser bool
		seedHash string
		hash     string
		wantErr  error
	}{
		{name: "stores the token", seedUser: true, hash: "t1"},
		{name: "same request reuses the slot", seedUser: true, seedHash: "t1", hash: "t1"},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			userID := int64(-1)
			if tt.seedUser {
				var seeded []*models.AuthToken
				if tt.seedHash != "" {
					seeded = append(seeded, newAuthToken(0, tt.seedHash, time.Now().Add(time.Hour)))
				}
				userID, _ = s.seed(seeded...)
			}
			token := newAuthToken(userID, tt.hash, time.Now().Add(time.Hour))

			err := s.tokens.CreateAuthToken(context.Background(), token)

			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Positive(token.ID)
			s.False(token.CreatedAt.IsZero())
			s.Nil(token.UsedAt)
		})
	}
}

func (s *AuthTokenRepoTestSuite) TestConsumeAuthToken() {
	tests := []struct {
		name         string
		expiresAt    time.Time
		consumeFirst bool
		hash         string
		purpose      string
		wantErr      error
	}{
		{name: "valid token is consumed", expiresAt: time.Now().Add(time.Hour), hash: "t1", purpose: models.TokenPurposeEmailVerification},
		{name: "second consume", expiresAt: time.Now().Add(time.Hour), consumeFirst: true, hash: "t1", purpose: models.TokenPurposeEmailVerification, wantErr: apperr.ErrNotFound},
		{name: "expired token", expiresAt: time.Now().Add(-time.Minute), hash: "t1", purpose: models.TokenPurposeEmailVerification, wantErr: apperr.ErrNotFound},
		{name: "wrong purpose", expiresAt: time.Now().Add(time.Hour), hash: "t1", purpose: models.TokenPurposePasswordReset, wantErr: apperr.ErrNotFound},
		{name: "unknown hash", expiresAt: time.Now().Add(time.Hour), hash: "missing", purpose: models.TokenPurposeEmailVerification, wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			ctx := context.Background()
			userID, tokens := s.seed(newAuthToken(0, "t1", tt.expiresAt))
			if tt.consumeFirst {
				_, err := s.tokens.ConsumeAuthToken(ctx, []byte("t1"), models.TokenPurposeEmailVerification)
				s.Require().NoError(err)
			}

			got, err := s.tokens.ConsumeAuthToken(ctx, []byte(tt.hash), tt.purpose)

			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Equal(tokens[0].ID, got.ID)
			s.Equal(userID, got.UserID)
			s.Equal(models.TokenPurposeEmailVerification, got.Purpose)
			s.Equal([]byte("t1"), got.TokenHash)
			s.NotNil(got.UsedAt)
		})
	}
}

func (s *AuthTokenRepoTestSuite) TestCreateAuthTokenSlot() {
	tests := []struct {
		name         string
		requestID    int64
		consumeFirst bool
		wantRequest  int64
		wantHash     string
		wantUsed     bool
	}{
		{name: "retry preserves original token and expiry", requestID: 10, wantRequest: 10, wantHash: "original"},
		{name: "new request replaces previous token", requestID: 11, wantRequest: 11, wantHash: "candidate"},
		{name: "older delivery cannot replace newer request", requestID: 9, wantRequest: 10, wantHash: "original"},
		{name: "retry cannot resurrect a consumed token", requestID: 10, consumeFirst: true, wantRequest: 10, wantHash: "original", wantUsed: true},
		{name: "new request after consumption is allowed", requestID: 11, consumeFirst: true, wantRequest: 11, wantHash: "candidate"},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			ctx := context.Background()
			original := newAuthToken(0, "original", time.Now().Add(time.Minute).Truncate(time.Microsecond))
			original.RequestID = 10
			userID, _ := s.seed(original)
			if tt.consumeFirst {
				_, err := s.tokens.ConsumeAuthToken(ctx, []byte("original"), models.TokenPurposeEmailVerification)
				s.Require().NoError(err)
			}
			candidate := newAuthToken(userID, "candidate", time.Now().Add(time.Hour))
			candidate.RequestID = tt.requestID
			s.Require().NoError(s.tokens.CreateAuthToken(ctx, candidate))
			s.Equal(tt.wantRequest, candidate.RequestID)
			s.Equal(tt.wantHash, string(candidate.TokenHash))
			s.Equal(tt.wantUsed, candidate.UsedAt != nil)
			if tt.wantRequest == 10 {
				s.WithinDuration(original.ExpiresAt, candidate.ExpiresAt, time.Microsecond)
			}
			var count int
			s.Require().NoError(s.db.QueryRow(ctx, `SELECT count(*) FROM auth_tokens WHERE user_id=$1`, userID).Scan(&count))
			s.Equal(1, count)
		})
	}
}

func (s *AuthTokenRepoTestSuite) TestCreateAuthTokenConcurrent() {
	s.Run("highest request wins concurrent empty slot creation", func() {
		ctx := context.Background()
		userID, _ := s.seed()
		const requests = 16
		start := make(chan struct{})
		errs := make(chan error, requests)
		var wg sync.WaitGroup
		for i := int64(1); i <= requests; i++ {
			wg.Go(func() {
				<-start
				token := newAuthToken(userID, fmt.Sprintf("request-%d", i), time.Now().Add(time.Hour))
				token.RequestID = i
				errs <- s.tokens.CreateAuthToken(ctx, token)
			})
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			s.Require().NoError(err)
		}
		for i := int64(1); i <= requests; i++ {
			_, err := s.tokens.ConsumeAuthToken(ctx, []byte(fmt.Sprintf("request-%d", i)), models.TokenPurposeEmailVerification)
			if i == requests {
				s.Require().NoError(err)
			} else {
				s.ErrorIs(err, apperr.ErrNotFound)
			}
		}
	})
}
