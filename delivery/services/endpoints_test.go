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
	"github.com/bete7512/hookbeam/pkg/secrets"
)

type EndpointServiceSuite struct {
	suite.Suite
	apps      *repo_mocks.MockApplicationRepo
	endpoints *repo_mocks.MockEndpointRepo
	secrets   *repo_mocks.MockEndpointSecretRepo
	subs      *repo_mocks.MockSubscriptionRepo
	cipher    *secrets.Cipher
	svc       services.EndpointService
}

func TestEndpointServiceSuite(t *testing.T) {
	suite.Run(t, new(EndpointServiceSuite))
}

func (s *EndpointServiceSuite) SetupSubTest() {
	ctrl := gomock.NewController(s.T())
	s.apps = repo_mocks.NewMockApplicationRepo(ctrl)
	s.endpoints = repo_mocks.NewMockEndpointRepo(ctrl)
	s.endpoints.EXPECT().WithTx(gomock.Any()).Return(s.endpoints).AnyTimes()
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
		Tx: fakeTx{}, Applications: s.apps, Endpoints: s.endpoints, Secrets: s.secrets, Subscriptions: s.subs,
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
