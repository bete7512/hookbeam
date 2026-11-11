//go:build integration

package repos_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
)

type OverviewRepoTestSuite struct {
	suite.Suite
	f *deliveryFixture
}

func TestOverviewRepoTestSuite(t *testing.T) {
	suite.Run(t, new(OverviewRepoTestSuite))
}

func (s *OverviewRepoTestSuite) SetupSuite()   { s.f = newDeliveryFixture(&s.Suite) }
func (s *OverviewRepoTestSuite) SetupSubTest() { s.f.truncate(&s.Suite) }

func (s *OverviewRepoTestSuite) TestGetOverview() {
	s.Run("counts the environment's volume, backlog, oldest pending and endpoint health", func() {
		ctx := context.Background()
		app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
		other := &models.Endpoint{WorkspaceID: ws, ApplicationID: app.ID, URL: "https://two.example.com/hooks", CreatedBy: actor}
		s.Require().NoError(s.f.endpoints.CreateEndpoint(ctx, other))
		unknownEp := &models.Endpoint{WorkspaceID: ws, ApplicationID: app.ID, URL: "https://three.example.com/hooks", CreatedBy: actor}
		s.Require().NoError(s.f.endpoints.CreateEndpoint(ctx, unknownEp))
		var ds []*models.Delivery
		for i := range 4 {
			m := s.f.seedMessage(&s.Suite, app.ID, "k"+string(rune('a'+i)))
			ds = append(ds, s.f.seedDelivery(&s.Suite, m.ID, e.ID, time.Now().Add(-time.Duration(i)*time.Minute)))
		}
		_, err := s.f.db.Exec(ctx, `UPDATE deliveries SET state = 'succeeded', completed_at = now() WHERE id = $1`, ds[0].ID)
		s.Require().NoError(err)
		_, err = s.f.db.Exec(ctx, `UPDATE deliveries SET state = 'failed', failure_reason = 'attempts_exhausted', completed_at = now() WHERE id = $1`, ds[1].ID)
		s.Require().NoError(err)
		// ds[2] and ds[3] stay queued; ds[3] is the oldest pending (due 3 minutes ago).
		health, err := repos.NewHealthRepo(s.f.db)
		s.Require().NoError(err)
		_, err = health.ObserveEndpointHealth(ctx, models.HealthObservation{EndpointID: e.ID, WorkspaceID: ws, ApplicationID: app.ID, Revision: 1, Failed: false, ObservedAt: time.Now()})
		s.Require().NoError(err)
		_, err = health.ObserveEndpointHealth(ctx, models.HealthObservation{EndpointID: other.ID, WorkspaceID: ws, ApplicationID: app.ID, Revision: 1, Failed: true, ObservedAt: time.Now()})
		s.Require().NoError(err)
		s.Require().NoError(health.OpenEpisode(ctx, &models.HealthEpisode{EndpointID: other.ID, WorkspaceID: ws, Revision: 1, ConsecutiveFailures: 3, OpenedAt: time.Now()}))

		repo, err := repos.NewOverviewRepo(s.f.db)
		s.Require().NoError(err)
		o, err := repo.GetOverview(ctx, ws, app.EnvironmentID, time.Now().Add(-time.Hour))
		s.Require().NoError(err)
		s.Equal(int64(4), o.Accepted)
		s.Equal(int64(1), o.Succeeded)
		s.Equal(int64(1), o.Exhausted)
		s.Equal(int64(2), o.Backlog)
		s.Require().NotNil(o.OldestPendingDueAt)
		s.WithinDuration(*ds[3].DueAt, *o.OldestPendingDueAt, time.Second)
		s.Equal(int32(1), o.EndpointsHealthy)
		s.Equal(int32(1), o.EndpointsUnhealthy)
		s.Equal(int32(1), o.EndpointsUnknown)
		s.NotNil(o.HealthObservedAt)
		s.Require().Len(o.Series, 2, "one bucket per hour from the window start to now")
		last := o.Series[len(o.Series)-1]
		s.Equal(int64(4), last.Accepted+o.Series[0].Accepted)
		s.Equal(int64(1), last.Succeeded+o.Series[0].Succeeded)
		s.Equal(int64(1), last.Failed+o.Series[0].Failed)

		stale, err := repo.GetOverview(ctx, ws, app.EnvironmentID, time.Now().Add(time.Hour))
		s.Require().NoError(err)
		s.Zero(stale.Accepted, "the window excludes everything")
		s.Equal(int64(2), stale.Backlog, "the backlog is not windowed")
		none, err := repo.GetOverview(ctx, "ws_other", app.EnvironmentID, time.Now().Add(-time.Hour))
		s.Require().NoError(err)
		s.Zero(none.Accepted)
		s.Zero(none.Backlog)
	})
	s.Run("waiting count per endpoint reads its partial index", func() {
		ctx := context.Background()
		app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
		for i := range 3 {
			m := s.f.seedMessage(&s.Suite, app.ID, "w"+string(rune('a'+i)))
			s.f.seedDelivery(&s.Suite, m.ID, e.ID, time.Now())
		}
		_, err := s.f.db.Exec(ctx, `UPDATE deliveries SET state = 'in_flight' WHERE id = (SELECT min(id) FROM deliveries)`)
		s.Require().NoError(err)
		n, err := s.f.deliveries.CountWaitingForEndpoint(ctx, e.ID)
		s.Require().NoError(err)
		s.Equal(int32(2), n, "in_flight is not waiting")
		_, err = s.f.db.Exec(ctx, `SET enable_seqscan = off`)
		s.Require().NoError(err)
		rows, err := s.f.db.Query(ctx, `EXPLAIN SELECT count(*) FROM deliveries WHERE endpoint_id = 1 AND state IN ('queued', 'retry_scheduled')`)
		s.Require().NoError(err)
		var lines []string
		for rows.Next() {
			var l string
			s.Require().NoError(rows.Scan(&l))
			lines = append(lines, l)
		}
		rows.Close()
		s.Require().NoError(rows.Err())
		_, err = s.f.db.Exec(ctx, `RESET enable_seqscan`)
		s.Require().NoError(err)
		s.Contains(strings.Join(lines, "\n"), "deliveries_endpoint_waiting_idx")

		secrets, err := s.f.secrets.ListEndpointSecrets(ctx, e.ID)
		s.Require().NoError(err)
		s.Empty(secrets, "seedEndpoint stores no secret")
		s.Require().NoError(s.f.secrets.CreateEndpointSecret(ctx, &models.EndpointSecret{EndpointID: e.ID, Ciphertext: []byte("c1"), KeyID: "k", Fingerprint: "f1"}))
		s.Require().NoError(s.f.secrets.CreateEndpointSecret(ctx, &models.EndpointSecret{EndpointID: e.ID, Ciphertext: []byte("c2"), KeyID: "k", Fingerprint: "f2"}))
		secrets, err = s.f.secrets.ListEndpointSecrets(ctx, e.ID)
		s.Require().NoError(err)
		s.Require().Len(secrets, 2)
		s.Equal(int32(2), secrets[0].Version, "newest first")
	})
}
