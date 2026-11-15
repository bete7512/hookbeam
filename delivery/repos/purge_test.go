//go:build integration

package repos_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

// PurgeRepoTestSuite covers the DeleteXForApplication methods together: foreign keys fix the order they run in.
type PurgeRepoTestSuite struct {
	suite.Suite
	f      *deliveryFixture
	envs   repos.EnvironmentRepo
	health repos.HealthRepo
}

func TestPurgeRepoTestSuite(t *testing.T) {
	suite.Run(t, new(PurgeRepoTestSuite))
}

func (s *PurgeRepoTestSuite) SetupSuite() {
	s.f = newDeliveryFixture(&s.Suite)
	var err error
	s.envs, err = repos.NewEnvironmentRepo(s.f.db)
	s.Require().NoError(err)
	s.health, err = repos.NewHealthRepo(s.f.db)
	s.Require().NoError(err)
}

func (s *PurgeRepoTestSuite) SetupSubTest() { s.f.truncate(&s.Suite) }

// tree is everything one application owns.
type tree struct {
	app      *models.Application
	endpoint *models.Endpoint
}

// seedTree creates an application in workspace with an endpoint (secret, subscription, health row, open episode),
// a message, a failed delivery with one attempt and a queued replay of it.
func (s *PurgeRepoTestSuite) seedTree(workspace, name string) tree {
	s.T().Helper()
	ctx := context.Background()
	env, err := s.envs.EnsureDefaultEnvironment(ctx, workspace, actor)
	s.Require().NoError(err)
	app := &models.Application{WorkspaceID: workspace, EnvironmentID: env.ID, Name: name, CreatedBy: actor}
	s.Require().NoError(s.f.apps.CreateApplication(ctx, app))
	e := &models.Endpoint{WorkspaceID: workspace, ApplicationID: app.ID, URL: "https://receiver.example.com/hooks", CreatedBy: actor}
	s.Require().NoError(s.f.endpoints.CreateEndpoint(ctx, e))
	s.Require().NoError(s.f.secrets.CreateEndpointSecret(ctx, &models.EndpointSecret{EndpointID: e.ID, Ciphertext: []byte("x"), KeyID: "k", Fingerprint: "f"}))
	s.Require().NoError(s.f.subs.CreateSubscriptions(ctx, e.ID, []string{"order.shipped"}))
	now := time.Now()
	_, err = s.health.ObserveEndpointHealth(ctx, models.HealthObservation{EndpointID: e.ID, WorkspaceID: workspace, ApplicationID: app.ID, Revision: 1, Failed: true, ObservedAt: now})
	s.Require().NoError(err)
	s.Require().NoError(s.health.OpenEpisode(ctx, &models.HealthEpisode{EndpointID: e.ID, WorkspaceID: workspace, Revision: 1, ConsecutiveFailures: 3, OpenedAt: now}))

	payload := []byte(`{"type":"order.shipped"}`)
	m := &models.Message{
		PublicID:         fmt.Sprintf("msg_%s_%d", workspace, app.ID),
		WorkspaceID:      workspace,
		ApplicationID:    app.ID,
		EventType:        "order.shipped",
		Payload:          payload,
		PayloadSHA256:    []byte("sum"),
		PayloadExpiresAt: now.Add(time.Hour),
	}
	s.Require().NoError(s.f.messages.CreateMessage(ctx, m))
	original := &models.Delivery{WorkspaceID: workspace, MessageID: m.ID, EndpointID: e.ID, EndpointRevision: 1, State: models.DeliveryQueued, DueAt: &now, DeadlineAt: now.Add(time.Hour)}
	s.Require().NoError(s.f.deliveries.CreateDelivery(ctx, original))
	s.Require().NoError(s.f.attempts.CreateAttempt(ctx, &models.Attempt{DeliveryID: original.ID, Number: 1, EndpointRevision: 1, SecretVersion: 1, RequestURL: e.URL, StartedAt: now}))
	_, err = s.f.db.Exec(ctx, `UPDATE deliveries SET state = 'failed', due_at = NULL WHERE id = $1`, original.ID)
	s.Require().NoError(err)
	key := "replay-1"
	replay := &models.Delivery{WorkspaceID: workspace, MessageID: m.ID, EndpointID: e.ID, EndpointRevision: 1, State: models.DeliveryQueued, DueAt: &now, DeadlineAt: now.Add(time.Hour), ReplayOfDeliveryID: &original.ID, ReplayIdempotencyKey: &key}
	s.Require().NoError(s.f.deliveries.CreateDelivery(ctx, replay))
	return tree{app: app, endpoint: e}
}

// rows counts what an application still owns, table by table.
func (s *PurgeRepoTestSuite) rows(t tree) map[string]int {
	s.T().Helper()
	queries := map[string]string{
		"applications":     `SELECT count(*) FROM applications WHERE id = $1`,
		"endpoints":        `SELECT count(*) FROM endpoints WHERE application_id = $1`,
		"endpoint_secrets": `SELECT count(*) FROM endpoint_secrets WHERE endpoint_id IN (SELECT id FROM endpoints WHERE application_id = $1)`,
		"subscriptions":    `SELECT count(*) FROM subscriptions WHERE endpoint_id IN (SELECT id FROM endpoints WHERE application_id = $1)`,
		"endpoint_health":  `SELECT count(*) FROM endpoint_health WHERE application_id = $1`,
		"messages":         `SELECT count(*) FROM messages WHERE application_id = $1`,
		"deliveries":       `SELECT count(*) FROM deliveries WHERE message_id IN (SELECT id FROM messages WHERE application_id = $1)`,
		"attempts":         `SELECT count(*) FROM attempts WHERE delivery_id IN (SELECT d.id FROM deliveries d JOIN messages m ON m.id = d.message_id WHERE m.application_id = $1)`,
	}
	out := map[string]int{}
	for table, q := range queries {
		var n int
		s.Require().NoError(s.f.db.QueryRow(context.Background(), q, t.app.ID).Scan(&n))
		out[table] = n
	}
	return out
}

var fullTree = map[string]int{
	"applications":     1,
	"endpoints":        1,
	"endpoint_secrets": 1,
	"subscriptions":    1,
	"endpoint_health":  1,
	"messages":         1,
	"deliveries":       2,
	"attempts":         1,
}

func (s *PurgeRepoTestSuite) TestDeleteForApplication() {
	ctx := context.Background()
	s.Run("deletes one application's rows in dependency order and nothing else", func() {
		target := s.seedTree(ws, "Checkout")
		sibling := s.seedTree(ws, "Billing")
		foreign := s.seedTree("ws_2", "Checkout")
		_, err := s.f.apps.ArchiveApplication(ctx, ws, target.app.ID)
		s.Require().NoError(err)

		attempts, err := s.f.attempts.DeleteAttemptsForApplication(ctx, target.app.ID)
		s.Require().NoError(err)
		s.Equal(int64(1), attempts)
		deliveries, err := s.f.deliveries.DeleteDeliveriesForApplication(ctx, target.app.ID)
		s.Require().NoError(err)
		s.Equal(int64(2), deliveries, "the original and the replay that references it go in one statement")
		messages, err := s.f.messages.DeleteMessagesForApplication(ctx, target.app.ID)
		s.Require().NoError(err)
		s.Equal(int64(1), messages)
		health, err := s.health.DeleteHealthForApplication(ctx, target.app.ID)
		s.Require().NoError(err)
		s.Equal(int64(2), health, "the health row and its episode")
		endpoints, err := s.f.endpoints.DeleteEndpointsForApplication(ctx, target.app.ID)
		s.Require().NoError(err)
		s.Equal(int64(1), endpoints)
		s.Require().NoError(s.f.apps.DeleteApplication(ctx, ws, target.app.ID))

		for table, n := range s.rows(target) {
			s.Zero(n, "%s of the purged application", table)
		}
		var episodes int
		s.Require().NoError(s.f.db.QueryRow(ctx, `SELECT count(*) FROM health_episodes WHERE endpoint_id = $1`, target.endpoint.ID).Scan(&episodes))
		s.Zero(episodes)
		s.Equal(fullTree, s.rows(sibling), "another application of the workspace is untouched")
		s.Equal(fullTree, s.rows(foreign), "another workspace is untouched")
		s.Require().NoError(s.f.db.QueryRow(ctx, `SELECT count(*) FROM health_episodes WHERE endpoint_id IN ($1, $2)`, sibling.endpoint.ID, foreign.endpoint.ID).Scan(&episodes))
		s.Equal(2, episodes)
	})
	s.Run("an application with nothing under it deletes to zero counts", func() {
		app := seedApplication(&s.Suite, s.f.db, s.f.apps, "Empty")
		_, err := s.f.apps.ArchiveApplication(ctx, ws, app.ID)
		s.Require().NoError(err)
		for name, del := range map[string]func(context.Context, int64) (int64, error){
			"attempts":   s.f.attempts.DeleteAttemptsForApplication,
			"deliveries": s.f.deliveries.DeleteDeliveriesForApplication,
			"messages":   s.f.messages.DeleteMessagesForApplication,
			"health":     s.health.DeleteHealthForApplication,
			"endpoints":  s.f.endpoints.DeleteEndpointsForApplication,
		} {
			n, err := del(ctx, app.ID)
			s.Require().NoError(err, name)
			s.Zero(n, name)
		}
		s.Require().NoError(s.f.apps.DeleteApplication(ctx, ws, app.ID))
	})
	s.Run("deliveries cannot go before their attempts", func() {
		target := s.seedTree(ws, "Checkout")
		_, err := s.f.deliveries.DeleteDeliveriesForApplication(ctx, target.app.ID)
		s.Require().Error(err, "the foreign key from attempts holds")
		s.Equal(fullTree, s.rows(target))
	})
}

func (s *PurgeRepoTestSuite) TestDeleteApplication() {
	ctx := context.Background()
	tests := []struct {
		name      string
		workspace string
		archive   bool
		wantErr   error
	}{
		{name: "an archived application of the workspace", workspace: ws, archive: true},
		{name: "an application that is not archived is not found", workspace: ws, wantErr: apperr.ErrNotFound},
		{name: "another workspace's application is not found", workspace: "ws_2", archive: true, wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			app := seedApplication(&s.Suite, s.f.db, s.f.apps, "Checkout")
			if tt.archive {
				_, err := s.f.apps.ArchiveApplication(ctx, ws, app.ID)
				s.Require().NoError(err)
			}
			err := s.f.apps.DeleteApplication(ctx, tt.workspace, app.ID)
			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				_, err = s.f.apps.GetApplication(ctx, ws, app.ID)
				s.Require().NoError(err, "still there")
				return
			}
			s.Require().NoError(err)
			_, err = s.f.apps.GetApplication(ctx, ws, app.ID)
			s.Require().ErrorIs(err, apperr.ErrNotFound)
		})
	}
	s.Run("unknown id", func() {
		s.Require().ErrorIs(s.f.apps.DeleteApplication(ctx, ws, int64(-1)), apperr.ErrNotFound)
	})
}

func (s *PurgeRepoTestSuite) TestCountInFlightForApplication() {
	ctx := context.Background()
	s.Run("counts only the application's leased deliveries", func() {
		target := s.seedTree(ws, "Checkout")
		other := s.seedTree(ws, "Billing")
		n, err := s.f.deliveries.CountInFlightForApplication(ctx, target.app.ID)
		s.Require().NoError(err)
		s.Zero(n, "a failed delivery and a queued replay")
		_, err = s.f.db.Exec(ctx, `UPDATE deliveries SET state = 'in_flight', lease_token = 'x', lease_expires_at = now() + interval '1 minute' WHERE state = 'queued' AND endpoint_id IN ($1, $2)`, target.endpoint.ID, other.endpoint.ID)
		s.Require().NoError(err)
		n, err = s.f.deliveries.CountInFlightForApplication(ctx, target.app.ID)
		s.Require().NoError(err)
		s.Equal(1, n)
	})
}
