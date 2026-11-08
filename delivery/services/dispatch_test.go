package services_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	apievents "github.com/bete7512/hookbeam/delivery/events"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos/repo_mocks"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/delivery/services/service_mocks"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/secrets"
	"github.com/bete7512/hookbeam/pkg/webhooks"
)

// fakeSender records the request and answers a canned result; block, when set, makes every call wait until
// that many calls are in progress, which proves the batch is attempted concurrently.
type fakeSender struct {
	mu    sync.Mutex
	got   []services.SendRequest
	res   services.SendResult
	block *sync.WaitGroup
}

func (f *fakeSender) Send(_ context.Context, req services.SendRequest) services.SendResult {
	f.mu.Lock()
	f.got = append(f.got, req)
	f.mu.Unlock()
	if f.block != nil {
		f.block.Done()
		f.block.Wait()
	}
	return f.res
}

type DispatchServiceSuite struct {
	suite.Suite
	deliveries *repo_mocks.MockDeliveryRepo
	attempts   *repo_mocks.MockAttemptRepo
	messages   *repo_mocks.MockMessageRepo
	endpoints  *repo_mocks.MockEndpointRepo
	secrets    *repo_mocks.MockEndpointSecretRepo
	outbox     *repo_mocks.MockOutboxRepo
	health     *service_mocks.MockHealthService
	obsMu      sync.Mutex
	observed   []models.HealthObservation
	cipher     *secrets.Cipher
	sender     *fakeSender
	svc        services.DispatchService
	secret     string
}

func TestDispatchServiceSuite(t *testing.T) {
	suite.Run(t, new(DispatchServiceSuite))
}

func (s *DispatchServiceSuite) SetupSubTest() {
	ctrl := gomock.NewController(s.T())
	s.deliveries = repo_mocks.NewMockDeliveryRepo(ctrl)
	s.deliveries.EXPECT().WithTx(gomock.Any()).Return(s.deliveries).AnyTimes()
	s.deliveries.EXPECT().LockClaims(gomock.Any()).Return(nil).AnyTimes()
	s.attempts = repo_mocks.NewMockAttemptRepo(ctrl)
	s.attempts.EXPECT().WithTx(gomock.Any()).Return(s.attempts).AnyTimes()
	s.messages = repo_mocks.NewMockMessageRepo(ctrl)
	s.endpoints = repo_mocks.NewMockEndpointRepo(ctrl)
	s.secrets = repo_mocks.NewMockEndpointSecretRepo(ctrl)
	s.outbox = repo_mocks.NewMockOutboxRepo(ctrl)
	s.outbox.EXPECT().WithTx(gomock.Any()).Return(s.outbox).AnyTimes()
	s.health = service_mocks.NewMockHealthService(ctrl)
	s.observed = nil
	s.health.EXPECT().Observe(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ db.DBTX, o models.HealthObservation) error {
		s.obsMu.Lock()
		defer s.obsMu.Unlock()
		s.observed = append(s.observed, o)
		return nil
	}).AnyTimes()
	s.sender = &fakeSender{}
	key := make([]byte, 32)
	_, err := rand.Read(key)
	s.Require().NoError(err)
	s.cipher, err = secrets.New(base64.StdEncoding.EncodeToString(key))
	s.Require().NoError(err)
	s.secret = "whsec_" + base64.StdEncoding.EncodeToString(key)
	policy := services.DefaultPolicy
	policy.Jitter = 0
	s.svc, err = services.NewDispatchService(services.DispatchServiceDeps{
		Tx: fakeTx{}, Deliveries: s.deliveries, Attempts: s.attempts, Messages: s.messages, Endpoints: s.endpoints, Secrets: s.secrets, Outbox: s.outbox, Health: s.health,
		Cipher: s.cipher, Sender: s.sender, Policy: policy, LeaseTTL: time.Minute, BatchSize: 10, EndpointConcurrency: 2, Concurrency: 4, Logger: slog.New(slog.DiscardHandler),
	})
	s.Require().NoError(err)
}

// observations returns a snapshot of what the health service was told.
func (s *DispatchServiceSuite) observations() []models.HealthObservation {
	s.obsMu.Lock()
	defer s.obsMu.Unlock()
	return append([]models.HealthObservation(nil), s.observed...)
}

func (s *DispatchServiceSuite) TestNew() {
	_, err := services.NewDispatchService(services.DispatchServiceDeps{Tx: fakeTx{}})
	s.Error(err)
}

func (s *DispatchServiceSuite) claimed(attempt int32) models.Delivery {
	token := "lease-1"
	return models.Delivery{ID: 5, MessageID: 42, EndpointID: 3, EndpointRevision: 1, State: models.DeliveryInFlight, AttemptCount: attempt, LeaseToken: &token, DeadlineAt: time.Now().Add(24 * time.Hour)}
}

func (s *DispatchServiceSuite) encrypted(version int32) models.EndpointSecret {
	ct, err := s.cipher.Encrypt([]byte(s.secret))
	s.Require().NoError(err)
	return models.EndpointSecret{EndpointID: 3, Version: version, Ciphertext: ct}
}

func (s *DispatchServiceSuite) expectLoads(endpoint *models.Endpoint, secretVersions ...int32) {
	s.messages.EXPECT().GetMessageByID(gomock.Any(), int64(42)).Return(&models.Message{ID: 42, WorkspaceID: ws, ApplicationID: 7, PublicID: "msg_abc", Payload: []byte(body)}, nil)
	s.endpoints.EXPECT().GetEndpointByID(gomock.Any(), int64(3)).Return(endpoint, nil)
	if endpoint.ArchivedAt == nil {
		active := make([]models.EndpointSecret, 0, len(secretVersions))
		for _, v := range secretVersions {
			active = append(active, s.encrypted(v))
		}
		s.secrets.EXPECT().ListActiveEndpointSecrets(gomock.Any(), int64(3)).Return(active, nil)
	}
}

func (s *DispatchServiceSuite) TestDispatch() {
	endpoint := &models.Endpoint{ID: 3, URL: "https://receiver.example.com/hooks", Revision: 2}
	now := time.Now()
	archived := &models.Endpoint{ID: 3, URL: endpoint.URL, Revision: 2, ArchivedAt: &now}
	tests := []struct {
		name        string
		delivery    models.Delivery
		res         services.SendResult
		setup       func()
		wantSent    bool
		wantState   string
		wantReason  string
		wantAttempt func(a *models.Attempt)
		wantHealth  string // "" no observation, "ok" a success, "failed" a counted failure
	}{
		{name: "2xx succeeds and records the attempt", delivery: s.claimed(1), res: services.SendResult{StatusCode: 204, Headers: map[string]string{"server": "x"}}, wantSent: true, wantState: models.DeliverySucceeded, wantHealth: "ok", setup: func() {
			s.expectLoads(endpoint, 2, 1)
		}, wantAttempt: func(a *models.Attempt) {
			s.Equal(int32(1), a.Number)
			s.Equal(int32(2), a.EndpointRevision)
			s.Equal(int32(2), a.SecretVersion, "newest active secret")
			s.Equal(int32(204), *a.StatusCode)
			s.Nil(a.ErrorClass)
			s.Equal("x", a.ResponseHeaders["server"])
		}},
		{name: "503 schedules a retry", delivery: s.claimed(1), res: services.SendResult{StatusCode: 503, BodyPreview: "later"}, wantSent: true, wantState: models.DeliveryRetryScheduled, wantReason: "http_503", wantHealth: "failed", setup: func() {
			s.expectLoads(endpoint, 1)
		}, wantAttempt: func(a *models.Attempt) { s.Equal("later", *a.ResponsePreview) }},
		{name: "timeout on the last attempt exhausts", delivery: s.claimed(8), res: services.SendResult{ErrorClass: "timeout", Err: errors.New("deadline")}, wantSent: true, wantState: models.DeliveryFailed, wantReason: services.ReasonAttemptsExhausted, wantHealth: "failed", setup: func() {
			s.expectLoads(endpoint, 1)
		}, wantAttempt: func(a *models.Attempt) {
			s.Nil(a.StatusCode)
			s.Equal("timeout", *a.ErrorClass)
		}},
		{name: "archived endpoint cancels without sending", delivery: s.claimed(1), wantState: models.DeliveryCancelled, wantReason: services.ReasonEndpointArchived, setup: func() {
			s.expectLoads(archived)
		}},
		{name: "lost lease is logged, not an error", delivery: s.claimed(1), res: services.SendResult{StatusCode: 200}, wantSent: true, setup: func() {
			s.expectLoads(endpoint, 1)
			s.attempts.EXPECT().CreateAttempt(gomock.Any(), gomock.Any()).Return(nil)
			s.deliveries.EXPECT().RecordOutcome(gomock.Any(), gomock.Any()).Return("", apperr.ErrNotFound)
		}},
		{name: "load failure releases the lease", delivery: s.claimed(1), setup: func() {
			s.messages.EXPECT().GetMessageByID(gomock.Any(), int64(42)).Return(nil, errors.New("boom"))
			s.deliveries.EXPECT().ReleaseDelivery(gomock.Any(), int64(5), gomock.Any()).Return(nil)
		}},
		{name: "load failure with the lease already gone", delivery: s.claimed(1), setup: func() {
			s.messages.EXPECT().GetMessageByID(gomock.Any(), int64(42)).Return(nil, errors.New("boom"))
			s.deliveries.EXPECT().ReleaseDelivery(gomock.Any(), int64(5), gomock.Any()).Return(apperr.ErrNotFound)
		}},
		{name: "no active secret releases the lease", delivery: s.claimed(1), setup: func() {
			s.expectLoads(endpoint)
			s.deliveries.EXPECT().ReleaseDelivery(gomock.Any(), int64(5), gomock.Any()).Return(nil)
		}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.sender.res = tt.res
			s.deliveries.EXPECT().ClaimDueDeliveries(gomock.Any(), 4, 2, gomock.Any(), time.Minute).Return([]models.Delivery{tt.delivery}, nil)
			tt.setup()
			var recorded *models.Attempt
			if tt.wantState != "" {
				if tt.wantSent {
					s.attempts.EXPECT().CreateAttempt(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, a *models.Attempt) error {
						recorded = a
						return nil
					})
				}
				s.deliveries.EXPECT().RecordOutcome(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, o models.DeliveryOutcome) (string, error) {
					s.Equal(int64(5), o.DeliveryID)
					s.Equal(tt.wantState, o.State)
					if tt.wantReason != "" {
						s.Equal(tt.wantReason, *o.FailureReason)
					}
					if tt.wantState == models.DeliveryRetryScheduled {
						s.NotNil(o.DueAt)
					}
					return o.State, nil
				})
				s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.OutboxEvent) error {
					s.Equal("delivery."+tt.wantState, e.Type)
					var p apievents.DeliveryStatePayload
					s.Require().NoError(json.Unmarshal(e.Payload, &p))
					s.Equal(int64(5), p.DeliveryID)
					s.Equal(int64(3), p.EndpointID)
					s.Equal(int64(42), p.MessageID)
					s.Equal(tt.wantState, p.State)
					return nil
				})
			}

			n, err := s.svc.Dispatch(context.Background())
			s.svc.Drain()

			s.Equal(1, n)
			observed := s.observations()
			switch tt.wantHealth {
			case "":
				s.Empty(observed, "no network verdict, no health observation")
			default:
				s.Require().Len(observed, 1)
				s.Equal(tt.wantHealth == "failed", observed[0].Failed)
				s.Equal(int64(3), observed[0].EndpointID)
				s.Equal(int32(2), observed[0].Revision, "the revision the attempt used")
				s.Equal(ws, observed[0].WorkspaceID)
			}
			s.Require().NoError(err)
			if !tt.wantSent {
				s.Empty(s.sender.got)
				return
			}
			s.Require().Len(s.sender.got, 1)
			req := s.sender.got[0]
			s.Equal(endpoint.URL, req.URL)
			s.JSONEq(body, string(req.Body))
			s.Equal("msg_abc", req.Headers[webhooks.HeaderID])
			s.Equal("application/json", req.Headers["content-type"])
			ts, err := strconv.ParseInt(req.Headers[webhooks.HeaderTimestamp], 10, 64)
			s.Require().NoError(err)
			s.WithinDuration(time.Now(), time.Unix(ts, 0), 5*time.Second)
			h := http.Header{}
			for k, v := range req.Headers {
				h.Set(k, v)
			}
			s.Require().NoError(webhooks.Verify([]string{s.secret}, h, req.Body, time.Now(), time.Minute), "receiver can verify the signature")
			if tt.wantAttempt != nil {
				s.Require().NotNil(recorded)
				tt.wantAttempt(recorded)
			}
		})
	}
	s.Run("claim failure", func() {
		s.deliveries.EXPECT().ClaimDueDeliveries(gomock.Any(), 4, 2, gomock.Any(), time.Minute).Return(nil, errors.New("boom"))
		_, err := s.svc.Dispatch(context.Background())
		requireErr(&s.Suite, err, 500, nil)
	})
	s.Run("a batch is attempted concurrently, so one stalling destination does not hold up the rest", func() {
		s.sender.res = services.SendResult{StatusCode: 200}
		s.sender.block = &sync.WaitGroup{}
		s.sender.block.Add(3)
		batch := []models.Delivery{s.claimed(1), s.claimed(1), s.claimed(1)}
		s.deliveries.EXPECT().ClaimDueDeliveries(gomock.Any(), 4, 2, gomock.Any(), time.Minute).Return(batch, nil)
		for range batch {
			s.expectLoads(endpoint, 1)
		}
		s.attempts.EXPECT().CreateAttempt(gomock.Any(), gomock.Any()).Return(nil).Times(3)
		s.deliveries.EXPECT().RecordOutcome(gomock.Any(), gomock.Any()).Return(models.DeliverySucceeded, nil).Times(3)
		s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).Return(nil).Times(3)
		n, err := s.svc.Dispatch(context.Background())
		s.Require().NoError(err)
		s.Equal(3, n)
		done := make(chan struct{})
		go func() { s.svc.Drain(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			s.Fail("the three attempts never ran at the same time")
		}
		s.Len(s.sender.got, 3)
	})
	s.Run("claims only as many as there are free slots", func() {
		s.sender.res = services.SendResult{StatusCode: 200}
		s.sender.block = &sync.WaitGroup{}
		s.sender.block.Add(4)
		batch := []models.Delivery{s.claimed(1), s.claimed(1), s.claimed(1), s.claimed(1)}
		s.deliveries.EXPECT().ClaimDueDeliveries(gomock.Any(), 4, 2, gomock.Any(), time.Minute).Return(batch, nil)
		for range batch {
			s.expectLoads(endpoint, 1)
		}
		s.attempts.EXPECT().CreateAttempt(gomock.Any(), gomock.Any()).Return(nil).Times(4)
		s.deliveries.EXPECT().RecordOutcome(gomock.Any(), gomock.Any()).Return(models.DeliverySucceeded, nil).Times(4)
		s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).Return(nil).Times(4)
		n, err := s.svc.Dispatch(context.Background())
		s.Require().NoError(err)
		s.Equal(4, n)
		n, err = s.svc.Dispatch(context.Background())
		s.Require().NoError(err)
		s.Equal(0, n, "every slot is taken, nothing is claimed")
		s.svc.Drain()
	})
	s.Run("the state event survives CreateAttempt replacing the struct with the stored row", func() {
		s.sender.res = services.SendResult{StatusCode: 200}
		s.deliveries.EXPECT().ClaimDueDeliveries(gomock.Any(), 4, 2, gomock.Any(), time.Minute).Return([]models.Delivery{s.claimed(1)}, nil)
		s.expectLoads(endpoint, 1)
		s.attempts.EXPECT().CreateAttempt(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, a *models.Attempt) error {
			*a = models.Attempt{ID: 99, DeliveryID: a.DeliveryID, Number: a.Number} // what the repo does: RETURNING replaces the struct
			return nil
		})
		s.deliveries.EXPECT().RecordOutcome(gomock.Any(), gomock.Any()).Return(models.DeliverySucceeded, nil)
		s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.OutboxEvent) error {
			var p apievents.DeliveryStatePayload
			s.Require().NoError(json.Unmarshal(e.Payload, &p))
			s.Equal(int64(3), p.EndpointID)
			s.Equal(ws, p.WorkspaceID)
			s.Equal(int32(200), *p.StatusCode)
			s.False(p.OccurredAt.IsZero())
			return nil
		})
		_, err := s.svc.Dispatch(context.Background())
		s.Require().NoError(err)
		s.svc.Drain()
	})
	s.Run("two secrets during rotation both sign", func() {
		s.sender.res = services.SendResult{StatusCode: 200}
		s.deliveries.EXPECT().ClaimDueDeliveries(gomock.Any(), 4, 2, gomock.Any(), time.Minute).Return([]models.Delivery{s.claimed(1)}, nil)
		s.expectLoads(endpoint, 2, 1)
		s.attempts.EXPECT().CreateAttempt(gomock.Any(), gomock.Any()).Return(nil)
		s.deliveries.EXPECT().RecordOutcome(gomock.Any(), gomock.Any()).Return(models.DeliverySucceeded, nil)
		s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).Return(nil)
		_, err := s.svc.Dispatch(context.Background())
		s.Require().NoError(err)
		s.svc.Drain()
		s.Len(s.sender.got, 1)
		s.Len(splitSpaces(s.sender.got[0].Headers[webhooks.HeaderSignature]), 2)
	})
}

func (s *DispatchServiceSuite) expired(attempt int32) models.ExpiredDelivery {
	d := s.claimed(attempt)
	return models.ExpiredDelivery{Delivery: d, LeaseExpiredAt: time.Now().Add(-time.Second)}
}

func (s *DispatchServiceSuite) TestSweep() {
	endpoint := &models.Endpoint{ID: 3, URL: "https://receiver.example.com/hooks", Revision: 2}
	tests := []struct {
		name       string
		delivery   models.ExpiredDelivery
		setup      func()
		wantState  string
		wantReason string
		wantErr    bool
	}{
		{name: "lost attempt is recorded as unknown and rescheduled", delivery: s.expired(1), wantState: models.DeliveryRetryScheduled, wantReason: services.ErrorClassUnknown, setup: func() {
			s.endpoints.EXPECT().GetEndpointByID(gomock.Any(), int64(3)).Return(endpoint, nil)
			s.messages.EXPECT().GetMessageByID(gomock.Any(), int64(42)).Return(&models.Message{ID: 42, WorkspaceID: ws, ApplicationID: 7}, nil)
		}},
		{name: "lost last attempt exhausts", delivery: s.expired(8), wantState: models.DeliveryFailed, wantReason: services.ReasonAttemptsExhausted, setup: func() {
			s.endpoints.EXPECT().GetEndpointByID(gomock.Any(), int64(3)).Return(endpoint, nil)
			s.messages.EXPECT().GetMessageByID(gomock.Any(), int64(42)).Return(&models.Message{ID: 42, WorkspaceID: ws, ApplicationID: 7}, nil)
		}},
		{name: "endpoint load failure leaves the new lease to expire", delivery: s.expired(1), wantErr: true, setup: func() {
			s.endpoints.EXPECT().GetEndpointByID(gomock.Any(), int64(3)).Return(nil, errors.New("boom"))
		}},
		{name: "lease taken by someone else meanwhile is logged, not an error", delivery: s.expired(1), setup: func() {
			s.endpoints.EXPECT().GetEndpointByID(gomock.Any(), int64(3)).Return(endpoint, nil)
			s.messages.EXPECT().GetMessageByID(gomock.Any(), int64(42)).Return(&models.Message{ID: 42, WorkspaceID: ws, ApplicationID: 7}, nil)
			s.attempts.EXPECT().CreateAttempt(gomock.Any(), gomock.Any()).Return(nil)
			s.deliveries.EXPECT().RecordOutcome(gomock.Any(), gomock.Any()).Return("", apperr.ErrNotFound)
		}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.deliveries.EXPECT().ClaimExpiredLeases(gomock.Any(), 10, gomock.Any(), time.Minute).Return([]models.ExpiredDelivery{tt.delivery}, nil)
			s.deliveries.EXPECT().ExpireDeadlines(gomock.Any(), 10).Return(int64(0), nil)
			tt.setup()
			if tt.wantState != "" {
				s.attempts.EXPECT().CreateAttempt(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, a *models.Attempt) error {
					s.Equal(tt.delivery.AttemptCount, a.Number)
					s.Equal(int32(1), a.EndpointRevision)
					s.Equal(endpoint.URL, a.RequestURL)
					s.Equal(services.ErrorClassUnknown, *a.ErrorClass)
					s.Nil(a.CompletedAt)
					s.Nil(a.StatusCode)
					s.WithinDuration(tt.delivery.LeaseExpiredAt.Add(-time.Minute), a.StartedAt, time.Second)
					return nil
				})
				s.deliveries.EXPECT().RecordOutcome(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, o models.DeliveryOutcome) (string, error) {
					s.Equal(int64(5), o.DeliveryID)
					s.Equal(tt.wantState, o.State)
					s.Equal(tt.wantReason, *o.FailureReason)
					if tt.wantState == models.DeliveryRetryScheduled {
						s.NotNil(o.DueAt)
					}
					return o.State, nil
				})
				s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.OutboxEvent) error {
					s.Equal("delivery."+tt.wantState, e.Type)
					return nil
				})
			}

			n, err := s.svc.Sweep(context.Background())
			s.Empty(s.observations(), "a lost attempt says nothing about the receiver")

			s.Equal(1, n)
			if tt.wantErr {
				s.Error(err)
				return
			}
			s.NoError(err)
		})
	}
	s.Run("claim failure", func() {
		s.deliveries.EXPECT().ClaimExpiredLeases(gomock.Any(), 10, gomock.Any(), time.Minute).Return(nil, errors.New("boom"))
		_, err := s.svc.Sweep(context.Background())
		requireErr(&s.Suite, err, 500, nil)
	})
	s.Run("scheduled work past its deadline is failed", func() {
		s.deliveries.EXPECT().ClaimExpiredLeases(gomock.Any(), 10, gomock.Any(), time.Minute).Return(nil, nil)
		s.deliveries.EXPECT().ExpireDeadlines(gomock.Any(), 10).Return(int64(2), nil)
		n, err := s.svc.Sweep(context.Background())
		s.Require().NoError(err)
		s.Equal(2, n)
	})
	s.Run("deadline expiry failure is reported", func() {
		s.deliveries.EXPECT().ClaimExpiredLeases(gomock.Any(), 10, gomock.Any(), time.Minute).Return(nil, nil)
		s.deliveries.EXPECT().ExpireDeadlines(gomock.Any(), 10).Return(int64(0), errors.New("boom"))
		_, err := s.svc.Sweep(context.Background())
		s.Error(err)
	})
}

// TestCrashBetweenSendAndRecord plays the fault the sweep exists for: the worker sends, dies before recording,
// the sweep records the attempt as unknown, and the next dispatch sends again, so the receiver sees exactly one duplicate.
func (s *DispatchServiceSuite) TestCrashBetweenSendAndRecord() {
	s.Run("one duplicate, both attempts recorded", func() {
		endpoint := &models.Endpoint{ID: 3, URL: "https://receiver.example.com/hooks", Revision: 2}
		s.sender.res = services.SendResult{StatusCode: 200}

		// 1. The worker sends attempt 1 and crashes before the outcome commits.
		s.deliveries.EXPECT().ClaimDueDeliveries(gomock.Any(), 4, 2, gomock.Any(), time.Minute).Return([]models.Delivery{s.claimed(1)}, nil)
		s.expectLoads(endpoint, 1)
		s.attempts.EXPECT().CreateAttempt(gomock.Any(), gomock.Any()).Return(errors.New("connection lost"))
		_, err := s.svc.Dispatch(context.Background())
		s.Require().NoError(err)
		s.svc.Drain()
		s.Require().Len(s.sender.got, 1, "bytes were sent")

		// 2. The sweep finds the expired lease and records attempt 1 as unknown.
		var attempts []models.Attempt
		s.deliveries.EXPECT().ClaimExpiredLeases(gomock.Any(), 10, gomock.Any(), time.Minute).Return([]models.ExpiredDelivery{s.expired(1)}, nil)
		s.deliveries.EXPECT().ExpireDeadlines(gomock.Any(), 10).Return(int64(0), nil)
		s.endpoints.EXPECT().GetEndpointByID(gomock.Any(), int64(3)).Return(endpoint, nil)
		s.messages.EXPECT().GetMessageByID(gomock.Any(), int64(42)).Return(&models.Message{ID: 42, WorkspaceID: ws, ApplicationID: 7}, nil)
		s.attempts.EXPECT().CreateAttempt(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, a *models.Attempt) error { attempts = append(attempts, *a); return nil })
		s.deliveries.EXPECT().RecordOutcome(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, o models.DeliveryOutcome) (string, error) {
			s.Equal(models.DeliveryRetryScheduled, o.State)
			return o.State, nil
		})
		s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).Return(nil)
		n, err := s.svc.Sweep(context.Background())
		s.Require().NoError(err)
		s.Equal(1, n)

		// 3. The next dispatch sends attempt 2; the receiver dedupes by webhook-id.
		s.deliveries.EXPECT().ClaimDueDeliveries(gomock.Any(), 4, 2, gomock.Any(), time.Minute).Return([]models.Delivery{s.claimed(2)}, nil)
		s.expectLoads(endpoint, 1)
		s.attempts.EXPECT().CreateAttempt(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, a *models.Attempt) error { attempts = append(attempts, *a); return nil })
		s.deliveries.EXPECT().RecordOutcome(gomock.Any(), gomock.Any()).Return(models.DeliverySucceeded, nil)
		s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).Return(nil)
		_, err = s.svc.Dispatch(context.Background())
		s.Require().NoError(err)
		s.svc.Drain()

		s.Len(s.sender.got, 2, "the receiver saw one duplicate")
		s.Equal(s.sender.got[0].Headers[webhooks.HeaderID], s.sender.got[1].Headers[webhooks.HeaderID], "same webhook-id, so the receiver can dedupe")
		s.Require().Len(attempts, 2)
		s.Equal([]int32{1, 2}, []int32{attempts[0].Number, attempts[1].Number})
		s.Equal(services.ErrorClassUnknown, *attempts[0].ErrorClass)
		s.Equal(int32(200), *attempts[1].StatusCode)
	})
}

func splitSpaces(s string) []string {
	var out []string
	for f := range splitFields(s) {
		out = append(out, f)
	}
	return out
}

func splitFields(s string) func(func(string) bool) {
	return func(yield func(string) bool) {
		start := -1
		for i := 0; i <= len(s); i++ {
			if i == len(s) || s[i] == ' ' {
				if start >= 0 && !yield(s[start:i]) {
					return
				}
				start = -1
			} else if start < 0 {
				start = i
			}
		}
	}
}

// TestAttemptSpanCarriesNoPayloadOrSecret runs one attempt under a recording tracer and checks the span: ids,
// numbers and the outcome, never the body, the signature headers or the signing secret.
func (s *DispatchServiceSuite) TestAttemptSpanCarriesNoPayloadOrSecret() {
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	defer otel.SetTracerProvider(sdktrace.NewTracerProvider())
	s.Run("canary", func() {
		endpoint := &models.Endpoint{ID: 3, URL: "https://receiver.example.com/hooks", Revision: 2}
		d := s.claimed(1)
		s.deliveries.EXPECT().ClaimDueDeliveries(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]models.Delivery{d}, nil)
		s.deliveries.EXPECT().ClaimDueDeliveries(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
		s.messages.EXPECT().GetMessageByID(gomock.Any(), int64(42)).Return(&models.Message{ID: 42, WorkspaceID: ws, ApplicationID: 7, PublicID: "msg_abc", Payload: []byte(`{"type":"t","data":{"note":"CANARY-7f3a9c"}}`)}, nil)
		s.endpoints.EXPECT().GetEndpointByID(gomock.Any(), int64(3)).Return(endpoint, nil)
		s.secrets.EXPECT().ListActiveEndpointSecrets(gomock.Any(), int64(3)).Return([]models.EndpointSecret{s.encrypted(1)}, nil)
		s.sender.res = services.SendResult{StatusCode: 204}
		s.attempts.EXPECT().CreateAttempt(gomock.Any(), gomock.Any()).Return(nil)
		s.deliveries.EXPECT().RecordOutcome(gomock.Any(), gomock.Any()).Return(models.DeliverySucceeded, nil)
		s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).Return(nil)
		_, err := s.svc.Dispatch(context.Background())
		s.Require().NoError(err)
		s.svc.Drain()
		spans := rec.Ended()
		s.Require().NotEmpty(spans)
		var found bool
		for _, sp := range spans {
			if sp.Name() == "delivery.attempt" {
				found = true
			}
			for _, kv := range sp.Attributes() {
				v := kv.Value.String()
				s.NotContains(v, "CANARY", "span %s attribute %s", sp.Name(), kv.Key)
				s.NotContains(v, s.secret, "the signing secret")
				s.NotContains(v, "v1,", "a signature")
			}
		}
		s.True(found, "the attempt span exists")
	})
}
