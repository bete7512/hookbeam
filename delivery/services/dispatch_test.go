package services_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos/repo_mocks"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/secrets"
	"github.com/bete7512/hookbeam/pkg/webhooks"
)

// fakeSender records the request and answers a canned result.
type fakeSender struct {
	got []services.SendRequest
	res services.SendResult
}

func (f *fakeSender) Send(_ context.Context, req services.SendRequest) services.SendResult {
	f.got = append(f.got, req)
	return f.res
}

type DispatchServiceSuite struct {
	suite.Suite
	deliveries *repo_mocks.MockDeliveryRepo
	attempts   *repo_mocks.MockAttemptRepo
	messages   *repo_mocks.MockMessageRepo
	endpoints  *repo_mocks.MockEndpointRepo
	secrets    *repo_mocks.MockEndpointSecretRepo
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
	s.attempts = repo_mocks.NewMockAttemptRepo(ctrl)
	s.attempts.EXPECT().WithTx(gomock.Any()).Return(s.attempts).AnyTimes()
	s.messages = repo_mocks.NewMockMessageRepo(ctrl)
	s.endpoints = repo_mocks.NewMockEndpointRepo(ctrl)
	s.secrets = repo_mocks.NewMockEndpointSecretRepo(ctrl)
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
		Tx: fakeTx{}, Deliveries: s.deliveries, Attempts: s.attempts, Messages: s.messages, Endpoints: s.endpoints, Secrets: s.secrets,
		Cipher: s.cipher, Sender: s.sender, Policy: policy, LeaseTTL: time.Minute, BatchSize: 10, Logger: slog.New(slog.DiscardHandler),
	})
	s.Require().NoError(err)
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
	s.messages.EXPECT().GetMessageByID(gomock.Any(), int64(42)).Return(&models.Message{ID: 42, PublicID: "msg_abc", Payload: []byte(body)}, nil)
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
		wantErr     bool
	}{
		{name: "2xx succeeds and records the attempt", delivery: s.claimed(1), res: services.SendResult{StatusCode: 204, Headers: map[string]string{"server": "x"}}, wantSent: true, wantState: models.DeliverySucceeded, setup: func() {
			s.expectLoads(endpoint, 2, 1)
		}, wantAttempt: func(a *models.Attempt) {
			s.Equal(int32(1), a.Number)
			s.Equal(int32(2), a.EndpointRevision)
			s.Equal(int32(2), a.SecretVersion, "newest active secret")
			s.Equal(int32(204), *a.StatusCode)
			s.Nil(a.ErrorClass)
			s.Equal("x", a.ResponseHeaders["server"])
		}},
		{name: "503 schedules a retry", delivery: s.claimed(1), res: services.SendResult{StatusCode: 503, BodyPreview: "later"}, wantSent: true, wantState: models.DeliveryRetryScheduled, wantReason: "http_503", setup: func() {
			s.expectLoads(endpoint, 1)
		}, wantAttempt: func(a *models.Attempt) { s.Equal("later", *a.ResponsePreview) }},
		{name: "timeout on the last attempt exhausts", delivery: s.claimed(8), res: services.SendResult{ErrorClass: "timeout", Err: errors.New("deadline")}, wantSent: true, wantState: models.DeliveryFailed, wantReason: services.ReasonAttemptsExhausted, setup: func() {
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
			s.deliveries.EXPECT().RecordOutcome(gomock.Any(), gomock.Any()).Return(apperr.ErrNotFound)
		}},
		{name: "load failure releases the lease", delivery: s.claimed(1), wantErr: true, setup: func() {
			s.messages.EXPECT().GetMessageByID(gomock.Any(), int64(42)).Return(nil, errors.New("boom"))
			s.deliveries.EXPECT().ReleaseDelivery(gomock.Any(), int64(5), gomock.Any()).Return(nil)
		}},
		{name: "load failure with the lease already gone", delivery: s.claimed(1), wantErr: true, setup: func() {
			s.messages.EXPECT().GetMessageByID(gomock.Any(), int64(42)).Return(nil, errors.New("boom"))
			s.deliveries.EXPECT().ReleaseDelivery(gomock.Any(), int64(5), gomock.Any()).Return(apperr.ErrNotFound)
		}},
		{name: "no active secret releases the lease", delivery: s.claimed(1), wantErr: true, setup: func() {
			s.expectLoads(endpoint)
			s.deliveries.EXPECT().ReleaseDelivery(gomock.Any(), int64(5), gomock.Any()).Return(nil)
		}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.sender.res = tt.res
			s.deliveries.EXPECT().ClaimDueDeliveries(gomock.Any(), 10, gomock.Any(), time.Minute).Return([]models.Delivery{tt.delivery}, nil)
			tt.setup()
			var recorded *models.Attempt
			if tt.wantState != "" {
				if tt.wantSent {
					s.attempts.EXPECT().CreateAttempt(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, a *models.Attempt) error {
						recorded = a
						return nil
					})
				}
				s.deliveries.EXPECT().RecordOutcome(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, o models.DeliveryOutcome) error {
					s.Equal(int64(5), o.DeliveryID)
					s.Equal(tt.wantState, o.State)
					if tt.wantReason != "" {
						s.Equal(tt.wantReason, *o.FailureReason)
					}
					if tt.wantState == models.DeliveryRetryScheduled {
						s.NotNil(o.DueAt)
					}
					return nil
				})
			}

			n, err := s.svc.Dispatch(context.Background())

			s.Equal(1, n)
			if tt.wantErr {
				s.Error(err)
				return
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
		s.deliveries.EXPECT().ClaimDueDeliveries(gomock.Any(), 10, gomock.Any(), time.Minute).Return(nil, errors.New("boom"))
		_, err := s.svc.Dispatch(context.Background())
		requireErr(&s.Suite, err, 500, nil)
	})
	s.Run("two secrets during rotation both sign", func() {
		s.sender.res = services.SendResult{StatusCode: 200}
		s.deliveries.EXPECT().ClaimDueDeliveries(gomock.Any(), 10, gomock.Any(), time.Minute).Return([]models.Delivery{s.claimed(1)}, nil)
		s.expectLoads(endpoint, 2, 1)
		s.attempts.EXPECT().CreateAttempt(gomock.Any(), gomock.Any()).Return(nil)
		s.deliveries.EXPECT().RecordOutcome(gomock.Any(), gomock.Any()).Return(nil)
		_, err := s.svc.Dispatch(context.Background())
		s.Require().NoError(err)
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
		}},
		{name: "lost last attempt exhausts", delivery: s.expired(8), wantState: models.DeliveryFailed, wantReason: services.ReasonAttemptsExhausted, setup: func() {
			s.endpoints.EXPECT().GetEndpointByID(gomock.Any(), int64(3)).Return(endpoint, nil)
		}},
		{name: "endpoint load failure leaves the new lease to expire", delivery: s.expired(1), wantErr: true, setup: func() {
			s.endpoints.EXPECT().GetEndpointByID(gomock.Any(), int64(3)).Return(nil, errors.New("boom"))
		}},
		{name: "lease taken by someone else meanwhile is logged, not an error", delivery: s.expired(1), setup: func() {
			s.endpoints.EXPECT().GetEndpointByID(gomock.Any(), int64(3)).Return(endpoint, nil)
			s.attempts.EXPECT().CreateAttempt(gomock.Any(), gomock.Any()).Return(nil)
			s.deliveries.EXPECT().RecordOutcome(gomock.Any(), gomock.Any()).Return(apperr.ErrNotFound)
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
				s.deliveries.EXPECT().RecordOutcome(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, o models.DeliveryOutcome) error {
					s.Equal(int64(5), o.DeliveryID)
					s.Equal(tt.wantState, o.State)
					s.Equal(tt.wantReason, *o.FailureReason)
					if tt.wantState == models.DeliveryRetryScheduled {
						s.NotNil(o.DueAt)
					}
					return nil
				})
			}

			n, err := s.svc.Sweep(context.Background())

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
		s.deliveries.EXPECT().ClaimDueDeliveries(gomock.Any(), 10, gomock.Any(), time.Minute).Return([]models.Delivery{s.claimed(1)}, nil)
		s.expectLoads(endpoint, 1)
		s.attempts.EXPECT().CreateAttempt(gomock.Any(), gomock.Any()).Return(errors.New("connection lost"))
		_, err := s.svc.Dispatch(context.Background())
		s.Require().Error(err)
		s.Require().Len(s.sender.got, 1, "bytes were sent")

		// 2. The sweep finds the expired lease and records attempt 1 as unknown.
		var attempts []models.Attempt
		s.deliveries.EXPECT().ClaimExpiredLeases(gomock.Any(), 10, gomock.Any(), time.Minute).Return([]models.ExpiredDelivery{s.expired(1)}, nil)
		s.deliveries.EXPECT().ExpireDeadlines(gomock.Any(), 10).Return(int64(0), nil)
		s.endpoints.EXPECT().GetEndpointByID(gomock.Any(), int64(3)).Return(endpoint, nil)
		s.attempts.EXPECT().CreateAttempt(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, a *models.Attempt) error { attempts = append(attempts, *a); return nil })
		s.deliveries.EXPECT().RecordOutcome(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, o models.DeliveryOutcome) error {
			s.Equal(models.DeliveryRetryScheduled, o.State)
			return nil
		})
		n, err := s.svc.Sweep(context.Background())
		s.Require().NoError(err)
		s.Equal(1, n)

		// 3. The next dispatch sends attempt 2; the receiver dedupes by webhook-id.
		s.deliveries.EXPECT().ClaimDueDeliveries(gomock.Any(), 10, gomock.Any(), time.Minute).Return([]models.Delivery{s.claimed(2)}, nil)
		s.expectLoads(endpoint, 1)
		s.attempts.EXPECT().CreateAttempt(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, a *models.Attempt) error { attempts = append(attempts, *a); return nil })
		s.deliveries.EXPECT().RecordOutcome(gomock.Any(), gomock.Any()).Return(nil)
		_, err = s.svc.Dispatch(context.Background())
		s.Require().NoError(err)

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
