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
	apps *repo_mocks.MockApplicationRepo
	svc  services.ApplicationService
}

func TestApplicationServiceSuite(t *testing.T) {
	suite.Run(t, new(ApplicationServiceSuite))
}

func (s *ApplicationServiceSuite) SetupSubTest() {
	s.apps = repo_mocks.NewMockApplicationRepo(gomock.NewController(s.T()))
	var err error
	s.svc, err = services.NewApplicationService(services.ApplicationServiceDeps{Applications: s.apps, Logger: slog.New(slog.DiscardHandler)})
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
		{name: "creates with a trimmed name", scope: scope, appName: "  Customer A  ", setup: func() {
			s.apps.EXPECT().CreateApplication(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, a *models.Application) error {
				s.Equal("Customer A", a.Name)
				s.Equal(ws, a.WorkspaceID)
				s.Equal(actor, a.CreatedBy)
				a.ID = 1
				return nil
			})
		}},
		{name: "no workspace", scope: services.Scope{ActorID: actor}, appName: "x", wantStatus: 401},
		{name: "blank name", scope: scope, appName: "  ", wantStatus: 422, wantFields: []string{"name"}},
		{name: "name too long", scope: scope, appName: strings.Repeat("x", 101), wantStatus: 422, wantFields: []string{"name"}},
		{name: "duplicate name", scope: scope, appName: "Customer A", setup: func() {
			s.apps.EXPECT().CreateApplication(gomock.Any(), gomock.Any()).Return(fmt.Errorf("%w: dup", apperr.ErrConflict))
		}, wantStatus: 409},
		{name: "repo failure", scope: scope, appName: "Customer A", setup: func() {
			s.apps.EXPECT().CreateApplication(gomock.Any(), gomock.Any()).Return(errors.New("boom"))
		}, wantStatus: 500},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setup != nil {
				tt.setup()
			}
			a, err := s.svc.CreateApplication(context.Background(), tt.scope, tt.appName)
			requireErr(&s.Suite, err, tt.wantStatus, tt.wantFields)
			if tt.wantStatus == 0 {
				s.Equal(int64(1), a.ID)
			}
		})
	}
}
