//go:build integration

package repos_test

import (
	"context"
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

type SessionRepoTestSuite struct {
	suite.Suite
	db       *pgxpool.Pool
	users    repos.UserRepo
	sessions repos.SessionRepo
}

func TestSessionRepoTestSuite(t *testing.T) {
	suite.Run(t, new(SessionRepoTestSuite))
}

func (s *SessionRepoTestSuite) SetupSuite() {
	s.db = testutil.Postgres(s.T(), migrations.Up)
	var err error
	s.users, err = repos.NewUserRepo(s.db)
	s.Require().NoError(err)
	s.sessions, err = repos.NewSessionRepo(s.db)
	s.Require().NoError(err)
}

func (s *SessionRepoTestSuite) SetupSubTest() {
	testutil.Truncate(s.T(), s.db, "users")
}

func newSession(userID int64, token string) *models.Session {
	return &models.Session{UserID: userID, RefreshTokenHash: []byte(token), ExpiresAt: time.Now().Add(time.Hour)}
}

// seed creates a user with one session per token and returns the user id and the sessions.
func (s *SessionRepoTestSuite) seed(tokens ...string) (int64, []*models.Session) {
	ctx := context.Background()
	hash := "hash"
	user := &models.User{Email: "ada@example.com", Name: "Ada", PasswordHash: &hash}
	s.Require().NoError(s.users.CreateUser(ctx, user))

	sessions := make([]*models.Session, 0, len(tokens))
	for _, token := range tokens {
		session := newSession(user.ID, token)
		s.Require().NoError(s.sessions.CreateSession(ctx, session))
		sessions = append(sessions, session)
	}
	return user.ID, sessions
}

func (s *SessionRepoTestSuite) TestCreateSession() {
	tests := []struct {
		name     string
		seedUser bool
		wantErr  bool
	}{
		{name: "stores the session", seedUser: true},
		{name: "unknown user", wantErr: true},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			userID := int64(-1)
			if tt.seedUser {
				userID, _ = s.seed()
			}
			session := newSession(userID, "t1")

			err := s.sessions.CreateSession(context.Background(), session)

			if tt.wantErr {
				s.Require().Error(err)
				return
			}
			s.Require().NoError(err)
			s.Positive(session.ID)
			s.False(session.CreatedAt.IsZero())
		})
	}
}

func (s *SessionRepoTestSuite) TestFindSessionByToken() {
	tests := []struct {
		name        string
		rotateFirst bool
		token       string
		wantErr     error
	}{
		{name: "current token", token: "t1"},
		{name: "previous token after a rotation", rotateFirst: true, token: "t1"},
		{name: "unknown token", token: "missing", wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			ctx := context.Background()
			_, sessions := s.seed("t1")
			if tt.rotateFirst {
				sessions[0].RefreshTokenHash = []byte("t2")
				s.Require().NoError(s.sessions.RotateRefreshToken(ctx, sessions[0], []byte("t1")))
			}

			got, err := s.sessions.FindSessionByToken(ctx, []byte(tt.token))

			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Equal(sessions[0].ID, got.ID)
		})
	}
}

func (s *SessionRepoTestSuite) TestRotateRefreshToken() {
	tests := []struct {
		name        string
		currentHash string
		revoke      bool
		wantErr     error
	}{
		{name: "rotates the current token", currentHash: "t1"},
		{name: "stale current token", currentHash: "old", wantErr: apperr.ErrNotFound},
		{name: "revoked session", currentHash: "t1", revoke: true, wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			ctx := context.Background()
			_, sessions := s.seed("t1")
			session := sessions[0]
			if tt.revoke {
				s.Require().NoError(s.sessions.RevokeSession(ctx, session.ID))
			}
			session.RefreshTokenHash = []byte("t2")

			err := s.sessions.RotateRefreshToken(ctx, session, []byte(tt.currentHash))

			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Equal([]byte("t1"), session.PreviousTokenHash)
			got, err := s.sessions.FindSessionByToken(ctx, []byte("t2"))
			s.Require().NoError(err)
			s.Equal([]byte("t2"), got.RefreshTokenHash)
			s.Equal([]byte("t1"), got.PreviousTokenHash)
		})
	}
}

func (s *SessionRepoTestSuite) TestRevokeSession() {
	tests := []struct {
		name string
		seed bool
	}{
		{name: "revokes the session", seed: true},
		{name: "unknown session is a no-op"},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			ctx := context.Background()
			id := int64(-1)
			if tt.seed {
				_, sessions := s.seed("t1")
				id = sessions[0].ID
			}

			s.Require().NoError(s.sessions.RevokeSession(ctx, id))

			if !tt.seed {
				return
			}
			got, err := s.sessions.FindSessionByToken(ctx, []byte("t1"))
			s.Require().NoError(err)
			s.NotNil(got.RevokedAt)
		})
	}
}

func (s *SessionRepoTestSuite) TestRevokeUserSessions() {
	tests := []struct {
		name   string
		tokens []string
	}{
		{name: "revokes every session", tokens: []string{"t1", "t2"}},
		{name: "user without sessions"},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			ctx := context.Background()
			userID, _ := s.seed(tt.tokens...)

			s.Require().NoError(s.sessions.RevokeUserSessions(ctx, userID))

			for _, token := range tt.tokens {
				got, err := s.sessions.FindSessionByToken(ctx, []byte(token))
				s.Require().NoError(err)
				s.NotNil(got.RevokedAt)
			}
		})
	}
}

func (s *SessionRepoTestSuite) TestSessionGeneration() {
	s.Run("cannot create a session from credentials invalidated meanwhile", func() {
		ctx := context.Background()
		userID, sessions := s.seed("old")
		s.Require().NoError(s.users.BumpUserTokenVersion(ctx, userID))
		s.Require().ErrorIs(s.sessions.CreateSession(ctx, newSession(userID, "stale")), apperr.ErrNotFound)
		stored, err := s.sessions.FindSessionByToken(ctx, []byte("old"))
		s.Require().NoError(err)
		s.Equal(int32(0), stored.TokenVersion)
		s.Equal(sessions[0].ID, stored.ID)
		fresh := newSession(userID, "fresh")
		fresh.TokenVersion = 1
		s.Require().NoError(s.sessions.CreateSession(ctx, fresh))
		stored, err = s.sessions.FindSessionByToken(ctx, []byte("fresh"))
		s.Require().NoError(err)
		s.Equal(int32(1), stored.TokenVersion)
	})
}
