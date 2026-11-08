package services_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos/repo_mocks"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

const (
	ws    = "ws_1"
	actor = "u_ada"
)

var scope = services.Scope{WorkspaceID: ws, ActorID: actor}

// requireErr asserts an AppError with the status and fields, or no error when wantStatus is 0.
func requireErr(s *suite.Suite, err error, wantStatus int, wantFields []string) {
	s.T().Helper()
	if wantStatus == 0 {
		s.Require().NoError(err)
		return
	}
	var appErr *apperr.AppError
	s.Require().ErrorAs(err, &appErr, "%v", err)
	s.Equal(wantStatus, appErr.Code)
	for _, f := range wantFields {
		s.Contains(appErr.Fields, f)
	}
}

type ApplicationServiceSuite struct {
	suite.Suite
	apps    *repo_mocks.MockApplicationRepo
	envs    *repo_mocks.MockEnvironmentRepo
	audit   *repo_mocks.MockAuditRepo
	audited []models.AuditEvent
	svc     services.ApplicationService
}

func TestApplicationServiceSuite(t *testing.T) {
	suite.Run(t, new(ApplicationServiceSuite))
}

func (s *ApplicationServiceSuite) SetupSubTest() {
	ctrl := gomock.NewController(s.T())
	s.apps = repo_mocks.NewMockApplicationRepo(ctrl)
	s.apps.EXPECT().WithTx(gomock.Any()).Return(s.apps).AnyTimes()
	s.envs = repo_mocks.NewMockEnvironmentRepo(ctrl)
	s.audit = repo_mocks.NewMockAuditRepo(ctrl)
	s.audit.EXPECT().WithTx(gomock.Any()).Return(s.audit).AnyTimes()
	s.audited = nil
	s.audit.EXPECT().CreateAuditEvent(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.AuditEvent) error {
		s.audited = append(s.audited, *e)
		return nil
	}).AnyTimes()
	var err error
	s.svc, err = services.NewApplicationService(services.ApplicationServiceDeps{Tx: fakeTx{}, Applications: s.apps, Environments: s.envs, Audit: s.audit, Logger: slog.New(slog.DiscardHandler)})
	s.Require().NoError(err)
}

func (s *ApplicationServiceSuite) TestNew() {
	_, err := services.NewApplicationService(services.ApplicationServiceDeps{})
	s.Error(err)
}

func (s *ApplicationServiceSuite) TestCreateApplication() {
	tests := []struct {
		name       string
		scope      services.Scope
		appName    string
		setup      func()
		wantStatus int
		wantFields []string
	}{
		{name: "creates with a trimmed name in the default environment", scope: scope, appName: "  Customer A  ", setup: func() {
			s.envs.EXPECT().EnsureDefaultEnvironment(gomock.Any(), ws, actor).Return(&models.Environment{ID: 1, Kind: models.EnvironmentDevelopment}, nil)
			s.apps.EXPECT().CreateApplication(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, a *models.Application) error {
				s.Equal("Customer A", a.Name)
				s.Equal(ws, a.WorkspaceID)
				s.Equal(int64(1), a.EnvironmentID)
				s.Equal(models.EnvironmentDevelopment, a.EnvironmentKind)
				s.Equal(actor, a.CreatedBy)
				a.ID = 1
				return nil
			})
		}},
		{name: "no workspace", scope: services.Scope{ActorID: actor}, appName: "x", wantStatus: 401},
		{name: "blank name", scope: scope, appName: "  ", wantStatus: 422, wantFields: []string{"name"}},
		{name: "name too long", scope: scope, appName: strings.Repeat("x", 101), wantStatus: 422, wantFields: []string{"name"}},
		{name: "duplicate name", scope: scope, appName: "Customer A", setup: func() {
			s.envs.EXPECT().EnsureDefaultEnvironment(gomock.Any(), ws, actor).Return(&models.Environment{ID: 1, Kind: models.EnvironmentDevelopment}, nil)
			s.apps.EXPECT().CreateApplication(gomock.Any(), gomock.Any()).Return(fmt.Errorf("%w: dup", apperr.ErrConflict))
		}, wantStatus: 409},
		{name: "repo failure", scope: scope, appName: "Customer A", setup: func() {
			s.envs.EXPECT().EnsureDefaultEnvironment(gomock.Any(), ws, actor).Return(&models.Environment{ID: 1, Kind: models.EnvironmentDevelopment}, nil)
			s.apps.EXPECT().CreateApplication(gomock.Any(), gomock.Any()).Return(errors.New("boom"))
		}, wantStatus: 500},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setup != nil {
				tt.setup()
			}
			a, err := s.svc.CreateApplication(context.Background(), tt.scope, 0, tt.appName)
			requireErr(&s.Suite, err, tt.wantStatus, tt.wantFields)
			if tt.wantStatus == 0 {
				s.Equal(int64(1), a.ID)
			}
		})
	}
}

func (s *ApplicationServiceSuite) TestCreateApplicationInEnvironment() {
	s.Run("a given environment of the workspace", func() {
		s.envs.EXPECT().GetEnvironment(gomock.Any(), ws, int64(2)).Return(&models.Environment{ID: 2, Kind: models.EnvironmentProduction}, nil)
		s.apps.EXPECT().CreateApplication(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, a *models.Application) error {
			s.Equal(int64(2), a.EnvironmentID)
			s.Equal(models.EnvironmentProduction, a.EnvironmentKind)
			return nil
		})
		got, err := s.svc.CreateApplication(context.Background(), scope, 2, "Customer A")
		s.Require().NoError(err)
		s.Equal(models.EnvironmentProduction, got.EnvironmentKind)
	})
	s.Run("an environment of another workspace is not found", func() {
		s.envs.EXPECT().GetEnvironment(gomock.Any(), ws, int64(9)).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
		_, err := s.svc.CreateApplication(context.Background(), scope, 9, "Customer A")
		requireErr(&s.Suite, err, 404, nil)
	})
}

func (s *ApplicationServiceSuite) TestUpdateApplication() {
	on := true
	tests := []struct {
		name       string
		scope      services.Scope
		in         services.UpdateApplicationInput
		setup      func()
		wantStatus int
		wantAudit  bool
	}{
		{name: "enables previews and audits in the transaction", scope: scope, in: services.UpdateApplicationInput{ResponsePreviewsEnabled: &on}, setup: func() {
			s.apps.EXPECT().SetResponsePreviews(gomock.Any(), ws, int64(7), true).Return(&models.Application{ID: 7, ResponsePreviewsEnabled: true}, nil)
		}, wantAudit: true},
		{name: "nothing to change", scope: scope, in: services.UpdateApplicationInput{}, setup: func() {}, wantStatus: 422},
		{name: "another workspace", scope: scope, in: services.UpdateApplicationInput{ResponsePreviewsEnabled: &on}, setup: func() {
			s.apps.EXPECT().SetResponsePreviews(gomock.Any(), ws, int64(7), true).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
		}, wantStatus: 404},
		{name: "no workspace", scope: services.Scope{}, in: services.UpdateApplicationInput{ResponsePreviewsEnabled: &on}, setup: func() {}, wantStatus: 401},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			tt.setup()
			a, err := s.svc.UpdateApplication(context.Background(), tt.scope, 7, tt.in)
			requireErr(&s.Suite, err, tt.wantStatus, nil)
			if tt.wantStatus != 0 {
				s.Empty(s.audited)
				return
			}
			s.True(a.ResponsePreviewsEnabled)
			s.Require().Len(s.audited, 1)
			s.Equal(services.AuditApplicationUpdate, s.audited[0].Action)
			s.Equal(int64(7), *s.audited[0].ResourceID)
			s.JSONEq(`{"response_previews_enabled":true}`, string(s.audited[0].Details))
		})
	}
}

func (s *ApplicationServiceSuite) TestGetAndListApplications() {
	s.Run("get", func() {
		s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(&models.Application{ID: 7}, nil)
		a, err := s.svc.GetApplication(context.Background(), scope, 7)
		s.Require().NoError(err)
		s.Equal(int64(7), a.ID)
		s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(-1)).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
		_, err = s.svc.GetApplication(context.Background(), scope, -1)
		requireErr(&s.Suite, err, 404, nil)
	})
	s.Run("list checks the environment belongs to the workspace and never answers null", func() {
		s.envs.EXPECT().GetEnvironment(gomock.Any(), ws, int64(2)).Return(&models.Environment{ID: 2}, nil)
		s.apps.EXPECT().ListApplications(gomock.Any(), ws, int64(2)).Return(nil, nil)
		list, err := s.svc.ListApplications(context.Background(), scope, 2)
		s.Require().NoError(err)
		s.NotNil(list)
		s.Empty(list)
		s.envs.EXPECT().GetEnvironment(gomock.Any(), ws, int64(9)).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
		_, err = s.svc.ListApplications(context.Background(), scope, 9)
		requireErr(&s.Suite, err, 404, nil)
	})
}
