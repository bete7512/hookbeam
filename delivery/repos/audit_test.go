//go:build integration

package repos_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/bete7512/hookbeam/delivery/migrations"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/pkg/testutil"
)

type AuditRepoTestSuite struct {
	suite.Suite
	db   *pgxpool.Pool
	repo repos.AuditRepo
}

func TestAuditRepoTestSuite(t *testing.T) {
	suite.Run(t, new(AuditRepoTestSuite))
}

func (s *AuditRepoTestSuite) SetupSuite() {
	s.db = testutil.Postgres(s.T(), migrations.Up)
	var err error
	s.repo, err = repos.NewAuditRepo(s.db)
	s.Require().NoError(err)
}

func (s *AuditRepoTestSuite) SetupSubTest() {
	testutil.Truncate(s.T(), s.db, "audit_events")
}

func (s *AuditRepoTestSuite) record(ws, actor, action, resource string, id int64, at time.Time) *models.AuditEvent {
	s.T().Helper()
	e := &models.AuditEvent{WorkspaceID: ws, ActorID: actor, Action: action, ResourceType: resource, RequestID: "req-1", Details: json.RawMessage(`{"k":"v"}`)}
	if id != 0 {
		e.ResourceID = &id
	}
	s.Require().NoError(s.repo.CreateAuditEvent(context.Background(), e))
	_, err := s.db.Exec(context.Background(), `UPDATE audit_events SET occurred_at = $1 WHERE id = $2`, at, e.ID)
	s.Require().NoError(err)
	return e
}

func (s *AuditRepoTestSuite) TestAuditEvents() {
	ctx := context.Background()
	s.Run("create fills defaults; list is newest first and filters by actor, resource and time; scoped by workspace", func() {
		now := time.Now()
		old := s.record("ws_1", "u_ada", "endpoint.pause", "endpoint", 3, now.Add(-2*time.Hour))
		mid := s.record("ws_1", "u_bob", "api_key.create", "api_key", 7, now.Add(-time.Hour))
		recent := s.record("ws_1", "u_ada", "endpoint.resume", "endpoint", 3, now)
		s.record("ws_other", "u_ada", "endpoint.pause", "endpoint", 3, now)
		s.Equal(json.RawMessage(`{"k": "v"}`), old.Details)

		hourAgo := now.Add(-90 * time.Minute)
		tests := []struct {
			name string
			opts repos.ListAuditOpts
			want []int64
		}{
			{name: "all, newest first", opts: repos.ListAuditOpts{WorkspaceID: "ws_1", Limit: 10}, want: []int64{recent.ID, mid.ID, old.ID}},
			{name: "actor", opts: repos.ListAuditOpts{WorkspaceID: "ws_1", ActorID: "u_bob", Limit: 10}, want: []int64{mid.ID}},
			{name: "resource type and id", opts: repos.ListAuditOpts{WorkspaceID: "ws_1", ResourceType: "endpoint", ResourceID: 3, Limit: 10}, want: []int64{recent.ID, old.ID}},
			{name: "after", opts: repos.ListAuditOpts{WorkspaceID: "ws_1", After: &hourAgo, Limit: 10}, want: []int64{recent.ID, mid.ID}},
			{name: "before", opts: repos.ListAuditOpts{WorkspaceID: "ws_1", Before: &hourAgo, Limit: 10}, want: []int64{old.ID}},
			{name: "page", opts: repos.ListAuditOpts{WorkspaceID: "ws_1", Limit: 1, Offset: 1}, want: []int64{mid.ID}},
			{name: "another workspace", opts: repos.ListAuditOpts{WorkspaceID: "ws_none", Limit: 10}, want: []int64{}},
		}
		for _, tt := range tests {
			got, err := s.repo.ListAuditEvents(ctx, tt.opts)
			s.Require().NoError(err, tt.name)
			ids := make([]int64, 0, len(got))
			for _, e := range got {
				ids = append(ids, e.ID)
			}
			s.Equal(tt.want, ids, tt.name)
			if tt.opts.Offset == 0 && tt.opts.Limit == 10 {
				total, err := s.repo.CountAuditEvents(ctx, tt.opts)
				s.Require().NoError(err, tt.name)
				s.Equal(len(tt.want), total, tt.name)
			}
		}
	})
	s.Run("prune deletes only what is older than the cutoff, in batches", func() {
		now := time.Now()
		s.record("ws_1", "u", "a", "r", 0, now.Add(-100*24*time.Hour))
		s.record("ws_1", "u", "a", "r", 0, now.Add(-95*24*time.Hour))
		s.record("ws_1", "u", "a", "r", 0, now)
		n, err := s.repo.DeleteAuditEventsBefore(ctx, now.Add(-90*24*time.Hour), 1)
		s.Require().NoError(err)
		s.Equal(int64(1), n)
		n, err = s.repo.DeleteAuditEventsBefore(ctx, now.Add(-90*24*time.Hour), 10)
		s.Require().NoError(err)
		s.Equal(int64(1), n)
		total, err := s.repo.CountAuditEvents(ctx, repos.ListAuditOpts{WorkspaceID: "ws_1"})
		s.Require().NoError(err)
		s.Equal(1, total)
	})
}
