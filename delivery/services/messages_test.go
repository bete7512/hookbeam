package services_test

import (
	"context"
	"crypto/sha256"
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
	"github.com/bete7512/hookbeam/delivery/repos/repo_mocks"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

type MessageServiceSuite struct {
	suite.Suite
	apps       *repo_mocks.MockApplicationRepo
	endpoints  *repo_mocks.MockEndpointRepo
	messages   *repo_mocks.MockMessageRepo
	deliveries *repo_mocks.MockDeliveryRepo
	attempts   *repo_mocks.MockAttemptRepo
	outbox     *repo_mocks.MockOutboxRepo
	svc        services.MessageService
}

func TestMessageServiceSuite(t *testing.T) {
	suite.Run(t, new(MessageServiceSuite))
}

func (s *MessageServiceSuite) SetupSubTest() {
	ctrl := gomock.NewController(s.T())
	s.apps = repo_mocks.NewMockApplicationRepo(ctrl)
	s.endpoints = repo_mocks.NewMockEndpointRepo(ctrl)
	s.messages = repo_mocks.NewMockMessageRepo(ctrl)
	s.messages.EXPECT().WithTx(gomock.Any()).Return(s.messages).AnyTimes()
	s.deliveries = repo_mocks.NewMockDeliveryRepo(ctrl)
	s.deliveries.EXPECT().WithTx(gomock.Any()).Return(s.deliveries).AnyTimes()
	s.attempts = repo_mocks.NewMockAttemptRepo(ctrl)
	s.outbox = repo_mocks.NewMockOutboxRepo(ctrl)
	s.outbox.EXPECT().WithTx(gomock.Any()).Return(s.outbox).AnyTimes()
	var err error
	s.svc, err = services.NewMessageService(services.MessageServiceDeps{
		Tx: fakeTx{}, Applications: s.apps, Endpoints: s.endpoints, Messages: s.messages, Deliveries: s.deliveries, Attempts: s.attempts, Outbox: s.outbox,
		Policy: services.DefaultPolicy, PayloadRetention: 7 * 24 * time.Hour, IdempotencyWindow: 7 * 24 * time.Hour, MaxPayloadBytes: 1024, Logger: slog.New(slog.DiscardHandler),
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
		{name: "transaction failure", scope: scope, in: valid, wantStatus: 500, setup: func() {
			s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(app, nil)
			s.messages.EXPECT().FindMessageByIdempotencyKey(gomock.Any(), int64(7), "order-817-v1").Return(nil, notFound)
			s.endpoints.EXPECT().ListSubscribedEndpoints(gomock.Any(), int64(7), "order.shipped").Return(endpoints, nil)
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
		s.messages.EXPECT().GetMessage(gomock.Any(), ws, int64(42)).Return(&models.Message{ID: 42}, nil)
		s.deliveries.EXPECT().ListDeliveriesByMessage(gomock.Any(), int64(42)).Return([]models.Delivery{{ID: 1}}, nil)
		m, ds, err := s.svc.GetMessage(context.Background(), scope, 42)
		s.Require().NoError(err)
		s.Equal(int64(42), m.ID)
		s.Len(ds, 1)
	})
	s.Run("not found", func() {
		s.messages.EXPECT().GetMessage(gomock.Any(), ws, int64(-1)).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
		_, _, err := s.svc.GetMessage(context.Background(), scope, -1)
		requireErr(&s.Suite, err, 404, nil)
	})
	s.Run("no workspace", func() {
		_, _, err := s.svc.GetMessage(context.Background(), services.Scope{}, 42)
		requireErr(&s.Suite, err, 401, nil)
	})
}

func (s *MessageServiceSuite) TestListAttempts() {
	s.Run("attempts of a delivery in the workspace", func() {
		s.deliveries.EXPECT().GetDelivery(gomock.Any(), ws, int64(5)).Return(&models.Delivery{ID: 5}, nil)
		s.attempts.EXPECT().ListAttempts(gomock.Any(), int64(5)).Return([]models.Attempt{{ID: 1}, {ID: 2}}, nil)
		got, err := s.svc.ListAttempts(context.Background(), scope, 5)
		s.Require().NoError(err)
		s.Len(got, 2)
	})
	s.Run("delivery of another workspace", func() {
		s.deliveries.EXPECT().GetDelivery(gomock.Any(), ws, int64(5)).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
		_, err := s.svc.ListAttempts(context.Background(), scope, 5)
		requireErr(&s.Suite, err, 404, nil)
	})
}
