// Package services holds the data plane's business rules; it knows no SQL and no transport.
package services

//go:generate mockgen -source=applications.go -destination=service_mocks/mock_applications.go -package=service_mocks

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

// Scope is the workspace a call acts in and the user acting, empty for an API key.
type Scope struct {
	WorkspaceID string
	ActorID     string
}

const maxNameLength = 100

// ApplicationService manages consumer applications.
type ApplicationService interface {
	// CreateApplication creates the application in environmentID, or in the workspace's development
	// environment (created on first use) when environmentID is 0.
	CreateApplication(ctx context.Context, scope Scope, environmentID int64, name string) (*models.Application, error)
}

// ApplicationServiceDeps are the dependencies; all are required.
type ApplicationServiceDeps struct {
	Applications repos.ApplicationRepo
	Environments repos.EnvironmentRepo
	Logger       *slog.Logger
}

type applicationService struct {
	deps ApplicationServiceDeps
}

// NewApplicationService returns an ApplicationService or an error if a dependency is missing.
func NewApplicationService(deps ApplicationServiceDeps) (ApplicationService, error) {
	if deps.Applications == nil || deps.Environments == nil || deps.Logger == nil {
		return nil, errors.New("services: Applications, Environments and Logger are required")
	}
	return &applicationService{deps: deps}, nil
}

func (s *applicationService) CreateApplication(ctx context.Context, scope Scope, environmentID int64, name string) (*models.Application, error) {
	if scope.WorkspaceID == "" {
		return nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxNameLength {
		return nil, apperr.NewValidation(map[string]string{"name": "must be 1 to 100 characters"})
	}
	var env *models.Environment
	var err error
	if environmentID == 0 {
		env, err = s.deps.Environments.EnsureDefaultEnvironment(ctx, scope.WorkspaceID, scope.ActorID)
	} else {
		env, err = s.deps.Environments.GetEnvironment(ctx, scope.WorkspaceID, environmentID)
	}
	if err != nil {
		return nil, endpointError("environment", err)
	}
	a := &models.Application{WorkspaceID: scope.WorkspaceID, EnvironmentID: env.ID, EnvironmentKind: env.Kind, Name: name, CreatedBy: scope.ActorID}
	if err := s.deps.Applications.CreateApplication(ctx, a); err != nil {
		if errors.Is(err, apperr.ErrConflict) {
			return nil, apperr.NewConflict("an application with this name exists in the environment", err)
		}
		return nil, apperr.NewInternal(err)
	}
	return a, nil
}
