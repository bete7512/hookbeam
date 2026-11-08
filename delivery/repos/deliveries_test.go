//go:build integration

package repos_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/suite"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/db"
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

func (s *DeliveryRepoTestSuite) TestBacklogGauges() {
	s.Run("oldest due and in-flight counts", func() {
		ctx := context.Background()
		at, err := s.f.deliveries.OldestDueAt(ctx)
		s.Require().NoError(err)
		s.Nil(at, "nothing due")
		n, err := s.f.deliveries.CountInFlight(ctx)
		s.Require().NoError(err)
		s.Zero(n)
		app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
		m1 := s.f.seedMessage(&s.Suite, app.ID, "k1")
		m2 := s.f.seedMessage(&s.Suite, app.ID, "k2")
		m3 := s.f.seedMessage(&s.Suite, app.ID, "k3")
		old := s.f.seedDelivery(&s.Suite, m1.ID, e.ID, time.Now().Add(-2*time.Minute))
		s.f.seedDelivery(&s.Suite, m2.ID, e.ID, time.Now().Add(-time.Minute))
		s.f.seedDelivery(&s.Suite, m3.ID, e.ID, time.Now().Add(time.Hour)) // not due yet
		_, err = s.f.db.Exec(ctx, `UPDATE deliveries SET state = 'in_flight', lease_token = 'x', lease_expires_at = now() + interval '1 minute' WHERE id = $1`, old.ID)
		s.Require().NoError(err)
		at, err = s.f.deliveries.OldestDueAt(ctx)
		s.Require().NoError(err)
		s.Require().NotNil(at)
		s.WithinDuration(time.Now().Add(-time.Minute), *at, 5*time.Second, "the claimed one no longer counts; the future one is not due")
		n, err = s.f.deliveries.CountInFlight(ctx)
		s.Require().NoError(err)
		s.Equal(1, n)
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
		{name: "skips a delivery past its deadline", due: past, prepare: func(_ *models.Endpoint, d *models.Delivery) {
			_, err := s.f.db.Exec(ctx, `UPDATE deliveries SET deadline_at = now() - interval '1 second' WHERE id = $1`, d.ID)
			s.Require().NoError(err)
		}},
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
			_, err := s.f.deliveries.ClaimDueDeliveries(ctx, 10, 10, "other-worker", time.Minute)
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
			got, err := s.f.deliveries.ClaimDueDeliveries(ctx, 10, 10, "worker-1", time.Minute)
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
		got, err := s.f.deliveries.ClaimDueDeliveries(ctx, 2, 10, "worker-1", time.Minute)
		s.Require().NoError(err)
		s.Require().Len(got, 2)
		s.Less(got[0].ID, got[1].ID)
		rest, err := s.f.deliveries.ClaimDueDeliveries(ctx, 2, 10, "worker-2", time.Minute)
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
		cancel  bool
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
		{name: "a cancel requested during the attempt ends a retry as cancelled", cancel: true, outcome: func(d *models.Delivery) models.DeliveryOutcome {
			return models.DeliveryOutcome{DeliveryID: d.ID, LeaseToken: *d.LeaseToken, State: models.DeliveryRetryScheduled, DueAt: &next, FailureReason: &reason}
		}, check: func(got *models.Delivery) {
			s.Equal(models.DeliveryCancelled, got.State)
			s.Equal("cancelled", *got.FailureReason)
			s.Nil(got.DueAt)
			s.NotNil(got.CompletedAt)
		}},
		{name: "a cancel requested during the attempt still records a success", cancel: true, outcome: func(d *models.Delivery) models.DeliveryOutcome {
			return models.DeliveryOutcome{DeliveryID: d.ID, LeaseToken: *d.LeaseToken, State: models.DeliverySucceeded}
		}, check: func(got *models.Delivery) {
			s.Equal(models.DeliverySucceeded, got.State)
			s.Nil(got.FailureReason)
		}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
			m := s.f.seedMessage(&s.Suite, app.ID, "k1")
			s.f.seedDelivery(&s.Suite, m.ID, e.ID, time.Now().Add(-time.Second))
			claimed, err := s.f.deliveries.ClaimDueDeliveries(ctx, 1, 10, "worker-1", time.Minute)
			s.Require().NoError(err)
			s.Require().Len(claimed, 1)
			if tt.cancel {
				_, err := s.f.deliveries.CancelDelivery(ctx, ws, claimed[0].ID)
				s.Require().NoError(err)
			}

			state, err := s.f.deliveries.RecordOutcome(ctx, tt.outcome(&claimed[0]))
			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			got, err := s.f.deliveries.GetDelivery(ctx, ws, claimed[0].ID)
			s.Require().NoError(err)
			s.Equal(got.State, state, "the stored state is returned")
			tt.check(got)
		})
	}
}

// claimExpired claims a due delivery under a lease that has already run out.
func (s *DeliveryRepoTestSuite) claimExpired(ttl time.Duration) models.Delivery {
	s.T().Helper()
	app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
	m := s.f.seedMessage(&s.Suite, app.ID, "k1")
	s.f.seedDelivery(&s.Suite, m.ID, e.ID, time.Now().Add(-time.Second))
	claimed, err := s.f.deliveries.ClaimDueDeliveries(context.Background(), 1, 10, "worker-1", ttl)
	s.Require().NoError(err)
	s.Require().Len(claimed, 1)
	return claimed[0]
}

func (s *DeliveryRepoTestSuite) TestClaimExpiredLeases() {
	ctx := context.Background()
	s.Run("re-leases an expired in_flight delivery and reports when the lease ended", func() {
		d := s.claimExpired(-time.Minute)

		got, err := s.f.deliveries.ClaimExpiredLeases(ctx, 10, "sweeper-1", time.Minute)

		s.Require().NoError(err)
		s.Require().Len(got, 1)
		s.Equal(d.ID, got[0].ID)
		s.Equal(models.DeliveryInFlight, got[0].State)
		s.Equal(int32(1), got[0].AttemptCount, "the lost attempt stays counted")
		s.Equal("sweeper-1", *got[0].LeaseToken)
		s.True(got[0].LeaseExpiresAt.After(time.Now()))
		s.WithinDuration(*d.LeaseExpiresAt, got[0].LeaseExpiredAt, time.Second)
		s.Require().NoError(recordOutcome(s.f.deliveries, ctx, models.DeliveryOutcome{DeliveryID: d.ID, LeaseToken: "sweeper-1", State: models.DeliveryFailed}), "the new token fences the outcome")
	})
	s.Run("skips a live lease", func() {
		s.claimExpired(time.Minute)
		got, err := s.f.deliveries.ClaimExpiredLeases(ctx, 10, "sweeper-1", time.Minute)
		s.Require().NoError(err)
		s.Empty(got)
	})
	s.Run("skips deliveries that are not in flight", func() {
		app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
		m := s.f.seedMessage(&s.Suite, app.ID, "k1")
		s.f.seedDelivery(&s.Suite, m.ID, e.ID, time.Now().Add(-time.Hour))
		got, err := s.f.deliveries.ClaimExpiredLeases(ctx, 10, "sweeper-1", time.Minute)
		s.Require().NoError(err)
		s.Empty(got)
	})
	s.Run("two concurrent sweepers recover it exactly once", func() {
		s.claimExpired(-time.Minute)
		results := make(chan int, 2)
		for _, token := range []string{"sweeper-1", "sweeper-2"} {
			go func() {
				got, err := s.f.deliveries.ClaimExpiredLeases(ctx, 10, token, time.Minute)
				s.NoError(err)
				results <- len(got)
			}()
		}
		s.Equal(1, <-results+<-results)
	})
}

func (s *DeliveryRepoTestSuite) TestReleaseDelivery() {
	ctx := context.Background()
	tests := []struct {
		name      string
		token     string
		wantErr   error
		wantState string
	}{
		{name: "first claim goes back to queued", token: "worker-1", wantState: models.DeliveryQueued},
		{name: "wrong token is not found", token: "stale", wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			d := s.claimExpired(time.Minute)

			err := s.f.deliveries.ReleaseDelivery(ctx, d.ID, tt.token)

			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			got, err := s.f.deliveries.GetDelivery(ctx, ws, d.ID)
			s.Require().NoError(err)
			s.Equal(tt.wantState, got.State)
			s.Equal(int32(0), got.AttemptCount)
			s.Nil(got.LeaseToken)
			s.WithinDuration(*d.LeaseExpiresAt, *got.DueAt, time.Second, "due again when the lease would have run out")
		})
	}
	s.Run("a cancel requested meanwhile ends it as cancelled", func() {
		d := s.claimExpired(time.Minute)
		_, err := s.f.deliveries.CancelDelivery(ctx, ws, d.ID)
		s.Require().NoError(err)

		s.Require().NoError(s.f.deliveries.ReleaseDelivery(ctx, d.ID, "worker-1"))

		got, err := s.f.deliveries.GetDelivery(ctx, ws, d.ID)
		s.Require().NoError(err)
		s.Equal(models.DeliveryCancelled, got.State)
		s.Equal("cancelled", *got.FailureReason)
		s.Equal(int32(0), got.AttemptCount)
	})
	s.Run("a later claim goes back to retry_scheduled", func() {
		d := s.claimExpired(time.Minute)
		reason := "http_503"
		s.Require().NoError(recordOutcome(s.f.deliveries, ctx, models.DeliveryOutcome{DeliveryID: d.ID, LeaseToken: "worker-1", State: models.DeliveryRetryScheduled, DueAt: d.DueAt, FailureReason: &reason}))
		claimed, err := s.f.deliveries.ClaimDueDeliveries(ctx, 1, 10, "worker-2", time.Minute)
		s.Require().NoError(err)
		s.Require().Len(claimed, 1)
		s.Require().Equal(int32(2), claimed[0].AttemptCount)

		s.Require().NoError(s.f.deliveries.ReleaseDelivery(ctx, d.ID, "worker-2"))

		got, err := s.f.deliveries.GetDelivery(ctx, ws, d.ID)
		s.Require().NoError(err)
		s.Equal(models.DeliveryRetryScheduled, got.State)
		s.Equal(int32(1), got.AttemptCount)
	})
}

func (s *DeliveryRepoTestSuite) TestCancelDelivery() {
	ctx := context.Background()
	tests := []struct {
		name      string
		prepare   func(d *models.Delivery)
		workspace string
		wantErr   error
		wantState string
		wantFlag  bool
	}{
		{name: "queued is cancelled now", workspace: ws, wantState: models.DeliveryCancelled},
		{name: "retry scheduled is cancelled now", workspace: ws, wantState: models.DeliveryCancelled, prepare: func(d *models.Delivery) {
			claimed, err := s.f.deliveries.ClaimDueDeliveries(ctx, 1, 10, "w", time.Minute)
			s.Require().NoError(err)
			reason := "http_503"
			s.Require().NoError(recordOutcome(s.f.deliveries, ctx, models.DeliveryOutcome{DeliveryID: claimed[0].ID, LeaseToken: "w", State: models.DeliveryRetryScheduled, DueAt: d.DueAt, FailureReason: &reason}))
		}},
		{name: "in flight is marked cancel requested and stays in flight", workspace: ws, wantState: models.DeliveryInFlight, wantFlag: true, prepare: func(_ *models.Delivery) {
			_, err := s.f.deliveries.ClaimDueDeliveries(ctx, 1, 10, "w", time.Minute)
			s.Require().NoError(err)
		}},
		{name: "a finished delivery is not found", workspace: ws, wantErr: apperr.ErrNotFound, prepare: func(_ *models.Delivery) {
			claimed, err := s.f.deliveries.ClaimDueDeliveries(ctx, 1, 10, "w", time.Minute)
			s.Require().NoError(err)
			s.Require().NoError(recordOutcome(s.f.deliveries, ctx, models.DeliveryOutcome{DeliveryID: claimed[0].ID, LeaseToken: "w", State: models.DeliverySucceeded}))
		}},
		{name: "another workspace is not found", workspace: "ws_other", wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
			m := s.f.seedMessage(&s.Suite, app.ID, "k1")
			d := s.f.seedDelivery(&s.Suite, m.ID, e.ID, time.Now().Add(-time.Second))
			if tt.prepare != nil {
				tt.prepare(d)
			}

			got, err := s.f.deliveries.CancelDelivery(ctx, tt.workspace, d.ID)

			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Equal(tt.wantState, got.State)
			s.Equal(tt.wantFlag, got.CancelRequested)
			if tt.wantState == models.DeliveryCancelled {
				s.Equal("cancelled", *got.FailureReason)
				s.Nil(got.DueAt)
				s.NotNil(got.CompletedAt)
				claimed, err := s.f.deliveries.ClaimDueDeliveries(ctx, 10, 10, "w2", time.Minute)
				s.Require().NoError(err)
				s.Empty(claimed, "cancelled work is never claimed")
			}
		})
	}
}

func (s *DeliveryRepoTestSuite) TestFindReplay() {
	ctx := context.Background()
	s.Run("replay found by original and key, index refuses a second active cycle", func() {
		app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
		m := s.f.seedMessage(&s.Suite, app.ID, "k1")
		original := s.f.seedDelivery(&s.Suite, m.ID, e.ID, time.Now().Add(-time.Second))
		claimed, err := s.f.deliveries.ClaimDueDeliveries(ctx, 1, 10, "w", time.Minute)
		s.Require().NoError(err)
		reason := "http_410"
		s.Require().NoError(recordOutcome(s.f.deliveries, ctx, models.DeliveryOutcome{DeliveryID: claimed[0].ID, LeaseToken: "w", State: models.DeliveryFailed, FailureReason: &reason}))
		key := "replay-1"
		due := time.Now()
		replay := &models.Delivery{MessageID: m.ID, EndpointID: e.ID, EndpointRevision: 2, State: models.DeliveryQueued, DueAt: &due, DeadlineAt: due.Add(time.Hour), ReplayOfDeliveryID: &original.ID, ReplayIdempotencyKey: &key}
		s.Require().NoError(s.f.deliveries.CreateDelivery(ctx, replay))

		got, err := s.f.deliveries.FindReplay(ctx, original.ID, key)
		s.Require().NoError(err)
		s.Equal(replay.ID, got.ID)
		s.Equal(original.ID, *got.ReplayOfDeliveryID)

		_, err = s.f.deliveries.FindReplay(ctx, original.ID, "other")
		s.Require().ErrorIs(err, apperr.ErrNotFound)

		other := "replay-2"
		second := &models.Delivery{MessageID: m.ID, EndpointID: e.ID, EndpointRevision: 2, State: models.DeliveryQueued, DueAt: &due, DeadlineAt: due.Add(time.Hour), ReplayOfDeliveryID: &original.ID, ReplayIdempotencyKey: &other}
		s.Require().ErrorIs(s.f.deliveries.CreateDelivery(ctx, second), apperr.ErrConflict, "one active cycle per message and endpoint")
	})
}

func (s *DeliveryRepoTestSuite) TestPauseResumeDrainsBacklog() {
	ctx := context.Background()
	s.Run("nothing is claimed while paused, everything after resume", func() {
		app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
		for _, key := range []string{"a", "b"} {
			m := s.f.seedMessage(&s.Suite, app.ID, key)
			s.f.seedDelivery(&s.Suite, m.ID, e.ID, time.Now().Add(-time.Second))
		}
		_, err := s.f.endpoints.PauseEndpoint(ctx, ws, e.ID)
		s.Require().NoError(err)

		got, err := s.f.deliveries.ClaimDueDeliveries(ctx, 10, 10, "w", time.Minute)
		s.Require().NoError(err)
		s.Empty(got)

		_, err = s.f.endpoints.ResumeEndpoint(ctx, ws, e.ID)
		s.Require().NoError(err)
		got, err = s.f.deliveries.ClaimDueDeliveries(ctx, 10, 10, "w", time.Minute)
		s.Require().NoError(err)
		s.Len(got, 2)
	})
}

func (s *DeliveryRepoTestSuite) TestCancelDeliveriesForEndpoint() {
	ctx := context.Background()
	s.Run("queued and retry-scheduled are cancelled, in-flight and other endpoints untouched", func() {
		app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
		_, other := s.f.seedEndpoint(&s.Suite, "Customer B")
		queued := s.f.seedDelivery(&s.Suite, s.f.seedMessage(&s.Suite, app.ID, "q").ID, e.ID, time.Now().Add(time.Hour))
		inFlight := s.f.seedDelivery(&s.Suite, s.f.seedMessage(&s.Suite, app.ID, "f").ID, e.ID, time.Now().Add(-time.Second))
		foreign := s.f.seedDelivery(&s.Suite, s.f.seedMessage(&s.Suite, app.ID, "o").ID, other.ID, time.Now().Add(time.Hour))
		claimed, err := s.f.deliveries.ClaimDueDeliveries(ctx, 1, 10, "w", time.Minute)
		s.Require().NoError(err)
		s.Require().Equal(inFlight.ID, claimed[0].ID)

		n, err := s.f.deliveries.CancelDeliveriesForEndpoint(ctx, e.ID, "endpoint_archived")

		s.Require().NoError(err)
		s.Equal(int64(1), n)
		for _, tc := range []struct {
			id   int64
			want string
		}{{queued.ID, models.DeliveryCancelled}, {inFlight.ID, models.DeliveryInFlight}, {foreign.ID, models.DeliveryQueued}} {
			got, err := s.f.deliveries.GetDelivery(ctx, ws, tc.id)
			s.Require().NoError(err)
			s.Equal(tc.want, got.State)
		}
		got, err := s.f.deliveries.GetDelivery(ctx, ws, queued.ID)
		s.Require().NoError(err)
		s.Equal("endpoint_archived", *got.FailureReason)
		s.Require().NoError(recordOutcome(s.f.deliveries, ctx, models.DeliveryOutcome{DeliveryID: inFlight.ID, LeaseToken: "w", State: models.DeliverySucceeded}), "the in-flight attempt still records")
	})
}

func (s *DeliveryRepoTestSuite) TestExpireDeadlines() {
	ctx := context.Background()
	s.Run("scheduled work past its deadline fails, the rest stays", func() {
		app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
		overdue := s.f.seedDelivery(&s.Suite, s.f.seedMessage(&s.Suite, app.ID, "a").ID, e.ID, time.Now().Add(-time.Second))
		fresh := s.f.seedDelivery(&s.Suite, s.f.seedMessage(&s.Suite, app.ID, "b").ID, e.ID, time.Now().Add(-time.Second))
		_, err := s.f.db.Exec(ctx, `UPDATE deliveries SET deadline_at = now() - interval '1 second' WHERE id = $1`, overdue.ID)
		s.Require().NoError(err)

		n, err := s.f.deliveries.ExpireDeadlines(ctx, 10)

		s.Require().NoError(err)
		s.Equal(int64(1), n)
		got, err := s.f.deliveries.GetDelivery(ctx, ws, overdue.ID)
		s.Require().NoError(err)
		s.Equal(models.DeliveryFailed, got.State)
		s.Equal("deadline_exceeded", *got.FailureReason)
		s.NotNil(got.CompletedAt)
		got, err = s.f.deliveries.GetDelivery(ctx, ws, fresh.ID)
		s.Require().NoError(err)
		s.Equal(models.DeliveryQueued, got.State)
	})
}

// claimLocked claims a batch of ten the way the worker does: lock, then claim, in one transaction.
func (s *DeliveryRepoTestSuite) claimLocked(perEndpoint int, token string) ([]models.Delivery, error) {
	var got []models.Delivery
	err := db.NewTransactor(s.f.db).WithTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		r := s.f.deliveries.WithTx(tx)
		if err := r.LockClaims(ctx); err != nil {
			return err
		}
		var err error
		got, err = r.ClaimDueDeliveries(ctx, 10, perEndpoint, token, time.Minute)
		return err
	})
	return got, err
}

func (s *DeliveryRepoTestSuite) TestFairClaim() {
	ctx := context.Background()
	past := time.Now().Add(-time.Minute)
	s.Run("a noisy endpoint cannot starve a healthy one", func() {
		app, slow := s.f.seedEndpoint(&s.Suite, "Customer A")
		_, healthy := s.f.seedEndpoint(&s.Suite, "Customer B")
		for i := range 50 {
			s.f.seedDelivery(&s.Suite, s.f.seedMessage(&s.Suite, app.ID, fmt.Sprintf("slow-%d", i)).ID, slow.ID, past.Add(time.Duration(i)*time.Millisecond))
		}
		for i := range 5 {
			s.f.seedDelivery(&s.Suite, s.f.seedMessage(&s.Suite, app.ID, fmt.Sprintf("ok-%d", i)).ID, healthy.ID, time.Now().Add(-time.Second))
		}

		first, err := s.claimLocked(2, "w1")
		s.Require().NoError(err)

		byEndpoint := map[int64]int{}
		for _, d := range first {
			byEndpoint[d.EndpointID]++
		}
		s.Equal(2, byEndpoint[healthy.ID], "the healthy endpoint is served in the first batch although it became due last")
		s.Equal(2, byEndpoint[slow.ID], "the cap holds for the noisy one too")

		second, err := s.claimLocked(2, "w2")
		s.Require().NoError(err)
		s.Empty(second, "both endpoints are at their cap until an attempt settles")
		for _, d := range first {
			s.Require().NoError(recordOutcome(s.f.deliveries, ctx, models.DeliveryOutcome{DeliveryID: d.ID, LeaseToken: "w1", State: models.DeliverySucceeded}))
		}
		third, err := s.claimLocked(2, "w3")
		s.Require().NoError(err)
		s.Len(third, 4, "two per endpoint again once the slots free up")
	})
	s.Run("two concurrent claimers never exceed the cap", func() {
		app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
		for i := range 20 {
			s.f.seedDelivery(&s.Suite, s.f.seedMessage(&s.Suite, app.ID, fmt.Sprintf("k-%d", i)).ID, e.ID, past)
		}
		type result struct {
			n   int
			err error
		}
		results := make(chan result, 2)
		for _, token := range []string{"a", "b"} {
			go func() {
				got, err := s.claimLocked(3, token)
				results <- result{len(got), err}
			}()
		}
		total := 0
		for range 2 {
			r := <-results
			s.Require().NoError(r.err)
			total += r.n
		}
		s.Equal(3, total)
		var inFlight int
		s.Require().NoError(s.f.db.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE endpoint_id = $1 AND state = 'in_flight'`, e.ID).Scan(&inFlight))
		s.Equal(3, inFlight)
	})
}

func (s *DeliveryRepoTestSuite) TestCountActiveDeliveries() {
	s.Run("counts non-terminal deliveries of the workspace, stops at the bound, and reads the partial index", func() {
		ctx := context.Background()
		app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
		for i := range 4 {
			m := s.f.seedMessage(&s.Suite, app.ID, fmt.Sprintf("k%d", i))
			s.f.seedDelivery(&s.Suite, m.ID, e.ID, time.Now())
		}
		_, err := s.f.db.Exec(ctx, `UPDATE deliveries SET state = 'succeeded' WHERE id = (SELECT min(id) FROM deliveries)`)
		s.Require().NoError(err)
		_, err = s.f.db.Exec(ctx, `UPDATE deliveries SET state = 'in_flight' WHERE id = (SELECT max(id) FROM deliveries)`)
		s.Require().NoError(err)

		n, err := s.f.deliveries.CountActiveDeliveries(ctx, ws, 100)
		s.Require().NoError(err)
		s.Equal(3, n, "queued, queued and in_flight count; succeeded does not")
		n, err = s.f.deliveries.CountActiveDeliveries(ctx, ws, 2)
		s.Require().NoError(err)
		s.Equal(2, n, "never more than the bound")
		n, err = s.f.deliveries.CountActiveDeliveries(ctx, "ws_other", 100)
		s.Require().NoError(err)
		s.Zero(n)

		var plan string
		s.Require().NoError(s.f.db.QueryRow(ctx, `EXPLAIN (FORMAT TEXT) SELECT count(*) FROM (SELECT 1 FROM deliveries WHERE workspace_id = 'ws_1' AND state IN ('queued', 'in_flight', 'retry_scheduled') LIMIT 100) active`).Scan(&plan))
		_, err = s.f.db.Exec(ctx, `SET enable_seqscan = off`)
		s.Require().NoError(err)
		rows, err := s.f.db.Query(ctx, `EXPLAIN SELECT 1 FROM deliveries WHERE workspace_id = 'ws_1' AND state IN ('queued', 'in_flight', 'retry_scheduled') LIMIT 100`)
		s.Require().NoError(err)
		var lines []string
		for rows.Next() {
			var l string
			s.Require().NoError(rows.Scan(&l))
			lines = append(lines, l)
		}
		rows.Close()
		_, err = s.f.db.Exec(ctx, `RESET enable_seqscan`)
		s.Require().NoError(err)
		s.Contains(strings.Join(lines, "\n"), "deliveries_workspace_active_idx", "the backlog count is index-backed")
	})
}
