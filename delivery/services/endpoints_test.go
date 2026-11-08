package services_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos/repo_mocks"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/safehttp"
	"github.com/bete7512/hookbeam/pkg/secrets"
)

type EndpointServiceSuite struct {
	suite.Suite
	apps       *repo_mocks.MockApplicationRepo
	endpoints  *repo_mocks.MockEndpointRepo
	deliveries *repo_mocks.MockDeliveryRepo
	audit      *repo_mocks.MockAuditRepo
	audited    []models.AuditEvent
	secrets    *repo_mocks.MockEndpointSecretRepo
	subs       *repo_mocks.MockSubscriptionRepo
	cipher     *secrets.Cipher
	svc        services.EndpointService
}

func TestEndpointServiceSuite(t *testing.T) {
	suite.Run(t, new(EndpointServiceSuite))
}

func (s *EndpointServiceSuite) SetupSubTest() {
	ctrl := gomock.NewController(s.T())
	s.apps = repo_mocks.NewMockApplicationRepo(ctrl)
	s.endpoints = repo_mocks.NewMockEndpointRepo(ctrl)
	s.endpoints.EXPECT().WithTx(gomock.Any()).Return(s.endpoints).AnyTimes()
	s.deliveries = repo_mocks.NewMockDeliveryRepo(ctrl)
	s.deliveries.EXPECT().WithTx(gomock.Any()).Return(s.deliveries).AnyTimes()
	s.audit = repo_mocks.NewMockAuditRepo(ctrl)
	s.audit.EXPECT().WithTx(gomock.Any()).Return(s.audit).AnyTimes()
	s.audited = nil
	s.audit.EXPECT().CreateAuditEvent(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.AuditEvent) error {
		s.audited = append(s.audited, *e)
		return nil
	}).AnyTimes()
	s.secrets = repo_mocks.NewMockEndpointSecretRepo(ctrl)
	s.secrets.EXPECT().WithTx(gomock.Any()).Return(s.secrets).AnyTimes()
	s.subs = repo_mocks.NewMockSubscriptionRepo(ctrl)
	s.subs.EXPECT().WithTx(gomock.Any()).Return(s.subs).AnyTimes()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	s.Require().NoError(err)
	s.cipher, err = secrets.New(base64.StdEncoding.EncodeToString(key))
	s.Require().NoError(err)
	s.svc, err = services.NewEndpointService(services.EndpointServiceDeps{
		Tx: fakeTx{}, Applications: s.apps, Endpoints: s.endpoints, Deliveries: s.deliveries, Audit: s.audit, Secrets: s.secrets, Subscriptions: s.subs, URL: safehttp.Config{AllowPrivate: true},
		Cipher: s.cipher, KeyID: "2026-11", Logger: slog.New(slog.DiscardHandler),
	})
	s.Require().NoError(err)
}

func (s *EndpointServiceSuite) TestNew() {
	_, err := services.NewEndpointService(services.EndpointServiceDeps{Tx: fakeTx{}})
	s.Error(err)
}

func (s *EndpointServiceSuite) expectApplication(found bool) {
	call := s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7))
	if found {
		call.Return(&models.Application{ID: 7, WorkspaceID: ws}, nil)
	} else {
		call.Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
	}
}

func (s *EndpointServiceSuite) TestCreateEndpoint() {
	valid := services.CreateEndpointInput{ApplicationID: 7, URL: "https://receiver.example.com/hooks", Description: "prod", EventTypes: []string{"order.shipped", " order.shipped", "order.created"}}
	tests := []struct {
		name       string
		scope      services.Scope
		in         services.CreateEndpointInput
		setup      func()
		wantTypes  []string
		wantStatus int
		wantFields []string
	}{
		{name: "creates endpoint, secret and subscriptions in order", scope: scope, in: valid, wantTypes: []string{"order.created", "order.shipped"}, setup: func() {
			s.expectApplication(true)
			gomock.InOrder(
				s.endpoints.EXPECT().CreateEndpoint(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.Endpoint) error {
					s.Equal(ws, e.WorkspaceID)
					s.Equal(actor, e.CreatedBy)
					s.Equal("https://receiver.example.com/hooks", e.URL)
					s.Equal([]string{"order.created", "order.shipped"}, e.EventTypes)
					e.ID = 42
					return nil
				}),
				s.secrets.EXPECT().CreateEndpointSecret(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, sec *models.EndpointSecret) error {
					s.Equal(int64(42), sec.EndpointID)
					s.Equal("2026-11", sec.KeyID)
					s.Len(sec.Fingerprint, 16)
					plain, err := s.cipher.Decrypt(sec.Ciphertext)
					s.Require().NoError(err)
					s.True(strings.HasPrefix(string(plain), services.SecretPrefix))
					sec.Version = 1
					return nil
				}),
				s.subs.EXPECT().CreateSubscriptions(gomock.Any(), int64(42), []string{"order.created", "order.shipped"}).Return(nil),
			)
		}},
		{name: "no event types subscribes to everything", scope: scope, in: services.CreateEndpointInput{ApplicationID: 7, URL: "http://localhost:9999/hook"}, wantTypes: []string{}, setup: func() {
			s.expectApplication(true)
			s.endpoints.EXPECT().CreateEndpoint(gomock.Any(), gomock.Any()).Return(nil)
			s.secrets.EXPECT().CreateEndpointSecret(gomock.Any(), gomock.Any()).Return(nil)
			s.subs.EXPECT().CreateSubscriptions(gomock.Any(), gomock.Any(), []string{}).Return(nil)
		}},
		{name: "no workspace", scope: services.Scope{}, in: valid, wantStatus: 401},
		{name: "validation", scope: scope, in: services.CreateEndpointInput{URL: "ftp://x", Description: strings.Repeat("d", 501), EventTypes: []string{"*"}}, wantStatus: 422, wantFields: []string{"application_id", "url", "description", "event_types"}},
		{name: "url with credentials", scope: scope, in: services.CreateEndpointInput{ApplicationID: 7, URL: "https://user:pw@example.com/"}, wantStatus: 422, wantFields: []string{"url"}},
		{name: "event type with whitespace", scope: scope, in: services.CreateEndpointInput{ApplicationID: 7, URL: "https://example.com/", EventTypes: []string{"order shipped"}}, wantStatus: 422, wantFields: []string{"event_types"}},
		{name: "unknown application", scope: scope, in: valid, setup: func() { s.expectApplication(false) }, wantStatus: 404},
		{name: "application of another workspace is not found", scope: services.Scope{WorkspaceID: "ws_other", ActorID: actor}, in: valid, setup: func() {
			s.apps.EXPECT().GetApplication(gomock.Any(), "ws_other", int64(7)).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
		}, wantStatus: 404},
		{name: "transaction failure", scope: scope, in: valid, setup: func() {
			s.expectApplication(true)
			s.endpoints.EXPECT().CreateEndpoint(gomock.Any(), gomock.Any()).Return(errors.New("boom"))
		}, wantStatus: 500},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setup != nil {
				tt.setup()
			}
			e, secret, version, err := s.svc.CreateEndpoint(context.Background(), tt.scope, tt.in)
			requireErr(&s.Suite, err, tt.wantStatus, tt.wantFields)
			if tt.wantStatus != 0 {
				return
			}
			s.Equal(tt.wantTypes, e.EventTypes)
			s.True(strings.HasPrefix(secret, services.SecretPrefix))
			s.Greater(len(secret), 40)
			s.GreaterOrEqual(version, int32(0))
		})
	}
}

func (s *EndpointServiceSuite) TestPauseEndpoint() {
	now := time.Now()
	active := &models.Endpoint{ID: 3, WorkspaceID: ws}
	paused := &models.Endpoint{ID: 3, WorkspaceID: ws, PausedAt: &now}
	archived := &models.Endpoint{ID: 3, WorkspaceID: ws, ArchivedAt: &now}
	notFound := fmt.Errorf("%w: none", apperr.ErrNotFound)
	tests := []struct {
		name       string
		scope      services.Scope
		setup      func()
		wantStatus int
	}{
		{name: "pauses an active endpoint", scope: scope, setup: func() {
			s.endpoints.EXPECT().PauseEndpoint(gomock.Any(), ws, int64(3)).Return(paused, nil)
			s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return([]string{"a"}, nil)
		}},
		{name: "pausing a paused endpoint is a no-op", scope: scope, setup: func() {
			s.endpoints.EXPECT().PauseEndpoint(gomock.Any(), ws, int64(3)).Return(nil, notFound)
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(paused, nil)
			s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return(nil, nil)
		}},
		{name: "archived endpoint is a conflict", scope: scope, setup: func() {
			s.endpoints.EXPECT().PauseEndpoint(gomock.Any(), ws, int64(3)).Return(nil, notFound)
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(archived, nil)
		}, wantStatus: 409},
		{name: "unknown endpoint", scope: scope, setup: func() {
			s.endpoints.EXPECT().PauseEndpoint(gomock.Any(), ws, int64(3)).Return(nil, notFound)
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(nil, notFound)
		}, wantStatus: 404},
		{name: "no workspace", scope: services.Scope{}, wantStatus: 401},
		{name: "repo failure", scope: scope, setup: func() {
			s.endpoints.EXPECT().PauseEndpoint(gomock.Any(), ws, int64(3)).Return(nil, errors.New("boom"))
		}, wantStatus: 500},
		{name: "subscriptions failure", scope: scope, setup: func() {
			s.endpoints.EXPECT().PauseEndpoint(gomock.Any(), ws, int64(3)).Return(active, nil)
			s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return(nil, errors.New("boom"))
		}, wantStatus: 500},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setup != nil {
				tt.setup()
			}
			e, err := s.svc.PauseEndpoint(context.Background(), tt.scope, 3)
			requireErr(&s.Suite, err, tt.wantStatus, nil)
			if tt.wantStatus == 0 {
				s.NotNil(e.PausedAt)
			}
		})
	}
}

func (s *EndpointServiceSuite) TestGetEndpoint() {
	s.Run("found", func() {
		s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(&models.Endpoint{ID: 3}, nil)
		s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return([]string{"x"}, nil)
		e, err := s.svc.GetEndpoint(context.Background(), scope, 3)
		s.Require().NoError(err)
		s.Equal([]string{"x"}, e.EventTypes)
	})
	s.Run("not found", func() {
		s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(-1)).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
		_, err := s.svc.GetEndpoint(context.Background(), scope, -1)
		requireErr(&s.Suite, err, 404, nil)
	})
	s.Run("no workspace", func() {
		_, err := s.svc.GetEndpoint(context.Background(), services.Scope{}, 3)
		requireErr(&s.Suite, err, 401, nil)
	})
}

func (s *EndpointServiceSuite) TestResumeEndpoint() {
	now := time.Now()
	active := &models.Endpoint{ID: 3, WorkspaceID: ws}
	archived := &models.Endpoint{ID: 3, WorkspaceID: ws, ArchivedAt: &now}
	notFound := fmt.Errorf("%w: none", apperr.ErrNotFound)
	tests := []struct {
		name       string
		scope      services.Scope
		setup      func()
		wantStatus int
	}{
		{name: "resumes a paused endpoint", scope: scope, setup: func() {
			s.endpoints.EXPECT().ResumeEndpoint(gomock.Any(), ws, int64(3)).Return(active, nil)
			s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return([]string{"a"}, nil)
		}},
		{name: "resuming an unpaused endpoint is a no-op", scope: scope, setup: func() {
			s.endpoints.EXPECT().ResumeEndpoint(gomock.Any(), ws, int64(3)).Return(nil, notFound)
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(active, nil)
			s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return(nil, nil)
		}},
		{name: "archived endpoint is a conflict", scope: scope, setup: func() {
			s.endpoints.EXPECT().ResumeEndpoint(gomock.Any(), ws, int64(3)).Return(nil, notFound)
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(archived, nil)
		}, wantStatus: 409},
		{name: "unknown endpoint", scope: scope, setup: func() {
			s.endpoints.EXPECT().ResumeEndpoint(gomock.Any(), ws, int64(3)).Return(nil, notFound)
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(nil, notFound)
		}, wantStatus: 404},
		{name: "no workspace", scope: services.Scope{}, wantStatus: 401},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setup != nil {
				tt.setup()
			}
			got, err := s.svc.ResumeEndpoint(context.Background(), tt.scope, 3)
			requireErr(&s.Suite, err, tt.wantStatus, nil)
			if tt.wantStatus == 0 {
				s.Nil(got.PausedAt)
			}
		})
	}
}

func (s *EndpointServiceSuite) TestArchiveEndpoint() {
	now := time.Now()
	archived := &models.Endpoint{ID: 3, WorkspaceID: ws, ArchivedAt: &now}
	notFound := fmt.Errorf("%w: none", apperr.ErrNotFound)
	tests := []struct {
		name          string
		scope         services.Scope
		setup         func()
		wantStatus    int
		wantCancelled int64
	}{
		{name: "archives and cancels unsent cycles in one transaction", scope: scope, wantCancelled: 2, setup: func() {
			s.endpoints.EXPECT().ArchiveEndpoint(gomock.Any(), ws, int64(3)).Return(archived, nil)
			s.deliveries.EXPECT().CancelDeliveriesForEndpoint(gomock.Any(), int64(3), services.ReasonEndpointArchived).Return(int64(2), nil)
			s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return([]string{"a"}, nil)
		}},
		{name: "archiving twice is a no-op", scope: scope, setup: func() {
			s.endpoints.EXPECT().ArchiveEndpoint(gomock.Any(), ws, int64(3)).Return(nil, notFound)
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(archived, nil)
			s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return(nil, nil)
		}},
		{name: "unknown endpoint", scope: scope, setup: func() {
			s.endpoints.EXPECT().ArchiveEndpoint(gomock.Any(), ws, int64(3)).Return(nil, notFound)
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(nil, notFound)
		}, wantStatus: 404},
		{name: "cancel failure rolls back", scope: scope, setup: func() {
			s.endpoints.EXPECT().ArchiveEndpoint(gomock.Any(), ws, int64(3)).Return(archived, nil)
			s.deliveries.EXPECT().CancelDeliveriesForEndpoint(gomock.Any(), int64(3), services.ReasonEndpointArchived).Return(int64(0), errors.New("boom"))
		}, wantStatus: 500},
		{name: "no workspace", scope: services.Scope{}, wantStatus: 401},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setup != nil {
				tt.setup()
			}
			got, cancelled, err := s.svc.ArchiveEndpoint(context.Background(), tt.scope, 3)
			requireErr(&s.Suite, err, tt.wantStatus, nil)
			if tt.wantStatus == 0 {
				s.NotNil(got.ArchivedAt)
				s.Equal(tt.wantCancelled, cancelled)
			}
		})
	}
}

func (s *EndpointServiceSuite) TestUpdateEndpoint() {
	now := time.Now()
	current := &models.Endpoint{ID: 3, WorkspaceID: ws, ApplicationID: 7, URL: "https://old.example.com/hooks", Description: "old", Revision: 1}
	archived := &models.Endpoint{ID: 3, WorkspaceID: ws, ApplicationID: 7, URL: current.URL, Revision: 1, ArchivedAt: &now}
	bumped := &models.Endpoint{ID: 3, WorkspaceID: ws, ApplicationID: 7, URL: "https://new.example.com/hooks", Description: "old", Revision: 2}
	notFound := fmt.Errorf("%w: none", apperr.ErrNotFound)
	str := func(v string) *string { return &v }
	list := func(v ...string) *[]string { return &v }
	tests := []struct {
		name       string
		scope      services.Scope
		in         services.UpdateEndpointInput
		setup      func()
		wantStatus int
		wantFields []string
		wantTypes  []string
	}{
		{name: "url change bumps the revision and keeps the subscriptions", scope: scope, in: services.UpdateEndpointInput{EndpointID: 3, URL: str(" https://new.example.com/hooks ")}, wantTypes: []string{"a"}, setup: func() {
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(current, nil)
			s.endpoints.EXPECT().UpdateEndpoint(gomock.Any(), ws, int64(3), "https://new.example.com/hooks", "old").Return(bumped, nil)
			s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return([]string{"a"}, nil)
		}},
		{name: "subscriptions are replaced in the same transaction", scope: scope, in: services.UpdateEndpointInput{EndpointID: 3, EventTypes: list("b", " a ", "a")}, wantTypes: []string{"a", "b"}, setup: func() {
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(current, nil)
			gomock.InOrder(
				s.endpoints.EXPECT().UpdateEndpoint(gomock.Any(), ws, int64(3), current.URL, "old").Return(bumped, nil),
				s.subs.EXPECT().ReplaceSubscriptions(gomock.Any(), int64(3), []string{"a", "b"}).Return(nil),
			)
			s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return([]string{"a", "b"}, nil)
		}},
		{name: "empty list subscribes to every type", scope: scope, in: services.UpdateEndpointInput{EndpointID: 3, EventTypes: list()}, wantTypes: []string{}, setup: func() {
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(current, nil)
			s.endpoints.EXPECT().UpdateEndpoint(gomock.Any(), ws, int64(3), current.URL, "old").Return(bumped, nil)
			s.subs.EXPECT().ReplaceSubscriptions(gomock.Any(), int64(3), []string{}).Return(nil)
			s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return([]string{}, nil)
		}},
		{name: "nothing to change", scope: scope, in: services.UpdateEndpointInput{EndpointID: 3}, wantStatus: 422, wantFields: []string{"body"}},
		{name: "invalid url", scope: scope, in: services.UpdateEndpointInput{EndpointID: 3, URL: str("ftp://x")}, wantStatus: 422, wantFields: []string{"url"}, setup: func() {
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(current, nil)
		}},
		{name: "invalid event type", scope: scope, in: services.UpdateEndpointInput{EndpointID: 3, EventTypes: list("*")}, wantStatus: 422, wantFields: []string{"event_types"}, setup: func() {
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(current, nil)
		}},
		{name: "archived endpoint", scope: scope, in: services.UpdateEndpointInput{EndpointID: 3, URL: str("https://new.example.com")}, wantStatus: 409, setup: func() {
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(archived, nil)
		}},
		{name: "unknown endpoint", scope: scope, in: services.UpdateEndpointInput{EndpointID: 3, URL: str("https://new.example.com")}, wantStatus: 404, setup: func() {
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(nil, notFound)
		}},
		{name: "no workspace", scope: services.Scope{}, in: services.UpdateEndpointInput{EndpointID: 3, URL: str("https://x")}, wantStatus: 401},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setup != nil {
				tt.setup()
			}
			got, err := s.svc.UpdateEndpoint(context.Background(), tt.scope, tt.in)
			requireErr(&s.Suite, err, tt.wantStatus, tt.wantFields)
			if tt.wantStatus != 0 {
				return
			}
			s.Equal(int32(2), got.Revision)
			s.Equal(tt.wantTypes, got.EventTypes)
		})
	}
}

func (s *EndpointServiceSuite) TestPrivateURLPolicy() {
	s.Run("private http is refused unless the sender allows it", func() {
		svc, err := services.NewEndpointService(services.EndpointServiceDeps{
			Tx: fakeTx{}, Applications: s.apps, Endpoints: s.endpoints, Deliveries: s.deliveries, Audit: s.audit, Secrets: s.secrets, Subscriptions: s.subs,
			URL: safehttp.Config{AllowedPorts: []int{443}}, Cipher: s.cipher, KeyID: "2026-11", Logger: slog.New(slog.DiscardHandler),
		})
		s.Require().NoError(err)
		for _, raw := range []string{"http://localhost:8081/hooks", "https://10.0.0.5/hooks", "https://169.254.169.254/latest", "https://receiver.example.com:8080/hooks"} {
			_, _, _, err := svc.CreateEndpoint(context.Background(), scope, services.CreateEndpointInput{ApplicationID: 7, URL: raw})
			requireErr(&s.Suite, err, 422, []string{"url"})
		}
	})
}

func (s *EndpointServiceSuite) TestRotateEndpointSecret() {
	now := time.Now()
	active := &models.Endpoint{ID: 3, WorkspaceID: ws}
	archived := &models.Endpoint{ID: 3, WorkspaceID: ws, ArchivedAt: &now}
	notFound := fmt.Errorf("%w: none", apperr.ErrNotFound)
	tests := []struct {
		name       string
		scope      services.Scope
		retireIn   time.Duration
		setup      func()
		wantStatus int
		wantFields []string
	}{
		{name: "new version, others retire after the overlap", scope: scope, retireIn: 24 * time.Hour, setup: func() {
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(active, nil)
			gomock.InOrder(
				s.secrets.EXPECT().CreateEndpointSecret(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, sec *models.EndpointSecret) error {
					s.Equal(int64(3), sec.EndpointID)
					plain, err := s.cipher.Decrypt(sec.Ciphertext)
					s.Require().NoError(err)
					s.True(strings.HasPrefix(string(plain), services.SecretPrefix))
					sec.Version = 2
					return nil
				}),
				s.secrets.EXPECT().RetireEndpointSecrets(gomock.Any(), int64(3), int32(2), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, _ int32, at time.Time) (int64, error) {
					s.WithinDuration(now.Add(24*time.Hour), at, 5*time.Second)
					return 1, nil
				}),
			)
			s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return([]string{"a"}, nil)
		}},
		{name: "emergency rotation retires the others now", scope: scope, retireIn: 0, setup: func() {
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(active, nil)
			s.secrets.EXPECT().CreateEndpointSecret(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, sec *models.EndpointSecret) error { sec.Version = 2; return nil })
			s.secrets.EXPECT().RetireEndpointSecrets(gomock.Any(), int64(3), int32(2), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, _ int32, at time.Time) (int64, error) {
				s.WithinDuration(now, at, 5*time.Second)
				return 1, nil
			})
			s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return(nil, nil)
		}},
		{name: "overlap too long", scope: scope, retireIn: 8 * 24 * time.Hour, wantStatus: 422, wantFields: []string{"retireIn"}},
		{name: "negative overlap", scope: scope, retireIn: -time.Second, wantStatus: 422, wantFields: []string{"retireIn"}},
		{name: "archived endpoint", scope: scope, retireIn: time.Hour, wantStatus: 409, setup: func() {
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(archived, nil)
		}},
		{name: "unknown endpoint", scope: scope, retireIn: time.Hour, wantStatus: 404, setup: func() {
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(nil, notFound)
		}},
		{name: "no workspace", scope: services.Scope{}, retireIn: time.Hour, wantStatus: 401},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setup != nil {
				tt.setup()
			}
			e, raw, version, retireAt, err := s.svc.RotateEndpointSecret(context.Background(), tt.scope, 3, tt.retireIn)
			requireErr(&s.Suite, err, tt.wantStatus, tt.wantFields)
			if tt.wantStatus != 0 {
				return
			}
			s.Equal(int64(3), e.ID)
			s.True(strings.HasPrefix(raw, services.SecretPrefix))
			s.Equal(int32(2), version)
			s.WithinDuration(now.Add(tt.retireIn), retireAt, 5*time.Second)
		})
	}
}

// TestAuditTrail pins that every endpoint mutation writes one audit row in its transaction, naming the action and
// the endpoint, and that the raw secret never lands in the details.
func (s *EndpointServiceSuite) TestAuditTrail() {
	now := time.Now()
	active := &models.Endpoint{ID: 3, WorkspaceID: ws, ApplicationID: 7, URL: "https://r.example.com", Revision: 1}
	tests := []struct {
		name   string
		run    func() (string, error)
		action string
	}{
		{name: "create", action: services.AuditEndpointCreate, run: func() (string, error) {
			s.expectApplication(true)
			s.endpoints.EXPECT().CreateEndpoint(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.Endpoint) error { e.ID = 3; return nil })
			s.secrets.EXPECT().CreateEndpointSecret(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, sec *models.EndpointSecret) error { sec.Version = 1; return nil })
			s.subs.EXPECT().CreateSubscriptions(gomock.Any(), int64(3), gomock.Any()).Return(nil)
			_, raw, _, err := s.svc.CreateEndpoint(context.Background(), scope, services.CreateEndpointInput{ApplicationID: 7, URL: "https://r.example.com"})
			return raw, err
		}},
		{name: "update", action: services.AuditEndpointUpdate, run: func() (string, error) {
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(active, nil)
			s.endpoints.EXPECT().UpdateEndpoint(gomock.Any(), ws, int64(3), "https://new.example.com", "").Return(&models.Endpoint{ID: 3, Revision: 2}, nil)
			s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return(nil, nil)
			url := "https://new.example.com"
			_, err := s.svc.UpdateEndpoint(context.Background(), scope, services.UpdateEndpointInput{EndpointID: 3, URL: &url})
			return "", err
		}},
		{name: "pause", action: services.AuditEndpointPause, run: func() (string, error) {
			s.endpoints.EXPECT().PauseEndpoint(gomock.Any(), ws, int64(3)).Return(&models.Endpoint{ID: 3, PausedAt: &now}, nil)
			s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return(nil, nil)
			_, err := s.svc.PauseEndpoint(context.Background(), scope, 3)
			return "", err
		}},
		{name: "resume", action: services.AuditEndpointResume, run: func() (string, error) {
			s.endpoints.EXPECT().ResumeEndpoint(gomock.Any(), ws, int64(3)).Return(active, nil)
			s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return(nil, nil)
			_, err := s.svc.ResumeEndpoint(context.Background(), scope, 3)
			return "", err
		}},
		{name: "archive", action: services.AuditEndpointArchive, run: func() (string, error) {
			s.endpoints.EXPECT().ArchiveEndpoint(gomock.Any(), ws, int64(3)).Return(&models.Endpoint{ID: 3, ArchivedAt: &now}, nil)
			s.deliveries.EXPECT().CancelDeliveriesForEndpoint(gomock.Any(), int64(3), services.ReasonEndpointArchived).Return(int64(2), nil)
			s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return(nil, nil)
			_, _, err := s.svc.ArchiveEndpoint(context.Background(), scope, 3)
			return "", err
		}},
		{name: "rotate", action: services.AuditEndpointRotate, run: func() (string, error) {
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(active, nil)
			s.secrets.EXPECT().CreateEndpointSecret(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, sec *models.EndpointSecret) error { sec.Version = 2; return nil })
			s.secrets.EXPECT().RetireEndpointSecrets(gomock.Any(), int64(3), int32(2), gomock.Any()).Return(int64(1), nil)
			s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return(nil, nil)
			_, raw, _, _, err := s.svc.RotateEndpointSecret(context.Background(), scope, 3, time.Hour)
			return raw, err
		}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			raw, err := tt.run()
			s.Require().NoError(err)
			s.Require().Len(s.audited, 1, "exactly one audit row")
			e := s.audited[0]
			s.Equal(tt.action, e.Action)
			s.Equal("endpoint", e.ResourceType)
			s.Equal(int64(3), *e.ResourceID)
			s.Equal(ws, e.WorkspaceID)
			s.Equal(actor, e.ActorID)
			if raw != "" {
				s.NotContains(string(e.Details), raw, "the secret never lands in the audit trail")
			}
		})
	}
	s.Run("a no-op pause writes nothing", func() {
		s.endpoints.EXPECT().PauseEndpoint(gomock.Any(), ws, int64(3)).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
		s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(&models.Endpoint{ID: 3, PausedAt: &now}, nil)
		s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return(nil, nil)
		_, err := s.svc.PauseEndpoint(context.Background(), scope, 3)
		s.Require().NoError(err)
		s.Empty(s.audited)
	})
}
