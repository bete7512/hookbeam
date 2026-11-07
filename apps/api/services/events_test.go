package services_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/scaffold/apps/api/models"
	"github.com/bete7512/scaffold/apps/api/repos/repo_mocks"
	"github.com/bete7512/scaffold/apps/api/services"
	"github.com/bete7512/scaffold/pkg/events"
	"github.com/bete7512/scaffold/pkg/events/memory"
)

type EventServiceSuite struct {
	suite.Suite
	outbox    *repo_mocks.MockOutboxRepo
	processed *repo_mocks.MockProcessedEventRepo
	bus       *memory.Bus
}

func TestEventServiceSuite(t *testing.T) {
	suite.Run(t, new(EventServiceSuite))
}

func (s *EventServiceSuite) SetupSubTest() {
	ctrl := gomock.NewController(s.T())
	s.outbox = repo_mocks.NewMockOutboxRepo(ctrl)
	s.outbox.EXPECT().WithTx(gomock.Any()).Return(s.outbox).AnyTimes()
	s.processed = repo_mocks.NewMockProcessedEventRepo(ctrl)
	s.processed.EXPECT().WithTx(gomock.Any()).Return(s.processed).AnyTimes()
	s.bus = memory.New(100)
}

func (s *EventServiceSuite) newService(pub events.Publisher, batchSize int) services.EventService {
	svc, err := services.NewEventService(services.EventServiceDeps{
		Tx: fakeTx{}, Outbox: s.outbox, Processed: s.processed, Publisher: pub, BatchSize: batchSize, Retention: 7 * 24 * time.Hour, Logger: slog.New(slog.DiscardHandler),
	})
	s.Require().NoError(err)
	return svc
}

// failingPublisher rejects every batch.
type failingPublisher struct{ err error }

func (p failingPublisher) Publish(context.Context, []events.Event) error { return p.err }

type waitingPublisher struct{}

func (waitingPublisher) Publish(ctx context.Context, _ []events.Event) error {
	<-ctx.Done()
	return ctx.Err()
}

func (s *EventServiceSuite) TestPublishTimeout() {
	s.Run("blocked broker leaves the batch unpublished", func() {
		s.outbox.EXPECT().ClaimOutboxEvents(gomock.Any(), 1).Return([]models.OutboxEvent{{ID: 1, Type: "user.registered"}}, nil)
		svc, err := services.NewEventService(services.EventServiceDeps{
			Tx: fakeTx{}, Outbox: s.outbox, Processed: s.processed, Publisher: waitingPublisher{}, BatchSize: 1,
			Retention: time.Hour, PublishTimeout: 20 * time.Millisecond, Logger: slog.New(slog.DiscardHandler),
		})
		s.Require().NoError(err)
		n, err := svc.Publish(context.Background())
		s.Zero(n)
		s.Require().ErrorIs(err, context.DeadlineExceeded)
	})
}

func (s *EventServiceSuite) TestProcessOnceInvalidID() {
	s.Run("missing id never creates a replay marker", func() {
		fresh, err := s.newService(s.bus, 1).ProcessOnce(context.Background(), 0, func(context.Context) error {
			s.Fail("handler must not run")
			return nil
		})
		s.False(fresh)
		s.Require().Error(err)
	})
}

func (s *EventServiceSuite) TestPublish() {
	boom := errors.New("boom")
	batch := func(ids ...int64) []models.OutboxEvent {
		out := make([]models.OutboxEvent, 0, len(ids))
		for _, id := range ids {
			out = append(out, models.OutboxEvent{ID: id, Type: "user.registered", Payload: []byte(`{}`)})
		}
		return out
	}
	tests := []struct {
		name       string
		batches    [][]models.OutboxEvent // what each claim returns
		claimErrAt int                    // 1-based claim that fails; 0 = none
		publishErr error
		markErr    error
		want       int
		wantErr    error
	}{
		{name: "one short batch", batches: [][]models.OutboxEvent{batch(1, 2)}, want: 2},
		{name: "full batches loop until a short one", batches: [][]models.OutboxEvent{batch(1, 2, 3), batch(4, 5, 6), batch(7)}, want: 7},
		{name: "empty outbox publishes nothing", batches: [][]models.OutboxEvent{nil}, want: 0},
		{name: "claim failure", batches: [][]models.OutboxEvent{nil}, claimErrAt: 1, wantErr: boom},
		{name: "publish failure marks nothing", batches: [][]models.OutboxEvent{batch(1, 2)}, publishErr: boom, wantErr: boom},
		{name: "mark failure", batches: [][]models.OutboxEvent{batch(1, 2)}, markErr: boom, wantErr: boom},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			var pub events.Publisher = s.bus
			if tt.publishErr != nil {
				pub = failingPublisher{err: tt.publishErr}
			}
			for i, b := range tt.batches {
				var claimErr error
				if tt.claimErrAt == i+1 {
					claimErr = boom
				}
				s.outbox.EXPECT().ClaimOutboxEvents(gomock.Any(), 3).Return(b, claimErr)
				if claimErr == nil && tt.publishErr == nil && len(b) > 0 {
					ids := make([]int64, 0, len(b))
					for _, e := range b {
						ids = append(ids, e.ID)
					}
					s.outbox.EXPECT().MarkOutboxEventsPublished(gomock.Any(), ids).Return(tt.markErr)
				}
			}

			n, err := s.newService(pub, 3).Publish(context.Background())

			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Equal(tt.want, n)
			s.Equal(tt.want, s.bus.Len())
		})
	}
}

func (s *EventServiceSuite) TestCleanOutbox() {
	boom := errors.New("boom")
	tests := []struct {
		name    string
		deleted []int64 // rows deleted per call
		errAt   int     // 1-based call that fails; 0 = none
		want    int64
		wantErr error
	}{
		{name: "one short batch", deleted: []int64{3}, want: 3},
		{name: "loops until a short batch", deleted: []int64{10, 10, 4}, want: 24},
		{name: "nothing to delete", deleted: []int64{0}, want: 0},
		{name: "failure keeps the count so far", deleted: []int64{10, 0}, errAt: 2, want: 10, wantErr: boom},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			for i, n := range tt.deleted {
				var err error
				if tt.errAt == i+1 {
					err = boom
				}
				s.outbox.EXPECT().DeleteOutboxEventsPublishedBefore(gomock.Any(), gomock.Any(), 10).Return(n, err)
			}

			got, err := s.newService(s.bus, 10).CleanOutbox(context.Background())

			s.Equal(tt.want, got)
			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
		})
	}
}

func (s *EventServiceSuite) TestProcessOnce() {
	boom := errors.New("boom")
	tests := []struct {
		name      string
		fresh     bool
		markErr   error
		fnErr     error
		wantRan   bool
		wantFresh bool
		wantErr   error
	}{
		{name: "new event runs fn", fresh: true, wantRan: true, wantFresh: true},
		{name: "replay skips fn", fresh: false, wantRan: false, wantFresh: false},
		{name: "mark failure", markErr: boom, wantErr: boom},
		{name: "fn failure is returned", fresh: true, fnErr: boom, wantRan: true, wantErr: boom},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.processed.EXPECT().MarkEventProcessed(gomock.Any(), int64(42)).Return(tt.fresh, tt.markErr)
			ran := false

			fresh, err := s.newService(s.bus, 10).ProcessOnce(context.Background(), 42, func(context.Context) error {
				ran = true
				return tt.fnErr
			})

			s.Equal(tt.wantRan, ran)
			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Equal(tt.wantFresh, fresh)
		})
	}
}
