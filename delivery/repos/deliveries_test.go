//go:build integration

package repos_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

type DeliveryRepoTestSuite struct {
	suite.Suite
	f *deliveryFixture
}

func TestDeliveryRepoTestSuite(t *testing.T) {
	suite.Run(t, new(DeliveryRepoTestSuite))
}

func (s *DeliveryRepoTestSuite) SetupSuite()   { s.f = newDeliveryFixture(&s.Suite) }
func (s *DeliveryRepoTestSuite) SetupSubTest() { s.f.truncate(&s.Suite) }

func (s *DeliveryRepoTestSuite) TestCreateDelivery() {
	s.Run("creates queued with defaults", func() {
		app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
		m := s.f.seedMessage(&s.Suite, app.ID, "k1")
		d := s.f.seedDelivery(&s.Suite, m.ID, e.ID, time.Now())
		s.Positive(d.ID)
		s.Equal(int32(0), d.AttemptCount)
		s.Nil(d.LeaseToken)
		s.False(d.CancelRequested)
		n, err := s.f.deliveries.CountDeliveriesByMessage(context.Background(), m.ID)
		s.Require().NoError(err)
		s.Equal(int32(1), n)
	})
	s.Run("one active cycle per message and endpoint", func() {
		app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
		m := s.f.seedMessage(&s.Suite, app.ID, "k1")
		s.f.seedDelivery(&s.Suite, m.ID, e.ID, time.Now())
		due := time.Now()
		dup := &models.Delivery{MessageID: m.ID, EndpointID: e.ID, EndpointRevision: 1, State: models.DeliveryQueued, DueAt: &due, DeadlineAt: due}
		s.Require().ErrorIs(s.f.deliveries.CreateDelivery(context.Background(), dup), apperr.ErrConflict)
	})
}

func (s *DeliveryRepoTestSuite) TestGetDelivery() {
	s.Run("scoped through the message's workspace", func() {
		app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
		m := s.f.seedMessage(&s.Suite, app.ID, "k1")
		d := s.f.seedDelivery(&s.Suite, m.ID, e.ID, time.Now())
		got, err := s.f.deliveries.GetDelivery(context.Background(), ws, d.ID)
		s.Require().NoError(err)
		s.Equal(d.ID, got.ID)
		_, err = s.f.deliveries.GetDelivery(context.Background(), "ws_2", d.ID)
		s.Require().ErrorIs(err, apperr.ErrNotFound)
		list, err := s.f.deliveries.ListDeliveriesByMessage(context.Background(), m.ID)
		s.Require().NoError(err)
		s.Len(list, 1)
	})
}

func (s *DeliveryRepoTestSuite) TestClaimDueDeliveries() {
	ctx := context.Background()
	past, future := time.Now().Add(-time.Minute), time.Now().Add(time.Hour)
	tests := []struct {
		name    string
		prepare func(e *models.Endpoint, d *models.Delivery)
		due     time.Time
		want    int
	}{
		{name: "claims a due queued delivery", due: past, want: 1},
		{name: "skips a delivery that is not due", due: future},
		{name: "skips a paused endpoint", due: past, prepare: func(e *models.Endpoint, _ *models.Delivery) {
			_, err := s.f.endpoints.PauseEndpoint(ctx, ws, e.ID)
			s.Require().NoError(err)
		}},
		{name: "skips an archived endpoint", due: past, prepare: func(e *models.Endpoint, _ *models.Delivery) {
			_, err := s.f.db.Exec(ctx, `UPDATE endpoints SET archived_at = now() WHERE id = $1`, e.ID)
			s.Require().NoError(err)
		}},
		{name: "skips a cancel request", due: past, prepare: func(_ *models.Endpoint, d *models.Delivery) {
			_, err := s.f.db.Exec(ctx, `UPDATE deliveries SET cancel_requested = true WHERE id = $1`, d.ID)
			s.Require().NoError(err)
		}},
		{name: "skips an in-flight delivery", due: past, prepare: func(_ *models.Endpoint, d *models.Delivery) {
			_, err := s.f.deliveries.ClaimDueDeliveries(ctx, 10, "other-worker", time.Minute)
			s.Require().NoError(err)
		}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
			m := s.f.seedMessage(&s.Suite, app.ID, "k1")
			d := s.f.seedDelivery(&s.Suite, m.ID, e.ID, tt.due)
			if tt.prepare != nil {
				tt.prepare(e, d)
			}
			got, err := s.f.deliveries.ClaimDueDeliveries(ctx, 10, "worker-1", time.Minute)
			s.Require().NoError(err)
			s.Require().Len(got, tt.want)
			if tt.want == 1 {
				s.Equal(models.DeliveryInFlight, got[0].State)
				s.Equal(int32(1), got[0].AttemptCount)
				s.Equal("worker-1", *got[0].LeaseToken)
				s.True(got[0].LeaseExpiresAt.After(time.Now()))
			}
		})
	}
	s.Run("honours the limit in due order", func() {
		app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
		for i, key := range []string{"a", "b", "c"} {
			m := s.f.seedMessage(&s.Suite, app.ID, key)
			s.f.seedDelivery(&s.Suite, m.ID, e.ID, past.Add(time.Duration(i)*time.Second))
		}
		got, err := s.f.deliveries.ClaimDueDeliveries(ctx, 2, "worker-1", time.Minute)
		s.Require().NoError(err)
		s.Require().Len(got, 2)
		s.Less(got[0].ID, got[1].ID)
		rest, err := s.f.deliveries.ClaimDueDeliveries(ctx, 2, "worker-2", time.Minute)
		s.Require().NoError(err)
		s.Len(rest, 1)
	})
}

func (s *DeliveryRepoTestSuite) TestRecordOutcome() {
	ctx := context.Background()
	next := time.Now().Add(15 * time.Second)
	reason := "http_500"
	tests := []struct {
		name    string
		outcome func(d *models.Delivery) models.DeliveryOutcome
		wantErr error
		check   func(got *models.Delivery)
	}{
		{name: "success ends the cycle", outcome: func(d *models.Delivery) models.DeliveryOutcome {
			return models.DeliveryOutcome{DeliveryID: d.ID, LeaseToken: *d.LeaseToken, State: models.DeliverySucceeded}
		}, check: func(got *models.Delivery) {
			s.Equal(models.DeliverySucceeded, got.State)
			s.NotNil(got.CompletedAt)
			s.Nil(got.LeaseToken)
			s.Nil(got.DueAt)
		}},
		{name: "retry schedules the next attempt", outcome: func(d *models.Delivery) models.DeliveryOutcome {
			return models.DeliveryOutcome{DeliveryID: d.ID, LeaseToken: *d.LeaseToken, State: models.DeliveryRetryScheduled, DueAt: &next, FailureReason: &reason}
		}, check: func(got *models.Delivery) {
			s.Equal(models.DeliveryRetryScheduled, got.State)
			s.WithinDuration(next, *got.DueAt, time.Second)
			s.Equal(reason, *got.FailureReason)
			s.Nil(got.CompletedAt)
		}},
		{name: "a lost lease is not found", outcome: func(d *models.Delivery) models.DeliveryOutcome {
			return models.DeliveryOutcome{DeliveryID: d.ID, LeaseToken: "stale", State: models.DeliverySucceeded}
		}, wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
			m := s.f.seedMessage(&s.Suite, app.ID, "k1")
			s.f.seedDelivery(&s.Suite, m.ID, e.ID, time.Now().Add(-time.Second))
			claimed, err := s.f.deliveries.ClaimDueDeliveries(ctx, 1, "worker-1", time.Minute)
			s.Require().NoError(err)
			s.Require().Len(claimed, 1)

			err = s.f.deliveries.RecordOutcome(ctx, tt.outcome(&claimed[0]))
			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			got, err := s.f.deliveries.GetDelivery(ctx, ws, claimed[0].ID)
			s.Require().NoError(err)
			tt.check(got)
		})
	}
}
