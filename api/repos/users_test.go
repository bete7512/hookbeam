//go:build integration

package repos_test

import (
	"context"
	"os"
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

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

type UserRepoTestSuite struct {
	suite.Suite
	db   *pgxpool.Pool
	repo repos.UserRepo
}

func TestUserRepoTestSuite(t *testing.T) {
	suite.Run(t, new(UserRepoTestSuite))
}

func (s *UserRepoTestSuite) SetupSuite() {
	s.db = testutil.Postgres(s.T(), migrations.Up)
	var err error
	s.repo, err = repos.NewUserRepo(s.db)
	s.Require().NoError(err)
}

func (s *UserRepoTestSuite) SetupSubTest() {
	testutil.Truncate(s.T(), s.db, "users")
}

// seed inserts one user per email, in order, so later emails are newer.
func (s *UserRepoTestSuite) seed(emails ...string) []*models.User {
	users := make([]*models.User, 0, len(emails))
	for _, email := range emails {
		hash := "hash"
		user := &models.User{Email: email, Name: "Ada", PasswordHash: &hash}
		s.Require().NoError(s.repo.CreateUser(context.Background(), user))
		users = append(users, user)
	}
	return users
}

func (s *UserRepoTestSuite) TestCreateUser() {
	tests := []struct {
		name    string
		seed    []string
		email   string
		wantErr error
	}{
		{name: "creates user", email: "ada@example.com"},
		{name: "duplicate email ignores case", seed: []string{"ada@example.com"}, email: "ADA@EXAMPLE.COM", wantErr: apperr.ErrConflict},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.seed(tt.seed...)
			hash := "hash"
			user := &models.User{Email: tt.email, Name: "Ada", PasswordHash: &hash}

			err := s.repo.CreateUser(context.Background(), user)

			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Positive(user.ID)
			s.False(user.CreatedAt.IsZero())
		})
	}
}

func (s *UserRepoTestSuite) TestGetUser() {
	tests := []struct {
		name    string
		seed    bool
		wantErr error
	}{
		{name: "found", seed: true},
		{name: "not found", wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			id := int64(-1)
			if tt.seed {
				id = s.seed("ada@example.com")[0].ID
			}

			got, err := s.repo.GetUser(context.Background(), id)

			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Equal("ada@example.com", got.Email)
		})
	}
}

func (s *UserRepoTestSuite) TestGetUserByEmail() {
	tests := []struct {
		name    string
		email   string
		wantErr error
	}{
		{name: "exact match", email: "ada@example.com"},
		{name: "ignores case", email: "ADA@Example.com"},
		{name: "not found", email: "nobody@example.com", wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			users := s.seed("ada@example.com")

			got, err := s.repo.GetUserByEmail(context.Background(), tt.email)

			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Equal(users[0].ID, got.ID)
		})
	}
}

func (s *UserRepoTestSuite) TestUpdateUser() {
	tests := []struct {
		name    string
		seed    bool
		wantErr error
	}{
		{name: "updates name", seed: true},
		{name: "not found", wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			id := int64(-1)
			if tt.seed {
				id = s.seed("ada@example.com")[0].ID
			}
			user := &models.User{ID: id, Name: "Ada King"}

			err := s.repo.UpdateUser(context.Background(), user)

			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Equal("Ada King", user.Name)
			s.Equal("ada@example.com", user.Email)
			s.True(user.UpdatedAt.After(user.CreatedAt))
		})
	}
}

func (s *UserRepoTestSuite) TestUpdateUserPassword() {
	tests := []struct {
		name    string
		seed    bool
		wantErr error
	}{
		{name: "sets hash and bumps token version", seed: true},
		{name: "not found", wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			ctx := context.Background()
			id := int64(-1)
			if tt.seed {
				id = s.seed("ada@example.com")[0].ID
			}

			err := s.repo.UpdateUserPassword(ctx, id, "new-hash")

			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			got, err := s.repo.GetUser(ctx, id)
			s.Require().NoError(err)
			s.Equal("new-hash", *got.PasswordHash)
			s.Equal(int32(1), got.TokenVersion)
		})
	}
}

func (s *UserRepoTestSuite) TestBumpUserTokenVersion() {
	tests := []struct {
		name    string
		seed    bool
		wantErr error
	}{
		{name: "increments token version", seed: true},
		{name: "not found", wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			ctx := context.Background()
			id := int64(-1)
			if tt.seed {
				id = s.seed("ada@example.com")[0].ID
			}

			err := s.repo.BumpUserTokenVersion(ctx, id)

			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			got, err := s.repo.GetUser(ctx, id)
			s.Require().NoError(err)
			s.Equal(int32(1), got.TokenVersion)
		})
	}
}

func (s *UserRepoTestSuite) TestClearUserPassword() {
	tests := []struct {
		name    string
		seed    []string
		id      func([]*models.User) int64
		wantErr error
	}{
		{name: "clears the password and bumps the token version", seed: []string{"ada@example.com"}, id: func(u []*models.User) int64 { return u[0].ID }},
		{name: "unknown user", id: func([]*models.User) int64 { return -1 }, wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			users := s.seed(tt.seed...)

			err := s.repo.ClearUserPassword(context.Background(), tt.id(users))

			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			got, err := s.repo.GetUser(context.Background(), users[0].ID)
			s.Require().NoError(err)
			s.False(got.HasPassword())
			s.Equal(users[0].TokenVersion+1, got.TokenVersion)
		})
	}
}

func (s *UserRepoTestSuite) TestMarkEmailVerified() {
	tests := []struct {
		name    string
		seed    []string
		id      func([]*models.User) int64
		twice   bool
		wantErr error
	}{
		{name: "sets email_verified_at", seed: []string{"ada@example.com"}, id: func(u []*models.User) int64 { return u[0].ID }},
		{name: "second call keeps the first timestamp", seed: []string{"ada@example.com"}, id: func(u []*models.User) int64 { return u[0].ID }, twice: true},
		{name: "unknown user", id: func([]*models.User) int64 { return -1 }, wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			users := s.seed(tt.seed...)

			err := s.repo.MarkEmailVerified(context.Background(), tt.id(users))

			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			first, err := s.repo.GetUser(context.Background(), users[0].ID)
			s.Require().NoError(err)
			s.Require().NotNil(first.EmailVerifiedAt)
			if tt.twice {
				s.Require().NoError(s.repo.MarkEmailVerified(context.Background(), users[0].ID))
				second, err := s.repo.GetUser(context.Background(), users[0].ID)
				s.Require().NoError(err)
				s.True(first.EmailVerifiedAt.Equal(*second.EmailVerifiedAt))
			}
		})
	}
}

func (s *UserRepoTestSuite) TestDeleteUser() {
	tests := []struct {
		name    string
		seed    bool
		wantErr error
	}{
		{name: "deletes", seed: true},
		{name: "not found", wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			ctx := context.Background()
			id := int64(-1)
			if tt.seed {
				id = s.seed("ada@example.com")[0].ID
			}

			err := s.repo.DeleteUser(ctx, id)

			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			_, err = s.repo.GetUser(ctx, id)
			s.Require().ErrorIs(err, apperr.ErrNotFound)
		})
	}
}

func (s *UserRepoTestSuite) TestListUsers() {
	tests := []struct {
		name       string
		opts       repos.ListUsersOpts
		wantEmails []string
		scopeOwner bool
	}{
		{name: "only scoped owner", opts: repos.ListUsersOpts{Limit: 10}, scopeOwner: true, wantEmails: []string{"ada@example.com"}},
		{name: "search cannot escape owner scope", opts: repos.ListUsersOpts{Limit: 10, Search: "bob"}, scopeOwner: true, wantEmails: []string{}},
		{name: "first page sorted by id", opts: repos.ListUsersOpts{Limit: 2}, wantEmails: []string{"ada@example.com", "bob@example.com"}},
		{name: "second page", opts: repos.ListUsersOpts{Limit: 2, Offset: 2}, wantEmails: []string{"carol@example.com"}},
		{name: "sort by email desc", opts: repos.ListUsersOpts{Limit: 10, SortBy: "email", SortDir: "desc"}, wantEmails: []string{"carol@example.com", "bob@example.com", "ada@example.com"}},
		{name: "sort by createdAt desc", opts: repos.ListUsersOpts{Limit: 10, SortBy: "createdAt", SortDir: "desc"}, wantEmails: []string{"carol@example.com", "bob@example.com", "ada@example.com"}},
		{name: "unknown sort key falls back to id", opts: repos.ListUsersOpts{Limit: 10, SortBy: "email; DROP TABLE users", SortDir: "desc"}, wantEmails: []string{"carol@example.com", "bob@example.com", "ada@example.com"}},
		{name: "offset past end", opts: repos.ListUsersOpts{Limit: 2, Offset: 10}, wantEmails: []string{}},
		{name: "search ignores case", opts: repos.ListUsersOpts{Limit: 10, Search: "BOB"}, wantEmails: []string{"bob@example.com"}},
		{name: "search without match", opts: repos.ListUsersOpts{Limit: 10, Search: "zzz"}, wantEmails: []string{}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			seeded := s.seed("ada@example.com", "bob@example.com", "carol@example.com")
			if tt.scopeOwner {
				tt.opts.OwnerID = seeded[0].ID
			}

			users, err := s.repo.ListUsers(context.Background(), tt.opts)

			s.Require().NoError(err)
			emails := make([]string, 0, len(users))
			for _, u := range users {
				emails = append(emails, u.Email)
			}
			s.Equal(tt.wantEmails, emails)
		})
	}
}

func (s *UserRepoTestSuite) TestCountUsers() {
	tests := []struct {
		name       string
		opts       repos.ListUsersOpts
		want       int
		scopeOwner bool
	}{
		{name: "only scoped owner", opts: repos.ListUsersOpts{}, scopeOwner: true, want: 1},
		{name: "search cannot escape owner scope", opts: repos.ListUsersOpts{Search: "bob"}, scopeOwner: true, want: 0},
		{name: "all users", opts: repos.ListUsersOpts{}, want: 3},
		{name: "search", opts: repos.ListUsersOpts{Search: "bob"}, want: 1},
		{name: "search without match", opts: repos.ListUsersOpts{Search: "zzz"}, want: 0},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			seeded := s.seed("ada@example.com", "bob@example.com", "carol@example.com")
			if tt.scopeOwner {
				tt.opts.OwnerID = seeded[0].ID
			}

			got, err := s.repo.CountUsers(context.Background(), tt.opts)

			s.Require().NoError(err)
			s.Equal(tt.want, got)
		})
	}
}

func (s *UserRepoTestSuite) TestGetUserForUpdate() {
	s.Run("locks until transaction finishes", func() {
		ctx := context.Background()
		user := s.seed("locked@example.com")[0]
		tx, err := s.db.Begin(ctx)
		s.Require().NoError(err)
		defer tx.Rollback(ctx)
		got, err := s.repo.WithTx(tx).GetUserForUpdate(ctx, user.ID)
		s.Require().NoError(err)
		s.Equal(user.ID, got.ID)
		blocked, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		err = s.repo.UpdateUserPassword(blocked, user.ID, "new-hash")
		s.Require().Error(err)
		s.Require().NoError(tx.Rollback(ctx))
		s.Require().NoError(s.repo.UpdateUserPassword(ctx, user.ID, "new-hash"))
	})
	s.Run("missing user", func() {
		_, err := s.repo.GetUserForUpdate(context.Background(), -1)
		s.Require().ErrorIs(err, apperr.ErrNotFound)
	})
}
