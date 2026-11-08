package services_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos/repo_mocks"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

type EnvironmentServiceSuite struct {
	suite.Suite
	envs *repo_mocks.MockEnvironmentRepo
	svc  services.EnvironmentService
}

func TestEnvironmentServiceSuite(t *testing.T) {
	suite.Run(t, new(EnvironmentServiceSuite))
}

func (s *EnvironmentServiceSuite) SetupSubTest() {
	s.envs = repo_mocks.NewMockEnvironmentRepo(gomock.NewController(s.T()))
	var err error
	s.svc, err = services.NewEnvironmentService(services.EnvironmentServiceDeps{Environments: s.envs, Logger: slog.New(slog.DiscardHandler)})
	s.Require().NoError(err)
}

func (s *EnvironmentServiceSuite) TestNew() {
	_, err := services.NewEnvironmentService(services.EnvironmentServiceDeps{})
	s.Error(err)
}

func (s *EnvironmentServiceSuite) TestCreateEnvironment() {
	tests := []struct {
		name       string
		scope      services.Scope
		envName    string
		kind       string
		setup      func()
		wantStatus int
		wantFields []string
	}{
		{name: "creates production", scope: scope, envName: " production ", kind: "production", setup: func() {
			s.envs.EXPECT().CreateEnvironment(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.Environment) error {
				s.Equal("production", e.Name)
				s.Equal(models.EnvironmentProduction, e.Kind)
				s.Equal(actor, e.CreatedBy)
				e.ID = 2
				return nil
			})
		}},
		{name: "bad kind and empty name", scope: scope, envName: " ", kind: "staging", wantStatus: 422, wantFields: []string{"name", "kind"}},
		{name: "duplicate name", scope: scope, envName: "production", kind: "production", setup: func() {
			s.envs.EXPECT().CreateEnvironment(gomock.Any(), gomock.Any()).Return(fmt.Errorf("%w: dup", apperr.ErrConflict))
		}, wantStatus: 409},
		{name: "repo failure", scope: scope, envName: "production", kind: "production", setup: func() {
			s.envs.EXPECT().CreateEnvironment(gomock.Any(), gomock.Any()).Return(errors.New("boom"))
		}, wantStatus: 500},
		{name: "no workspace", scope: services.Scope{}, envName: "production", kind: "production", wantStatus: 401},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setup != nil {
				tt.setup()
			}
			got, err := s.svc.CreateEnvironment(context.Background(), tt.scope, tt.envName, tt.kind)
			requireErr(&s.Suite, err, tt.wantStatus, tt.wantFields)
			if tt.wantStatus == 0 {
				s.Equal(int64(2), got.ID)
			}
		})
	}
}

func (s *EnvironmentServiceSuite) TestListEnvironments() {
	s.Run("lists, creating the development environment on first use", func() {
		s.envs.EXPECT().EnsureDefaultEnvironment(gomock.Any(), ws, actor).Return(&models.Environment{ID: 1}, nil)
		s.envs.EXPECT().ListEnvironments(gomock.Any(), ws).Return([]models.Environment{{ID: 1}, {ID: 2}}, nil)
		got, err := s.svc.ListEnvironments(context.Background(), scope)
		s.Require().NoError(err)
		s.Len(got, 2)
	})
	s.Run("no workspace", func() {
		_, err := s.svc.ListEnvironments(context.Background(), services.Scope{})
		requireErr(&s.Suite, err, 401, nil)
	})
}
