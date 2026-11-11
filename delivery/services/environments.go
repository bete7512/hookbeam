package services

//go:generate mockgen -source=environments.go -destination=service_mocks/mock_environments.go -package=service_mocks

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/bete7512/hookbeam/pkg/depcheck"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

// EnvironmentService manages a workspace's environments.
type EnvironmentService interface {
	CreateEnvironment(ctx context.Context, scope Scope, name, kind string) (*models.Environment, error)
	ListEnvironments(ctx context.Context, scope Scope) ([]models.Environment, error)
}

// EnvironmentServiceDeps are the dependencies; all are required.
type EnvironmentServiceDeps struct {
	Environments repos.EnvironmentRepo
	Logger       *slog.Logger
}

type environmentService struct {
	deps EnvironmentServiceDeps
}

// NewEnvironmentService returns an EnvironmentService or an error if a dependency is missing.
func NewEnvironmentService(deps EnvironmentServiceDeps) (EnvironmentService, error) {
	if err := depcheck.Required("services", deps); err != nil {
		return nil, err
	}
	return &environmentService{deps: deps}, nil
}

func (s *environmentService) CreateEnvironment(ctx context.Context, scope Scope, name, kind string) (*models.Environment, error) {
	if scope.WorkspaceID == "" {
		return nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	name = strings.TrimSpace(name)
	fields := map[string]string{}
	if name == "" || utf8.RuneCountInString(name) > maxNameLength {
		fields["name"] = "must be 1 to 100 characters"
	}
	if kind != models.EnvironmentDevelopment && kind != models.EnvironmentProduction {
		fields["kind"] = "must be development or production"
	}
	if len(fields) > 0 {
		return nil, apperr.NewValidation(fields)
	}
	e := &models.Environment{WorkspaceID: scope.WorkspaceID, Name: name, Kind: kind, CreatedBy: scope.ActorID}
	if err := s.deps.Environments.CreateEnvironment(ctx, e); err != nil {
		if errors.Is(err, apperr.ErrConflict) {
			return nil, apperr.NewConflict("an environment with this name exists in the workspace", err)
		}
		return nil, apperr.NewInternal(err)
	}
	return e, nil
}

func (s *environmentService) ListEnvironments(ctx context.Context, scope Scope) ([]models.Environment, error) {
	if scope.WorkspaceID == "" {
		return nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	// Every workspace has a development environment; listing is its first use for a workspace that never
	// created an application, so the console's environment indicator always has something to show.
	if _, err := s.deps.Environments.EnsureDefaultEnvironment(ctx, scope.WorkspaceID, scope.ActorID); err != nil {
		return nil, apperr.NewInternal(err)
	}
	list, err := s.deps.Environments.ListEnvironments(ctx, scope.WorkspaceID)
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	return list, nil
}
