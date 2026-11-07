//go:build integration

package repos_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/bete7512/scaffold/apps/api/migrations"
	"github.com/bete7512/scaffold/apps/api/models"
	"github.com/bete7512/scaffold/apps/api/repos"
	"github.com/bete7512/scaffold/pkg/apperr"
	"github.com/bete7512/scaffold/pkg/testutil"
)

type UserIdentityRepoTestSuite struct {
	suite.Suite
	db         *pgxpool.Pool
	users      repos.UserRepo
	identities repos.UserIdentityRepo
}

func TestUserIdentityRepoTestSuite(t *testing.T) {
	suite.Run(t, new(UserIdentityRepoTestSuite))
}

func (s *UserIdentityRepoTestSuite) SetupSuite() {
	s.db = testutil.Postgres(s.T(), migrations.Up)
	var err error
	s.users, err = repos.NewUserRepo(s.db)
	s.Require().NoError(err)
	s.identities, err = repos.NewUserIdentityRepo(s.db)
	s.Require().NoError(err)
}

func (s *UserIdentityRepoTestSuite) SetupSubTest() {
	testutil.Truncate(s.T(), s.db, "users", "user_identities")
}

func newIdentity(userID int64, subject string) *models.UserIdentity {
	return &models.UserIdentity{UserID: userID, Provider: "google", Subject: subject, Email: "ada@example.com"}
}

// seed creates a user with one google identity per subject and returns the user id and the identities.
func (s *UserIdentityRepoTestSuite) seed(subjects ...string) (int64, []*models.UserIdentity) {
	ctx := context.Background()
	hash := "hash"
	user := &models.User{Email: "ada@example.com", Name: "Ada", PasswordHash: &hash}
	s.Require().NoError(s.users.CreateUser(ctx, user))

	identities := make([]*models.UserIdentity, 0, len(subjects))
	for _, subject := range subjects {
		identity := newIdentity(user.ID, subject)
		s.Require().NoError(s.identities.CreateUserIdentity(ctx, identity))
		identities = append(identities, identity)
	}
	return user.ID, identities
}

func (s *UserIdentityRepoTestSuite) TestCreateUserIdentity() {
	tests := []struct {
		name     string
		seedUser bool
		seed     []string
		subject  string
		wantErr  error
	}{
		{name: "stores the identity", seedUser: true, subject: "sub-1"},
		{name: "duplicate provider and subject", seedUser: true, seed: []string{"sub-1"}, subject: "sub-1", wantErr: apperr.ErrConflict},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			userID := int64(-1)
			if tt.seedUser {
				userID, _ = s.seed(tt.seed...)
			}
			identity := newIdentity(userID, tt.subject)

			err := s.identities.CreateUserIdentity(context.Background(), identity)

			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Positive(identity.ID)
			s.False(identity.CreatedAt.IsZero())
		})
	}
}

func (s *UserIdentityRepoTestSuite) TestGetUserIdentity() {
	tests := []struct {
		name     string
		provider string
		subject  string
		wantErr  error
	}{
		{name: "found", provider: "google", subject: "sub-1"},
		{name: "unknown subject", provider: "google", subject: "missing", wantErr: apperr.ErrNotFound},
		{name: "other provider", provider: "github", subject: "sub-1", wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			userID, identities := s.seed("sub-1")

			got, err := s.identities.GetUserIdentity(context.Background(), tt.provider, tt.subject)

			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Equal(identities[0].ID, got.ID)
			s.Equal(userID, got.UserID)
			s.Equal("google", got.Provider)
			s.Equal("sub-1", got.Subject)
			s.Equal("ada@example.com", got.Email)
		})
	}
}
