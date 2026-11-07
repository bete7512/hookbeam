//go:build integration

package repos_test

import (
	"context"
	"fmt"
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

type APIKeyRepoTestSuite struct {
	suite.Suite
	db   *pgxpool.Pool
	repo repos.APIKeyRepo
}

func TestAPIKeyRepoTestSuite(t *testing.T) {
	suite.Run(t, new(APIKeyRepoTestSuite))
}

func (s *APIKeyRepoTestSuite) SetupSuite() {
	s.db = testutil.Postgres(s.T(), migrations.Up)
	var err error
	s.repo, err = repos.NewAPIKeyRepo(s.db)
	s.Require().NoError(err)
}

func (s *APIKeyRepoTestSuite) SetupSubTest() {
	testutil.Truncate(s.T(), s.db, "api_keys")
}

// seed inserts a key; the hash is derived from the name so tests can find it again.
func (s *APIKeyRepoTestSuite) seed(workspace, name string, expiresAt *time.Time) *models.APIKey {
	k := &models.APIKey{WorkspaceID: workspace, Name: name, Prefix: "hb_" + name, KeyHash: "hash-" + workspace + "-" + name, Scopes: []string{models.ScopeEventsWrite}, CreatedBy: "u_1", ExpiresAt: expiresAt}
	s.Require().NoError(s.repo.CreateAPIKey(context.Background(), k))
	return k
}

func (s *APIKeyRepoTestSuite) TestCreateAPIKey() {
	tests := []struct {
		name    string
		seed    bool
		wantErr error
	}{
		{name: "creates with generated fields"},
		{name: "duplicate hash conflicts", seed: true, wantErr: apperr.ErrConflict},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.seed {
				s.seed("ws_1", "prod", nil)
			}
			k := &models.APIKey{WorkspaceID: "ws_1", Name: "prod", Prefix: "hb_prod", KeyHash: "hash-ws_1-prod", Scopes: []string{models.ScopeEventsWrite}, CreatedBy: "u_1"}

			err := s.repo.CreateAPIKey(context.Background(), k)

			if tt.wantErr != nil {
				s.ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Positive(k.ID)
			s.False(k.CreatedAt.IsZero())
			s.Equal(k.CreatedAt, k.UpdatedAt)
		})
	}
}

func (s *APIKeyRepoTestSuite) TestFindActiveAPIKeyByHash() {
	past := time.Now().Add(-time.Minute)
	future := time.Now().Add(time.Hour)
	tests := []struct {
		name    string
		seed    func() string
		wantErr error
	}{
		{name: "active key", seed: func() string { return s.seed("ws_1", "prod", nil).KeyHash }},
		{name: "not yet expired", seed: func() string { return s.seed("ws_1", "prod", &future).KeyHash }},
		{name: "expired", seed: func() string { return s.seed("ws_1", "prod", &past).KeyHash }, wantErr: apperr.ErrNotFound},
		{name: "revoked", seed: func() string {
			k := s.seed("ws_1", "prod", nil)
			s.Require().NoError(s.repo.RevokeAPIKey(context.Background(), "ws_1", k.ID))
			return k.KeyHash
		}, wantErr: apperr.ErrNotFound},
		{name: "unknown", seed: func() string { return "nope" }, wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			hash := tt.seed()

			k, err := s.repo.FindActiveAPIKeyByHash(context.Background(), hash)

			if tt.wantErr != nil {
				s.ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Equal(hash, k.KeyHash)
			s.Equal([]string{models.ScopeEventsWrite}, k.Scopes)
		})
	}
}

func (s *APIKeyRepoTestSuite) TestListAndCountAPIKeys() {
	tests := []struct {
		name      string
		opts      repos.ListAPIKeysOpts
		wantNames []string
		wantTotal int
	}{
		{name: "scoped to the workspace, id asc", opts: repos.ListAPIKeysOpts{WorkspaceID: "ws_1", Limit: 10}, wantNames: []string{"alpha", "beta", "gamma"}, wantTotal: 3},
		{name: "other workspace", opts: repos.ListAPIKeysOpts{WorkspaceID: "ws_2", Limit: 10}, wantNames: []string{"delta"}, wantTotal: 1},
		{name: "search is case-insensitive", opts: repos.ListAPIKeysOpts{WorkspaceID: "ws_1", Limit: 10, Search: "ALP"}, wantNames: []string{"alpha"}, wantTotal: 1},
		{name: "sort by name desc with paging", opts: repos.ListAPIKeysOpts{WorkspaceID: "ws_1", Limit: 2, Offset: 1, SortBy: "name", SortDir: "desc"}, wantNames: []string{"beta", "alpha"}, wantTotal: 3},
		{name: "unknown workspace", opts: repos.ListAPIKeysOpts{WorkspaceID: "ws_9", Limit: 10}, wantNames: []string{}, wantTotal: 0},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			for _, n := range []string{"alpha", "beta", "gamma"} {
				s.seed("ws_1", n, nil)
			}
			s.seed("ws_2", "delta", nil)

			keys, err := s.repo.ListAPIKeys(context.Background(), tt.opts)
			s.Require().NoError(err)
			total, err := s.repo.CountAPIKeys(context.Background(), tt.opts)
			s.Require().NoError(err)

			names := make([]string, 0, len(keys))
			for _, k := range keys {
				names = append(names, k.Name)
			}
			s.Equal(tt.wantNames, names)
			s.Equal(tt.wantTotal, total)
		})
	}
}

func (s *APIKeyRepoTestSuite) TestRevokeAPIKey() {
	tests := []struct {
		name      string
		workspace string
		id        func() int64
		wantErr   error
	}{
		{name: "revokes once", workspace: "ws_1", id: func() int64 { return s.seed("ws_1", "prod", nil).ID }},
		{name: "second revoke is not found", workspace: "ws_1", id: func() int64 {
			k := s.seed("ws_1", "prod", nil)
			s.Require().NoError(s.repo.RevokeAPIKey(context.Background(), "ws_1", k.ID))
			return k.ID
		}, wantErr: apperr.ErrNotFound},
		{name: "another workspace's key", workspace: "ws_2", id: func() int64 { return s.seed("ws_1", "prod", nil).ID }, wantErr: apperr.ErrNotFound},
		{name: "unknown id", workspace: "ws_1", id: func() int64 { return -1 }, wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			id := tt.id()

			err := s.repo.RevokeAPIKey(context.Background(), tt.workspace, id)

			if tt.wantErr != nil {
				s.ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			var revoked *time.Time
			s.Require().NoError(s.db.QueryRow(context.Background(), `SELECT revoked_at FROM api_keys WHERE id = $1`, id).Scan(&revoked))
			s.NotNil(revoked)
		})
	}
}

func (s *APIKeyRepoTestSuite) TestTouchAPIKey() {
	s.Run("records the first use and throttles the next", func() {
		k := s.seed("ws_1", "prod", nil)
		lastUsed := func() *time.Time {
			var t *time.Time
			s.Require().NoError(s.db.QueryRow(context.Background(), `SELECT last_used_at FROM api_keys WHERE id = $1`, k.ID).Scan(&t))
			return t
		}

		s.Require().NoError(s.repo.TouchAPIKey(context.Background(), k.ID))
		first := lastUsed()
		s.Require().NotNil(first)
		s.Require().NoError(s.repo.TouchAPIKey(context.Background(), k.ID))

		s.Equal(*first, *lastUsed(), "a second touch within a minute is skipped")
		s.NoError(s.repo.TouchAPIKey(context.Background(), -1), "unknown ids are ignored")
	})
	s.Run("touches again after a minute", func() {
		k := s.seed("ws_1", "prod", nil)
		_, err := s.db.Exec(context.Background(), `UPDATE api_keys SET last_used_at = now() - interval '2 minutes' WHERE id = $1`, k.ID)
		s.Require().NoError(err)

		s.Require().NoError(s.repo.TouchAPIKey(context.Background(), k.ID))

		var t time.Time
		s.Require().NoError(s.db.QueryRow(context.Background(), fmt.Sprintf(`SELECT last_used_at FROM api_keys WHERE id = %d`, k.ID)).Scan(&t))
		s.WithinDuration(time.Now(), t, 10*time.Second)
	})
}
