//go:build integration

package repos_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/bete7512/hookbeam/api/migrations"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/testutil"
)

type MemberRoleRepoTestSuite struct {
	suite.Suite
	db   *pgxpool.Pool
	repo repos.MemberRoleRepo
}

func TestMemberRoleRepoTestSuite(t *testing.T) {
	suite.Run(t, new(MemberRoleRepoTestSuite))
}

func (s *MemberRoleRepoTestSuite) SetupSuite() {
	s.db = testutil.Postgres(s.T(), migrations.Up)
	var err error
	s.repo, err = repos.NewMemberRoleRepo(s.db)
	s.Require().NoError(err)
}

func (s *MemberRoleRepoTestSuite) SetupSubTest() {
	testutil.Truncate(s.T(), s.db, "member_roles")
}

func (s *MemberRoleRepoTestSuite) TestMemberRoles() {
	ctx := context.Background()
	s.Run("set, replace, list, delete", func() {
		_, err := s.repo.GetMemberRole(ctx, "ws_1", "u_bob")
		s.Require().ErrorIs(err, apperr.ErrNotFound)

		m := &models.MemberRole{WorkspaceID: "ws_1", UserID: "u_bob", Role: models.RoleViewer, UpdatedBy: "u_ada"}
		s.Require().NoError(s.repo.SetMemberRole(ctx, m))
		s.False(m.CreatedAt.IsZero())
		role, err := s.repo.GetMemberRole(ctx, "ws_1", "u_bob")
		s.Require().NoError(err)
		s.Equal(models.RoleViewer, role)

		m.Role = models.RoleDeveloper
		s.Require().NoError(s.repo.SetMemberRole(ctx, m))
		all, err := s.repo.ListMemberRoles(ctx, "ws_1")
		s.Require().NoError(err)
		s.Equal(map[string]string{"u_bob": models.RoleDeveloper}, all)

		s.Require().NoError(s.repo.DeleteMemberRole(ctx, "ws_1", "u_bob"))
		s.Require().NoError(s.repo.DeleteMemberRole(ctx, "ws_1", "u_bob"), "deleting twice is fine")
		_, err = s.repo.GetMemberRole(ctx, "ws_1", "u_bob")
		s.Require().ErrorIs(err, apperr.ErrNotFound)
	})
	s.Run("only developer and viewer are stored", func() {
		s.Error(s.repo.SetMemberRole(ctx, &models.MemberRole{WorkspaceID: "ws_1", UserID: "u_bob", Role: "owner", UpdatedBy: "u_ada"}))
	})
}
