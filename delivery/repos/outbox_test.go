//go:build integration

package repos_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/bete7512/hookbeam/delivery/migrations"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/testutil"
)

type OutboxRepoTestSuite struct {
	suite.Suite
	db   *pgxpool.Pool
	repo repos.OutboxRepo
}

func TestOutboxRepoTestSuite(t *testing.T) {
	suite.Run(t, new(OutboxRepoTestSuite))
}

func (s *OutboxRepoTestSuite) SetupSuite() {
	s.db = testutil.Postgres(s.T(), migrations.Up)
	var err error
	s.repo, err = repos.NewOutboxRepo(s.db)
	s.Require().NoError(err)
}

func (s *OutboxRepoTestSuite) SetupSubTest() {
	testutil.Truncate(s.T(), s.db, "outbox")
}

// seed inserts n unpublished events in id order.
func (s *OutboxRepoTestSuite) seed(n int) {
	for i := range n {
		e := &models.OutboxEvent{Type: "delivery.due", Payload: json.RawMessage(fmt.Sprintf(`{"n":%d}`, i))}
		s.Require().NoError(s.repo.CreateOutboxEvent(context.Background(), e))
	}
}

// publish marks ids published at the given time.
func (s *OutboxRepoTestSuite) publish(at time.Time, ids ...int64) {
	_, err := s.db.Exec(context.Background(), `UPDATE outbox SET published_at = $1 WHERE id = ANY($2)`, at, ids)
	s.Require().NoError(err)
}

func (s *OutboxRepoTestSuite) all() []models.OutboxEvent {
	rows, err := s.db.Query(context.Background(), `SELECT id, type, payload, occurred_at, published_at FROM outbox ORDER BY id`)
	s.Require().NoError(err)
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[models.OutboxEvent])
	s.Require().NoError(err)
	return out
}

func (s *OutboxRepoTestSuite) TestCreateOutboxEvent() {
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	tests := []struct {
		name    string
		event   models.OutboxEvent
		wantAt  time.Time
		wantErr bool
	}{
		{name: "defaults occurred_at to now", event: models.OutboxEvent{Type: "delivery.due", Payload: json.RawMessage(`{"user_id":1}`)}},
		{name: "keeps a given occurred_at", event: models.OutboxEvent{Type: "delivery.due", Payload: json.RawMessage(`{"user_id":1}`), OccurredAt: fixed}, wantAt: fixed},
		{name: "rejects a missing payload", event: models.OutboxEvent{Type: "delivery.due"}, wantErr: true},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			e := tt.event

			err := s.repo.CreateOutboxEvent(context.Background(), &e)

			if tt.wantErr {
				s.Require().Error(err)
				s.Empty(s.all())
				return
			}
			s.Require().NoError(err)
			s.Positive(e.ID)
			if tt.wantAt.IsZero() {
				s.WithinDuration(time.Now(), e.OccurredAt, time.Minute)
			} else {
				s.True(tt.wantAt.Equal(e.OccurredAt))
			}
			got := s.all()
			s.Require().Len(got, 1)
			s.Equal(e.ID, got[0].ID)
			s.JSONEq(string(tt.event.Payload), string(got[0].Payload))
			s.Nil(got[0].PublishedAt)
		})
	}
}

func (s *OutboxRepoTestSuite) TestClaimOutboxEvents() {
	tests := []struct {
		name      string
		seed      int
		published []int64
		locked    []int64
		limit     int
		wantIDs   []int64
	}{
		{name: "claims unpublished in id order", seed: 3, limit: 10, wantIDs: []int64{1, 2, 3}},
		{name: "limit caps the batch", seed: 3, limit: 2, wantIDs: []int64{1, 2}},
		{name: "skips published rows", seed: 3, published: []int64{1}, limit: 10, wantIDs: []int64{2, 3}},
		{name: "skips rows locked by another transaction", seed: 3, locked: []int64{1, 2}, limit: 10, wantIDs: []int64{3}},
		{name: "empty outbox claims nothing", limit: 10, wantIDs: []int64{}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			ctx := context.Background()
			s.seed(tt.seed)
			s.publish(time.Now(), tt.published...)
			if len(tt.locked) > 0 {
				other, err := s.db.Begin(ctx)
				s.Require().NoError(err)
				defer func() { _ = other.Rollback(ctx) }()
				_, err = other.Exec(ctx, `SELECT id FROM outbox WHERE id = ANY($1) FOR UPDATE`, tt.locked)
				s.Require().NoError(err)
			}

			var gotIDs []int64
			err := db.WithTx(ctx, s.db, func(ctx context.Context, tx pgx.Tx) error {
				got, err := s.repo.WithTx(tx).ClaimOutboxEvents(ctx, tt.limit)
				if err != nil {
					return err
				}
				gotIDs = make([]int64, 0, len(got))
				for _, e := range got {
					gotIDs = append(gotIDs, e.ID)
					s.Equal("delivery.due", e.Type)
					s.Nil(e.PublishedAt)
				}
				return nil
			})

			s.Require().NoError(err)
			s.Equal(tt.wantIDs, gotIDs)
		})
	}
}

func (s *OutboxRepoTestSuite) TestMarkOutboxEventsPublished() {
	tests := []struct {
		name string
		seed int
		ids  []int64
	}{
		{name: "marks the given ids", seed: 3, ids: []int64{1, 3}},
		{name: "no ids changes nothing", seed: 2, ids: []int64{}},
		{name: "unknown ids are ignored", seed: 1, ids: []int64{-1}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.seed(tt.seed)

			err := s.repo.MarkOutboxEventsPublished(context.Background(), tt.ids)

			s.Require().NoError(err)
			for _, e := range s.all() {
				if slices.Contains(tt.ids, e.ID) {
					s.NotNil(e.PublishedAt)
				} else {
					s.Nil(e.PublishedAt)
				}
			}
		})
	}
}

func (s *OutboxRepoTestSuite) TestDeleteOutboxEventsPublishedBefore() {
	now := time.Now()
	old := now.Add(-8 * 24 * time.Hour)
	tests := []struct {
		name        string
		limit       int
		wantDeleted int64
		wantLeft    []int64
	}{
		{name: "deletes only published rows before the cutoff", limit: 100, wantDeleted: 2, wantLeft: []int64{3, 4}},
		{name: "limit caps one call", limit: 1, wantDeleted: 1, wantLeft: []int64{2, 3, 4}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.seed(4)
			s.publish(old, 1, 2)
			s.publish(now, 3)

			deleted, err := s.repo.DeleteOutboxEventsPublishedBefore(context.Background(), now.Add(-7*24*time.Hour), tt.limit)

			s.Require().NoError(err)
			s.Equal(tt.wantDeleted, deleted)
			left := make([]int64, 0)
			for _, e := range s.all() {
				left = append(left, e.ID)
			}
			s.Equal(tt.wantLeft, left)
		})
	}
}
