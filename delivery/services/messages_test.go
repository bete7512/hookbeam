package services_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	apievents "github.com/bete7512/hookbeam/delivery/events"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/delivery/repos/repo_mocks"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

type MessageServiceSuite struct {
	suite.Suite
	apps       *repo_mocks.MockApplicationRepo
	envs       *repo_mocks.MockEnvironmentRepo
	endpoints  *repo_mocks.MockEndpointRepo
	messages   *repo_mocks.MockMessageRepo
	deliveries *repo_mocks.MockDeliveryRepo
	attempts   *repo_mocks.MockAttemptRepo
	outbox     *repo_mocks.MockOutboxRepo
	audit      *repo_mocks.MockAuditRepo
	audited    []models.AuditEvent
	svc        services.MessageService
}

func TestMessageServiceSuite(t *testing.T) {
	suite.Run(t, new(MessageServiceSuite))
}

func (s *MessageServiceSuite) SetupSubTest() {
	ctrl := gomock.NewController(s.T())
	s.apps = repo_mocks.NewMockApplicationRepo(ctrl)
	s.envs = repo_mocks.NewMockEnvironmentRepo(ctrl)
	s.endpoints = repo_mocks.NewMockEndpointRepo(ctrl)
	s.messages = repo_mocks.NewMockMessageRepo(ctrl)
	s.messages.EXPECT().WithTx(gomock.Any()).Return(s.messages).AnyTimes()
	s.deliveries = repo_mocks.NewMockDeliveryRepo(ctrl)
	s.deliveries.EXPECT().WithTx(gomock.Any()).Return(s.deliveries).AnyTimes()
	s.attempts = repo_mocks.NewMockAttemptRepo(ctrl)
	s.outbox = repo_mocks.NewMockOutboxRepo(ctrl)
	s.outbox.EXPECT().WithTx(gomock.Any()).Return(s.outbox).AnyTimes()
	s.audit = repo_mocks.NewMockAuditRepo(ctrl)
	s.audit.EXPECT().WithTx(gomock.Any()).Return(s.audit).AnyTimes()
	s.audited = nil
	s.audit.EXPECT().CreateAuditEvent(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.AuditEvent) error {
		s.audited = append(s.audited, *e)
		return nil
	}).AnyTimes()
	var err error
	s.svc, err = services.NewMessageService(services.MessageServiceDeps{
		Tx: fakeTx{}, Applications: s.apps, Environments: s.envs, Endpoints: s.endpoints, Messages: s.messages, Deliveries: s.deliveries, Attempts: s.attempts, Outbox: s.outbox, Audit: s.audit,
		Policy: services.DefaultPolicy, Limits: testLimits, PayloadRetention: 7 * 24 * time.Hour, IdempotencyWindow: 7 * 24 * time.Hour, MaxPayloadBytes: 1024, ExpiryBatchSize: 2, Logger: slog.New(slog.DiscardHandler),
	})
	s.Require().NoError(err)
}

func (s *MessageServiceSuite) TestNew() {
	_, err := services.NewMessageService(services.MessageServiceDeps{Tx: fakeTx{}})
	s.Error(err)
}

const body = `{"type":"order.shipped","external_id":"order-817","data":{"order_id":"817"}}`

func (s *MessageServiceSuite) existing(payload string) *models.Message {
	sum := sha256.Sum256([]byte(payload))
	return &models.Message{ID: 9, PublicID: "msg_existing", WorkspaceID: ws, ApplicationID: 7, EventType: "order.shipped", PayloadSHA256: sum[:], Payload: []byte(payload)}
}

func (s *MessageServiceSuite) TestAccept() {
	notFound := fmt.Errorf("%w: none", apperr.ErrNotFound)
	valid := services.AcceptInput{ApplicationID: 7, EventType: "order.shipped", IdempotencyKey: "order-817-v1", Payload: []byte(body)}
	app := &models.Application{ID: 7, WorkspaceID: ws}
	now := time.Now()
	endpoints := []models.Endpoint{{ID: 1, Revision: 1}, {ID: 2, Revision: 3}}
	tests := []struct {
		name          string
		scope         services.Scope
		in            services.AcceptInput
		setup         func()
		wantStatus    int
		wantFields    []string
		wantCount     int32
		wantDuplicate bool
	}{
		{name: "accepts and plans one delivery per endpoint", scope: scope, in: valid, wantCount: 2, setup: func() {
			s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(app, nil)
			s.messages.EXPECT().FindMessageByIdempotencyKey(gomock.Any(), int64(7), "order-817-v1").Return(nil, notFound)
			s.endpoints.EXPECT().ListSubscribedEndpoints(gomock.Any(), int64(7), "order.shipped").Return(endpoints, nil)
			s.deliveries.EXPECT().CountActiveDeliveries(gomock.Any(), ws, 100).Return(0, nil)
			s.messages.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, m *models.Message) error {
				s.True(strings.HasPrefix(m.PublicID, "msg_"))
				s.Len(m.PublicID, 26)
				s.Equal(ws, m.WorkspaceID)
				s.Equal("order-817-v1", *m.IdempotencyKey)
				s.Equal(body, string(m.Payload)) //nolint:testifylint // byte-exactness is the assertion, not JSON equality
				s.Len(m.PayloadSHA256, 32)
				s.True(m.PayloadExpiresAt.After(now.Add(6 * 24 * time.Hour)))
				m.ID = 42
				return nil
			})
			gomock.InOrder(
				s.deliveries.EXPECT().CreateDelivery(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, d *models.Delivery) error {
					s.Equal(ws, d.WorkspaceID, "the workspace travels with the delivery for the backlog count")
					s.Equal(int64(42), d.MessageID)
					s.Equal(int64(1), d.EndpointID)
					s.Equal(int32(1), d.EndpointRevision)
					s.Equal(models.DeliveryQueued, d.State)
					s.WithinDuration(now, *d.DueAt, 5*time.Second)
					s.WithinDuration(now.Add(24*time.Hour), d.DeadlineAt, 5*time.Second)
					return nil
				}),
				s.deliveries.EXPECT().CreateDelivery(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, d *models.Delivery) error {
					s.Equal(int64(2), d.EndpointID)
					s.Equal(int32(3), d.EndpointRevision)
					return nil
				}),
			)
			s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.OutboxEvent) error {
				s.Equal(apievents.DeliveryDue, e.Type)
				s.JSONEq(`{"message_id":42}`, string(e.Payload))
				return nil
			})
		}},
		{name: "same key and bytes returns the first result", scope: scope, in: valid, wantCount: 2, wantDuplicate: true, setup: func() {
			s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(app, nil)
			s.messages.EXPECT().FindMessageByIdempotencyKey(gomock.Any(), int64(7), "order-817-v1").Return(s.existing(body), nil)
			s.deliveries.EXPECT().CountDeliveriesByMessage(gomock.Any(), int64(9)).Return(int32(2), nil)
		}},
		{name: "same key with other bytes conflicts", scope: scope, in: valid, wantStatus: 409, setup: func() {
			s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(app, nil)
			s.messages.EXPECT().FindMessageByIdempotencyKey(gomock.Any(), int64(7), "order-817-v1").Return(s.existing(`{"type":"order.shipped","data":{}}`), nil)
		}},
		{name: "concurrent duplicate loses the insert and returns the winner", scope: scope, in: valid, wantCount: 2, wantDuplicate: true, setup: func() {
			s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(app, nil)
			s.messages.EXPECT().FindMessageByIdempotencyKey(gomock.Any(), int64(7), "order-817-v1").Return(nil, notFound)
			s.endpoints.EXPECT().ListSubscribedEndpoints(gomock.Any(), int64(7), "order.shipped").Return(endpoints, nil)
			s.deliveries.EXPECT().CountActiveDeliveries(gomock.Any(), ws, 100).Return(0, nil)
			s.messages.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(fmt.Errorf("%w: dup", apperr.ErrConflict))
			s.messages.EXPECT().FindMessageByIdempotencyKey(gomock.Any(), int64(7), "order-817-v1").Return(s.existing(body), nil)
			s.deliveries.EXPECT().CountDeliveriesByMessage(gomock.Any(), int64(9)).Return(int32(2), nil)
		}},
		{name: "no subscribed endpoint is a validation error", scope: scope, in: valid, wantStatus: 422, wantFields: []string{"type"}, setup: func() {
			s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(app, nil)
			s.messages.EXPECT().FindMessageByIdempotencyKey(gomock.Any(), int64(7), "order-817-v1").Return(nil, notFound)
			s.endpoints.EXPECT().ListSubscribedEndpoints(gomock.Any(), int64(7), "order.shipped").Return(nil, nil)
		}},
		{name: "unknown application", scope: scope, in: valid, wantStatus: 404, setup: func() {
			s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(nil, notFound)
		}},
		{name: "archived application", scope: scope, in: valid, wantStatus: 409, setup: func() {
			s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(&models.Application{ID: 7, ArchivedAt: &now}, nil)
		}},
		{name: "no workspace", scope: services.Scope{}, in: valid, wantStatus: 401},
		{name: "validation", scope: scope, in: services.AcceptInput{EventType: "order shipped", IdempotencyKey: "", Payload: []byte("{not json")}, wantStatus: 422, wantFields: []string{"application_id", "type", "Idempotency-Key", "body"}},
		{name: "payload too large", scope: scope, in: services.AcceptInput{ApplicationID: 7, EventType: "x", IdempotencyKey: "k", Payload: []byte(`"` + strings.Repeat("a", 2000) + `"`)}, wantStatus: 422, wantFields: []string{"body"}},
		{name: "fan-out over the limit is a validation error", scope: scope, in: valid, wantStatus: 422, wantFields: []string{"type"}, setup: func() {
			s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(app, nil)
			s.messages.EXPECT().FindMessageByIdempotencyKey(gomock.Any(), int64(7), "order-817-v1").Return(nil, notFound)
			s.endpoints.EXPECT().ListSubscribedEndpoints(gomock.Any(), int64(7), "order.shipped").Return(make([]models.Endpoint, testLimits.FanoutPerMessage+1), nil)
		}},
		{name: "fan-out at the limit is accepted", scope: scope, in: valid, wantCount: int32(testLimits.FanoutPerMessage), setup: func() { //nolint:gosec // small test constant
			s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(app, nil)
			s.messages.EXPECT().FindMessageByIdempotencyKey(gomock.Any(), int64(7), "order-817-v1").Return(nil, notFound)
			s.endpoints.EXPECT().ListSubscribedEndpoints(gomock.Any(), int64(7), "order.shipped").Return(make([]models.Endpoint, testLimits.FanoutPerMessage), nil)
			s.deliveries.EXPECT().CountActiveDeliveries(gomock.Any(), ws, 100).Return(0, nil)
			s.messages.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(nil)
			s.deliveries.EXPECT().CreateDelivery(gomock.Any(), gomock.Any()).Return(nil).Times(testLimits.FanoutPerMessage)
			s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).Return(nil)
		}},
		{name: "a backlog with room for the fan-out is accepted", scope: scope, in: valid, wantCount: 2, setup: func() {
			s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(app, nil)
			s.messages.EXPECT().FindMessageByIdempotencyKey(gomock.Any(), int64(7), "order-817-v1").Return(nil, notFound)
			s.endpoints.EXPECT().ListSubscribedEndpoints(gomock.Any(), int64(7), "order.shipped").Return(endpoints, nil)
			s.deliveries.EXPECT().CountActiveDeliveries(gomock.Any(), ws, 100).Return(98, nil)
			s.messages.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(nil)
			s.deliveries.EXPECT().CreateDelivery(gomock.Any(), gomock.Any()).Return(nil).Times(2)
			s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).Return(nil)
		}},
		{name: "a backlog the fan-out would overflow is told to retry later", scope: scope, in: valid, wantStatus: 429, setup: func() {
			s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(app, nil)
			s.messages.EXPECT().FindMessageByIdempotencyKey(gomock.Any(), int64(7), "order-817-v1").Return(nil, notFound)
			s.endpoints.EXPECT().ListSubscribedEndpoints(gomock.Any(), int64(7), "order.shipped").Return(endpoints, nil)
			s.deliveries.EXPECT().CountActiveDeliveries(gomock.Any(), ws, 100).Return(99, nil)
		}},
		{name: "transaction failure", scope: scope, in: valid, wantStatus: 500, setup: func() {
			s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(app, nil)
			s.messages.EXPECT().FindMessageByIdempotencyKey(gomock.Any(), int64(7), "order-817-v1").Return(nil, notFound)
			s.endpoints.EXPECT().ListSubscribedEndpoints(gomock.Any(), int64(7), "order.shipped").Return(endpoints, nil)
			s.deliveries.EXPECT().CountActiveDeliveries(gomock.Any(), ws, 100).Return(0, nil)
			s.messages.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(errors.New("boom"))
		}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setup != nil {
				tt.setup()
			}
			res, err := s.svc.Accept(context.Background(), tt.scope, tt.in)
			requireErr(&s.Suite, err, tt.wantStatus, tt.wantFields)
			if tt.wantStatus == 429 {
				var appErr *apperr.AppError
				s.Require().ErrorAs(err, &appErr)
				s.Equal(testLimits.BacklogRetryAfter, appErr.RetryAfter, "the client is told when to retry")
			}
			if tt.wantStatus != 0 {
				return
			}
			s.Equal(tt.wantCount, res.DeliveryCount)
			s.Equal(tt.wantDuplicate, res.Duplicate)
			s.NotNil(res.Message)
		})
	}
}

func (s *MessageServiceSuite) TestGetMessage() {
	s.Run("message with deliveries", func() {
		s.messages.EXPECT().GetMessage(gomock.Any(), ws, int64(42)).Return(&models.Message{ID: 42, ApplicationID: 7}, nil)
		s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(&models.Application{ID: 7, EnvironmentID: 2, EnvironmentKind: "production"}, nil)
		s.deliveries.EXPECT().ListDeliveriesByMessage(gomock.Any(), int64(42)).Return([]models.Delivery{{ID: 1}}, nil)
		m, app, ds, err := s.svc.GetMessage(context.Background(), scope, 42)
		s.Require().NoError(err)
		s.Equal(int64(42), m.ID)
		s.Equal("production", app.EnvironmentKind)
		s.Len(ds, 1)
	})
	s.Run("not found", func() {
		s.messages.EXPECT().GetMessage(gomock.Any(), ws, int64(-1)).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
		_, _, _, err := s.svc.GetMessage(context.Background(), scope, -1)
		requireErr(&s.Suite, err, 404, nil)
	})
	s.Run("no workspace", func() {
		_, _, _, err := s.svc.GetMessage(context.Background(), services.Scope{}, 42)
		requireErr(&s.Suite, err, 401, nil)
	})
}

func (s *MessageServiceSuite) TestListAttempts() {
	preview := "try later"
	attempts := func() []models.Attempt {
		return []models.Attempt{{ID: 1, ResponsePreview: &preview, ResponseHeaders: map[string]string{"server": "x"}}, {ID: 2}}
	}
	tests := []struct {
		name        string
		include     bool
		appEnabled  *bool // nil = the application is never looked up
		wantPreview bool
	}{
		{name: "a caller without the payload permission never sees previews", include: false},
		{name: "previews stay when allowed and the application enables them", include: true, appEnabled: ptr(true), wantPreview: true},
		{name: "previews are stripped when the application keeps them off", include: true, appEnabled: ptr(false)},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.deliveries.EXPECT().GetDelivery(gomock.Any(), ws, int64(5)).Return(&models.Delivery{ID: 5, MessageID: 42}, nil)
			s.attempts.EXPECT().ListAttempts(gomock.Any(), int64(5)).Return(attempts(), nil)
			if tt.appEnabled != nil {
				s.messages.EXPECT().GetMessage(gomock.Any(), ws, int64(42)).Return(&models.Message{ID: 42, ApplicationID: 7}, nil)
				s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(&models.Application{ID: 7, ResponsePreviewsEnabled: *tt.appEnabled}, nil)
			}
			got, err := s.svc.ListAttempts(context.Background(), scope, 5, tt.include)
			s.Require().NoError(err)
			s.Require().Len(got, 2)
			if tt.wantPreview {
				s.Equal("try later", *got[0].ResponsePreview)
				s.Equal("x", got[0].ResponseHeaders["server"])
			} else {
				s.Nil(got[0].ResponsePreview)
				s.Nil(got[0].ResponseHeaders, "headers follow the preview permission")
			}
		})
	}
	s.Run("delivery of another workspace", func() {
		s.deliveries.EXPECT().GetDelivery(gomock.Any(), ws, int64(5)).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
		_, err := s.svc.ListAttempts(context.Background(), scope, 5, false)
		requireErr(&s.Suite, err, 404, nil)
	})
}

func (s *MessageServiceSuite) TestGetMessagePayload() {
	tests := []struct {
		name       string
		scope      services.Scope
		setup      func()
		wantStatus int
	}{
		{name: "retained payload is returned and the read audited without the bytes", scope: scope, setup: func() {
			s.messages.EXPECT().GetMessage(gomock.Any(), ws, int64(42)).Return(&models.Message{ID: 42, ApplicationID: 7, EventType: "order.shipped", Payload: []byte(`{"secret":"do-not-audit"}`)}, nil)
		}},
		{name: "expired payload is gone", scope: scope, setup: func() {
			s.messages.EXPECT().GetMessage(gomock.Any(), ws, int64(42)).Return(&models.Message{ID: 42, Payload: []byte{}}, nil)
		}, wantStatus: 410},
		{name: "message of another workspace", scope: scope, setup: func() {
			s.messages.EXPECT().GetMessage(gomock.Any(), ws, int64(42)).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
		}, wantStatus: 404},
		{name: "no workspace", scope: services.Scope{}, setup: func() {}, wantStatus: 401},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			tt.setup()
			m, err := s.svc.GetMessagePayload(context.Background(), tt.scope, 42)
			requireErr(&s.Suite, err, tt.wantStatus, nil)
			if tt.wantStatus != 0 {
				s.Empty(s.audited, "a refused read is not audited")
				return
			}
			s.JSONEq(`{"secret":"do-not-audit"}`, string(m.Payload))
			s.Require().Len(s.audited, 1)
			s.Equal(services.AuditMessagePayload, s.audited[0].Action)
			s.Equal("message", s.audited[0].ResourceType)
			s.Equal(int64(42), *s.audited[0].ResourceID)
			s.NotContains(string(s.audited[0].Details), "do-not-audit")
			s.Contains(string(s.audited[0].Details), `"bytes"`)
		})
	}
}

func (s *MessageServiceSuite) TestCancelDelivery() {
	cancelled := "cancelled"
	s.Run("cancel of unclaimed work writes one audit row in the transaction", func() {
		s.deliveries.EXPECT().CancelDelivery(gomock.Any(), ws, int64(5)).Return(&models.Delivery{ID: 5, MessageID: 42, EndpointID: 3, State: models.DeliveryCancelled, FailureReason: &cancelled}, nil)
		s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).Return(nil)
		_, err := s.svc.CancelDelivery(context.Background(), scope, 5)
		s.Require().NoError(err)
		s.Require().Len(s.audited, 1)
		s.Equal(services.AuditDeliveryCancel, s.audited[0].Action)
		s.Equal(int64(5), *s.audited[0].ResourceID)
	})
	s.Run("unclaimed delivery is cancelled and announced", func() {
		s.deliveries.EXPECT().CancelDelivery(gomock.Any(), ws, int64(5)).Return(&models.Delivery{ID: 5, MessageID: 42, EndpointID: 3, State: models.DeliveryCancelled, FailureReason: &cancelled}, nil)
		s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.OutboxEvent) error {
			s.Equal(apievents.DeliveryCancelled, e.Type)
			var p apievents.DeliveryStatePayload
			s.Require().NoError(json.Unmarshal(e.Payload, &p))
			s.Equal(int64(5), p.DeliveryID)
			s.Equal("cancelled", *p.Reason)
			return nil
		})
		got, err := s.svc.CancelDelivery(context.Background(), scope, 5)
		s.Require().NoError(err)
		s.Equal(models.DeliveryCancelled, got.State)
	})
	s.Run("in-flight delivery is only marked; the attempt announces the end", func() {
		s.deliveries.EXPECT().CancelDelivery(gomock.Any(), ws, int64(5)).Return(&models.Delivery{ID: 5, State: models.DeliveryInFlight, CancelRequested: true}, nil)
		got, err := s.svc.CancelDelivery(context.Background(), scope, 5)
		s.Require().NoError(err)
		s.True(got.CancelRequested)
	})
	s.Run("finished delivery is returned unchanged", func() {
		s.deliveries.EXPECT().CancelDelivery(gomock.Any(), ws, int64(5)).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
		s.deliveries.EXPECT().GetDelivery(gomock.Any(), ws, int64(5)).Return(&models.Delivery{ID: 5, State: models.DeliverySucceeded}, nil)
		got, err := s.svc.CancelDelivery(context.Background(), scope, 5)
		s.Require().NoError(err)
		s.Equal(models.DeliverySucceeded, got.State)
	})
	s.Run("unknown or foreign delivery", func() {
		s.deliveries.EXPECT().CancelDelivery(gomock.Any(), ws, int64(-1)).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
		s.deliveries.EXPECT().GetDelivery(gomock.Any(), ws, int64(-1)).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
		_, err := s.svc.CancelDelivery(context.Background(), scope, -1)
		requireErr(&s.Suite, err, 404, nil)
	})
	s.Run("repo failure", func() {
		s.deliveries.EXPECT().CancelDelivery(gomock.Any(), ws, int64(5)).Return(nil, errors.New("boom"))
		_, err := s.svc.CancelDelivery(context.Background(), scope, 5)
		requireErr(&s.Suite, err, 500, nil)
	})
	s.Run("no workspace", func() {
		_, err := s.svc.CancelDelivery(context.Background(), services.Scope{}, 5)
		requireErr(&s.Suite, err, 401, nil)
	})
}

func (s *MessageServiceSuite) TestReplayDelivery() {
	notFound := fmt.Errorf("%w: none", apperr.ErrNotFound)
	now := time.Now()
	failed := &models.Delivery{ID: 5, MessageID: 42, EndpointID: 3, EndpointRevision: 1, State: models.DeliveryFailed}
	endpoint := &models.Endpoint{ID: 3, Revision: 4}
	live := &models.Message{ID: 42, Payload: []byte(body), PayloadExpiresAt: now.Add(48 * time.Hour)}
	tests := []struct {
		name       string
		scope      services.Scope
		key        string
		setup      func()
		wantStatus int
		wantFields []string
		wantID     int64
		check      func(d *models.Delivery)
	}{
		{name: "replays under the current revision, wakes the worker and audits", scope: scope, key: "r1", setup: func() {
			s.deliveries.EXPECT().GetDelivery(gomock.Any(), ws, int64(5)).Return(failed, nil)
			s.deliveries.EXPECT().FindReplay(gomock.Any(), int64(5), "r1").Return(nil, notFound)
			s.endpoints.EXPECT().GetEndpointByID(gomock.Any(), int64(3)).Return(endpoint, nil)
			s.messages.EXPECT().GetMessageByID(gomock.Any(), int64(42)).Return(live, nil)
			s.deliveries.EXPECT().CreateDelivery(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, d *models.Delivery) error { d.ID = 9; return nil })
			s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.OutboxEvent) error {
				s.Equal("delivery.due", e.Type)
				s.JSONEq(`{"message_id":42}`, string(e.Payload))
				return nil
			})
		}, wantID: 9, check: func(d *models.Delivery) {
			s.Require().Len(s.audited, 1)
			s.Equal(services.AuditDeliveryReplay, s.audited[0].Action)
			s.Equal(int64(9), *s.audited[0].ResourceID)
			s.NotContains(string(s.audited[0].Details), "data", "no payload bytes in the audit trail")
			s.Equal(models.DeliveryQueued, d.State)
			s.Equal(int32(4), d.EndpointRevision)
			s.Equal(int64(5), *d.ReplayOfDeliveryID)
			s.Equal("r1", *d.ReplayIdempotencyKey)
			s.WithinDuration(now.Add(24*time.Hour), d.DeadlineAt, 5*time.Second)
		}},
		{name: "deadline is capped by payload expiry", scope: scope, key: "r1", setup: func() {
			soon := &models.Message{ID: 42, Payload: []byte(body), PayloadExpiresAt: now.Add(time.Hour)}
			s.deliveries.EXPECT().GetDelivery(gomock.Any(), ws, int64(5)).Return(failed, nil)
			s.deliveries.EXPECT().FindReplay(gomock.Any(), int64(5), "r1").Return(nil, notFound)
			s.endpoints.EXPECT().GetEndpointByID(gomock.Any(), int64(3)).Return(endpoint, nil)
			s.messages.EXPECT().GetMessageByID(gomock.Any(), int64(42)).Return(soon, nil)
			s.deliveries.EXPECT().CreateDelivery(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, d *models.Delivery) error { d.ID = 9; return nil })
			s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).Return(nil)
		}, wantID: 9, check: func(d *models.Delivery) { s.WithinDuration(now.Add(time.Hour), d.DeadlineAt, 5*time.Second) }},
		{name: "same key returns the same cycle", scope: scope, key: "r1", setup: func() {
			s.deliveries.EXPECT().GetDelivery(gomock.Any(), ws, int64(5)).Return(failed, nil)
			s.deliveries.EXPECT().FindReplay(gomock.Any(), int64(5), "r1").Return(&models.Delivery{ID: 9}, nil)
		}, wantID: 9},
		{name: "concurrent same key loses the insert and reads the winner", scope: scope, key: "r1", setup: func() {
			s.deliveries.EXPECT().GetDelivery(gomock.Any(), ws, int64(5)).Return(failed, nil)
			s.deliveries.EXPECT().FindReplay(gomock.Any(), int64(5), "r1").Return(nil, notFound)
			s.endpoints.EXPECT().GetEndpointByID(gomock.Any(), int64(3)).Return(endpoint, nil)
			s.messages.EXPECT().GetMessageByID(gomock.Any(), int64(42)).Return(live, nil)
			s.deliveries.EXPECT().CreateDelivery(gomock.Any(), gomock.Any()).Return(fmt.Errorf("%w: dup", apperr.ErrConflict))
			s.deliveries.EXPECT().FindReplay(gomock.Any(), int64(5), "r1").Return(&models.Delivery{ID: 9}, nil)
		}, wantID: 9},
		{name: "another cycle is active", scope: scope, key: "r2", setup: func() {
			s.deliveries.EXPECT().GetDelivery(gomock.Any(), ws, int64(5)).Return(failed, nil)
			s.deliveries.EXPECT().FindReplay(gomock.Any(), int64(5), "r2").Return(nil, notFound)
			s.endpoints.EXPECT().GetEndpointByID(gomock.Any(), int64(3)).Return(endpoint, nil)
			s.messages.EXPECT().GetMessageByID(gomock.Any(), int64(42)).Return(live, nil)
			s.deliveries.EXPECT().CreateDelivery(gomock.Any(), gomock.Any()).Return(fmt.Errorf("%w: dup", apperr.ErrConflict))
			s.deliveries.EXPECT().FindReplay(gomock.Any(), int64(5), "r2").Return(nil, notFound)
		}, wantStatus: 409},
		{name: "succeeded delivery is not replayable", scope: scope, key: "r1", setup: func() {
			s.deliveries.EXPECT().GetDelivery(gomock.Any(), ws, int64(5)).Return(&models.Delivery{ID: 5, State: models.DeliverySucceeded}, nil)
			s.deliveries.EXPECT().FindReplay(gomock.Any(), int64(5), "r1").Return(nil, notFound)
		}, wantStatus: 409},
		{name: "active delivery is not replayable", scope: scope, key: "r1", setup: func() {
			s.deliveries.EXPECT().GetDelivery(gomock.Any(), ws, int64(5)).Return(&models.Delivery{ID: 5, State: models.DeliveryRetryScheduled}, nil)
			s.deliveries.EXPECT().FindReplay(gomock.Any(), int64(5), "r1").Return(nil, notFound)
		}, wantStatus: 409},
		{name: "archived endpoint", scope: scope, key: "r1", setup: func() {
			s.deliveries.EXPECT().GetDelivery(gomock.Any(), ws, int64(5)).Return(failed, nil)
			s.deliveries.EXPECT().FindReplay(gomock.Any(), int64(5), "r1").Return(nil, notFound)
			s.endpoints.EXPECT().GetEndpointByID(gomock.Any(), int64(3)).Return(&models.Endpoint{ID: 3, ArchivedAt: &now}, nil)
		}, wantStatus: 409},
		{name: "expired payload", scope: scope, key: "r1", setup: func() {
			s.deliveries.EXPECT().GetDelivery(gomock.Any(), ws, int64(5)).Return(failed, nil)
			s.deliveries.EXPECT().FindReplay(gomock.Any(), int64(5), "r1").Return(nil, notFound)
			s.endpoints.EXPECT().GetEndpointByID(gomock.Any(), int64(3)).Return(endpoint, nil)
			s.messages.EXPECT().GetMessageByID(gomock.Any(), int64(42)).Return(&models.Message{ID: 42, PayloadExpiresAt: now.Add(-time.Hour)}, nil)
		}, wantStatus: 409},
		{name: "unknown or foreign delivery", scope: scope, key: "r1", setup: func() {
			s.deliveries.EXPECT().GetDelivery(gomock.Any(), ws, int64(5)).Return(nil, notFound)
		}, wantStatus: 404},
		{name: "missing key", scope: scope, key: " ", wantStatus: 422, wantFields: []string{"Idempotency-Key"}},
		{name: "no workspace", scope: services.Scope{}, key: "r1", wantStatus: 401},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setup != nil {
				tt.setup()
			}

			got, err := s.svc.ReplayDelivery(context.Background(), tt.scope, 5, tt.key)

			requireErr(&s.Suite, err, tt.wantStatus, tt.wantFields)
			if tt.wantStatus != 0 {
				return
			}
			s.Equal(tt.wantID, got.ID)
			if tt.check != nil {
				tt.check(got)
			}
		})
	}
}

func (s *MessageServiceSuite) TestExpireRetention() {
	s.Run("payloads are emptied batch by batch until a short batch", func() {
		gomock.InOrder(
			s.messages.EXPECT().ExpirePayloads(gomock.Any(), 2).Return(int64(2), nil),
			s.messages.EXPECT().ExpirePayloads(gomock.Any(), 2).Return(int64(1), nil),
		)
		n, err := s.svc.ExpirePayloads(context.Background())
		s.Require().NoError(err)
		s.Equal(int64(3), n)
	})
	s.Run("keys are cleared the same way", func() {
		s.messages.EXPECT().ExpireIdempotencyKeys(gomock.Any(), 2).Return(int64(0), nil)
		n, err := s.svc.ExpireIdempotencyKeys(context.Background())
		s.Require().NoError(err)
		s.Zero(n)
	})
	s.Run("repo failure", func() {
		s.messages.EXPECT().ExpirePayloads(gomock.Any(), 2).Return(int64(0), errors.New("boom"))
		_, err := s.svc.ExpirePayloads(context.Background())
		requireErr(&s.Suite, err, 500, nil)
	})
}

func (s *MessageServiceSuite) TestListMessages() {
	notFound := fmt.Errorf("%w: none", apperr.ErrNotFound)
	tests := []struct {
		name       string
		scope      services.Scope
		opts       repos.ListMessagesOpts
		setup      func()
		wantStatus int
		wantFields []string
		wantTotal  int
	}{
		{name: "application list", scope: scope, opts: repos.ListMessagesOpts{ApplicationID: 7, Limit: 20, EventType: " order.paid "}, setup: func() {
			s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(&models.Application{ID: 7}, nil)
			s.messages.EXPECT().ListMessages(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, o repos.ListMessagesOpts) ([]models.MessageForList, error) {
				s.Equal(ws, o.WorkspaceID)
				s.Equal("order.paid", o.EventType)
				return []models.MessageForList{{ID: 1}}, nil
			})
			s.messages.EXPECT().CountMessages(gomock.Any(), gomock.Any()).Return(1, nil)
		}, wantTotal: 1},
		{name: "environment list", scope: scope, opts: repos.ListMessagesOpts{EnvironmentID: 2, Limit: 20}, setup: func() {
			s.envs.EXPECT().GetEnvironment(gomock.Any(), ws, int64(2)).Return(&models.Environment{ID: 2}, nil)
			s.messages.EXPECT().ListMessages(gomock.Any(), gomock.Any()).Return(nil, nil)
			s.messages.EXPECT().CountMessages(gomock.Any(), gomock.Any()).Return(0, nil)
		}},
		{name: "neither application nor environment", scope: scope, opts: repos.ListMessagesOpts{Limit: 20}, wantStatus: 422, wantFields: []string{"application_id"}},
		{name: "bad limit, outcome, sort and range", scope: scope, opts: repos.ListMessagesOpts{ApplicationID: 7, Limit: 0, Outcome: "lost", SortBy: "payload", AcceptedAfter: ptrTime(time.Now()), AcceptedBefore: ptrTime(time.Now().Add(-time.Hour))}, wantStatus: 422, wantFields: []string{"limit", "outcome", "sort_by", "accepted_before"}},
		{name: "foreign application", scope: scope, opts: repos.ListMessagesOpts{ApplicationID: 7, Limit: 20}, setup: func() {
			s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(nil, notFound)
		}, wantStatus: 404},
		{name: "no workspace", scope: services.Scope{}, opts: repos.ListMessagesOpts{ApplicationID: 7, Limit: 20}, wantStatus: 401},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setup != nil {
				tt.setup()
			}
			_, total, err := s.svc.ListMessages(context.Background(), tt.scope, tt.opts)
			requireErr(&s.Suite, err, tt.wantStatus, tt.wantFields)
			if tt.wantStatus == 0 {
				s.Equal(tt.wantTotal, total)
			}
		})
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func (s *MessageServiceSuite) TestSendTest() {
	now := time.Now()
	s.Run("plans one hookbeam.test delivery for the endpoint alone", func() {
		s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(&models.Endpoint{ID: 3, WorkspaceID: ws, ApplicationID: 7, Revision: 2}, nil)
		s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(&models.Application{ID: 7, WorkspaceID: ws}, nil)
		s.deliveries.EXPECT().CountActiveDeliveries(gomock.Any(), ws, 100).Return(0, nil)
		s.messages.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, m *models.Message) error {
			s.Equal(services.TestEventType, m.EventType)
			s.True(strings.HasPrefix(*m.IdempotencyKey, "test-"))
			s.Contains(string(m.Payload), `"endpoint_id":3`)
			m.ID = 42
			return nil
		})
		s.deliveries.EXPECT().CreateDelivery(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, d *models.Delivery) error {
			s.Equal(int64(3), d.EndpointID)
			s.Equal(int32(2), d.EndpointRevision)
			d.ID = 9
			return nil
		})
		s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).Return(nil)
		m, d, err := s.svc.SendTest(context.Background(), scope, 3)
		s.Require().NoError(err)
		s.Equal(int64(42), m.ID)
		s.Equal(int64(9), d.ID)
		s.Equal(models.DeliveryQueued, d.State)
	})
	s.Run("an archived endpoint is a conflict", func() {
		s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(&models.Endpoint{ID: 3, ApplicationID: 7, ArchivedAt: &now}, nil)
		_, _, err := s.svc.SendTest(context.Background(), scope, 3)
		requireErr(&s.Suite, err, 409, nil)
	})
	s.Run("another workspace's endpoint", func() {
		s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
		_, _, err := s.svc.SendTest(context.Background(), scope, 3)
		requireErr(&s.Suite, err, 404, nil)
	})
	s.Run("no workspace", func() {
		_, _, err := s.svc.SendTest(context.Background(), services.Scope{}, 3)
		requireErr(&s.Suite, err, 401, nil)
	})
}
