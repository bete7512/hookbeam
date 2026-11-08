//go:build integration

package repos_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/bete7512/hookbeam/delivery/migrations"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/testutil"
)

// deliveryFixture holds the repos the message, delivery and attempt suites share.
type deliveryFixture struct {
	db         *pgxpool.Pool
	apps       repos.ApplicationRepo
	endpoints  repos.EndpointRepo
	secrets    repos.EndpointSecretRepo
	subs       repos.SubscriptionRepo
	messages   repos.MessageRepo
	deliveries repos.DeliveryRepo
	attempts   repos.AttemptRepo
}

func newDeliveryFixture(s *suite.Suite) *deliveryFixture {
	s.T().Helper()
	f := &deliveryFixture{db: testutil.Postgres(s.T(), migrations.Up)}
	var err error
	f.apps, err = repos.NewApplicationRepo(f.db)
	s.Require().NoError(err)
	f.endpoints, err = repos.NewEndpointRepo(f.db)
	s.Require().NoError(err)
	f.secrets, err = repos.NewEndpointSecretRepo(f.db)
	s.Require().NoError(err)
	f.subs, err = repos.NewSubscriptionRepo(f.db)
	s.Require().NoError(err)
	f.messages, err = repos.NewMessageRepo(f.db)
	s.Require().NoError(err)
	f.deliveries, err = repos.NewDeliveryRepo(f.db)
	s.Require().NoError(err)
	f.attempts, err = repos.NewAttemptRepo(f.db)
	s.Require().NoError(err)
	return f
}

func (f *deliveryFixture) truncate(s *suite.Suite) {
	testutil.Truncate(s.T(), f.db, "attempts", "deliveries", "messages", "subscriptions", "endpoint_secrets", "endpoints", "applications", "environments")
}

// seedEndpoint creates an application named appName with one endpoint subscribed to types (none = every type).
func (f *deliveryFixture) seedEndpoint(s *suite.Suite, appName string, types ...string) (*models.Application, *models.Endpoint) {
	s.T().Helper()
	app := seedApplication(s, f.db, f.apps, appName)
	e := &models.Endpoint{WorkspaceID: ws, ApplicationID: app.ID, URL: "https://receiver.example.com/hooks", CreatedBy: actor}
	s.Require().NoError(f.endpoints.CreateEndpoint(context.Background(), e))
	s.Require().NoError(f.subs.CreateSubscriptions(context.Background(), e.ID, types))
	return app, e
}

func (f *deliveryFixture) seedMessage(s *suite.Suite, appID int64, key string) *models.Message {
	s.T().Helper()
	payload := []byte(`{"type":"order.shipped","data":{"n":1}}`)
	sum := sha256.Sum256(payload)
	now := time.Now()
	exp := now.Add(7 * 24 * time.Hour)
	m := &models.Message{
		PublicID: fmt.Sprintf("msg_%d_%s", appID, key), WorkspaceID: ws, ApplicationID: appID, EventType: "order.shipped",
		IdempotencyKey: &key, IdempotencyExpiresAt: &exp, Payload: payload, PayloadSHA256: sum[:], PayloadExpiresAt: exp,
	}
	s.Require().NoError(f.messages.CreateMessage(context.Background(), m))
	return m
}

// recordOutcome applies an outcome and drops the returned state, for cases that only need the side effect.
func recordOutcome(r repos.DeliveryRepo, ctx context.Context, o models.DeliveryOutcome) error {
	_, err := r.RecordOutcome(ctx, o)
	return err
}

func (f *deliveryFixture) seedDelivery(s *suite.Suite, messageID, endpointID int64, due time.Time) *models.Delivery {
	s.T().Helper()
	d := &models.Delivery{MessageID: messageID, EndpointID: endpointID, EndpointRevision: 1, State: models.DeliveryQueued, DueAt: &due, DeadlineAt: due.Add(24 * time.Hour)}
	s.Require().NoError(f.deliveries.CreateDelivery(context.Background(), d))
	return d
}

type MessageRepoTestSuite struct {
	suite.Suite
	f *deliveryFixture
}

func TestMessageRepoTestSuite(t *testing.T) {
	suite.Run(t, new(MessageRepoTestSuite))
}

func (s *MessageRepoTestSuite) SetupSuite()   { s.f = newDeliveryFixture(&s.Suite) }
func (s *MessageRepoTestSuite) SetupSubTest() { s.f.truncate(&s.Suite) }

func (s *MessageRepoTestSuite) TestCreateMessage() {
	s.Run("creates and fills generated fields", func() {
		app, _ := s.f.seedEndpoint(&s.Suite, "Customer A")
		m := s.f.seedMessage(&s.Suite, app.ID, "k1")
		s.Positive(m.ID)
		s.False(m.AcceptedAt.IsZero())
		s.Equal(`{"type":"order.shipped","data":{"n":1}}`, string(m.Payload), "bytes come back exactly as stored") //nolint:testifylint // byte-exactness is the assertion, not JSON equality
	})
	s.Run("same key for the same application conflicts", func() {
		app, _ := s.f.seedEndpoint(&s.Suite, "Customer A")
		s.f.seedMessage(&s.Suite, app.ID, "k1")
		key := "k1"
		dup := &models.Message{PublicID: "msg_other", WorkspaceID: ws, ApplicationID: app.ID, EventType: "x", IdempotencyKey: &key, Payload: []byte(`{}`), PayloadSHA256: []byte("x"), PayloadExpiresAt: time.Now()}
		s.Require().ErrorIs(s.f.messages.CreateMessage(context.Background(), dup), apperr.ErrConflict)
	})
	s.Run("same key for another application is fine", func() {
		app, _ := s.f.seedEndpoint(&s.Suite, "Customer A")
		other, _ := s.f.seedEndpoint(&s.Suite, "Customer B")
		s.f.seedMessage(&s.Suite, app.ID, "k1")
		s.f.seedMessage(&s.Suite, other.ID, "k1")
	})
}

func (s *MessageRepoTestSuite) TestGetMessage() {
	tests := []struct {
		name      string
		workspace string
		id        func(m *models.Message) int64
		wantErr   error
	}{
		{name: "found", workspace: ws, id: func(m *models.Message) int64 { return m.ID }},
		{name: "other workspace", workspace: "ws_2", id: func(m *models.Message) int64 { return m.ID }, wantErr: apperr.ErrNotFound},
		{name: "unknown", workspace: ws, id: func(*models.Message) int64 { return -1 }, wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			app, _ := s.f.seedEndpoint(&s.Suite, "Customer A")
			m := s.f.seedMessage(&s.Suite, app.ID, "k1")
			got, err := s.f.messages.GetMessage(context.Background(), tt.workspace, tt.id(m))
			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Equal(m.PublicID, got.PublicID)
			byID, err := s.f.messages.GetMessageByID(context.Background(), m.ID)
			s.Require().NoError(err)
			s.Equal(m.ID, byID.ID)
		})
	}
}

func (s *MessageRepoTestSuite) TestFindMessageByIdempotencyKey() {
	s.Run("finds within the application only", func() {
		app, _ := s.f.seedEndpoint(&s.Suite, "Customer A")
		other, _ := s.f.seedEndpoint(&s.Suite, "Customer B")
		m := s.f.seedMessage(&s.Suite, app.ID, "k1")
		got, err := s.f.messages.FindMessageByIdempotencyKey(context.Background(), app.ID, "k1")
		s.Require().NoError(err)
		s.Equal(m.ID, got.ID)
		_, err = s.f.messages.FindMessageByIdempotencyKey(context.Background(), other.ID, "k1")
		s.Require().ErrorIs(err, apperr.ErrNotFound)
	})
}

func (s *MessageRepoTestSuite) TestExpireRetention() {
	ctx := context.Background()
	s.Run("expired payloads are emptied and keys cleared; fresh ones untouched; a cleared key accepts a new submission", func() {
		app, _ := s.f.seedEndpoint(&s.Suite, "Customer A")
		old := s.f.seedMessage(&s.Suite, app.ID, "old")
		fresh := s.f.seedMessage(&s.Suite, app.ID, "fresh")
		_, err := s.f.db.Exec(ctx, `UPDATE messages SET payload_expires_at = now() - interval '1 second', idempotency_expires_at = now() - interval '1 second' WHERE id = $1`, old.ID)
		s.Require().NoError(err)

		n, err := s.f.messages.ExpirePayloads(ctx, 10)
		s.Require().NoError(err)
		s.Equal(int64(1), n)
		n, err = s.f.messages.ExpireIdempotencyKeys(ctx, 10)
		s.Require().NoError(err)
		s.Equal(int64(1), n)

		got, err := s.f.messages.GetMessage(ctx, ws, old.ID)
		s.Require().NoError(err)
		s.Empty(got.Payload, "the bytes are gone")
		s.Equal(old.PublicID, got.PublicID, "the metadata stays")
		s.Nil(got.IdempotencyKey)
		_, err = s.f.messages.FindMessageByIdempotencyKey(ctx, app.ID, "old")
		s.Require().ErrorIs(err, apperr.ErrNotFound, "the key no longer dedupes")
		key, exp := "old", time.Now().Add(time.Hour)
		again := &models.Message{PublicID: "msg_again", WorkspaceID: ws, ApplicationID: app.ID, EventType: "order.shipped", IdempotencyKey: &key, IdempotencyExpiresAt: &exp, Payload: []byte("{}"), PayloadSHA256: make([]byte, 32), PayloadExpiresAt: exp}
		s.Require().NoError(s.f.messages.CreateMessage(ctx, again), "the same key is accepted as a new submission")

		got, err = s.f.messages.GetMessage(ctx, ws, fresh.ID)
		s.Require().NoError(err)
		s.NotEmpty(got.Payload)
		s.NotNil(got.IdempotencyKey)

		n, err = s.f.messages.ExpirePayloads(ctx, 10)
		s.Require().NoError(err)
		s.Zero(n, "already emptied rows are not touched again")
	})
	s.Run("batches honour the limit", func() {
		app, _ := s.f.seedEndpoint(&s.Suite, "Customer A")
		for _, k := range []string{"a", "b", "c"} {
			m := s.f.seedMessage(&s.Suite, app.ID, k)
			_, err := s.f.db.Exec(ctx, `UPDATE messages SET payload_expires_at = now() - interval '1 second' WHERE id = $1`, m.ID)
			s.Require().NoError(err)
		}
		n, err := s.f.messages.ExpirePayloads(ctx, 2)
		s.Require().NoError(err)
		s.Equal(int64(2), n)
		n, err = s.f.messages.ExpirePayloads(ctx, 2)
		s.Require().NoError(err)
		s.Equal(int64(1), n)
	})
}
