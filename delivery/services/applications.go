// Package services holds the data plane's business rules; it knows no SQL and no transport.
package services

//go:generate mockgen -source=applications.go -destination=service_mocks/mock_applications.go -package=service_mocks

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/db"
)

// UpdateApplicationInput carries the settings a change may touch; nil leaves one as it is.
type UpdateApplicationInput struct {
	ResponsePreviewsEnabled *bool
}

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
	// UpdateApplication changes the application's settings and audits the change in the same transaction.
	UpdateApplication(ctx context.Context, scope Scope, id int64, in UpdateApplicationInput) (*models.Application, error)
	GetApplication(ctx context.Context, scope Scope, id int64) (*models.Application, error)
	// ListApplications returns the environment's applications; the environment must belong to the workspace.
	ListApplications(ctx context.Context, scope Scope, environmentID int64) ([]models.Application, error)
	// ArchiveApplication retires the application, archives its endpoints and cancels their unsent deliveries in
	// one transaction; it returns the application and both counts. Archiving twice changes nothing.
	ArchiveApplication(ctx context.Context, scope Scope, id int64) (*models.Application, int64, int64, error)
}

// ApplicationServiceDeps are the dependencies; all are required.
type ApplicationServiceDeps struct {
	Tx           db.Transactor
	Applications repos.ApplicationRepo
	Environments repos.EnvironmentRepo
	Endpoints    repos.EndpointRepo
	Deliveries   repos.DeliveryRepo
	Audit        repos.AuditRepo
	Logger       *slog.Logger
}

type applicationService struct {
	deps ApplicationServiceDeps
}

// NewApplicationService returns an ApplicationService or an error if a dependency is missing.
func NewApplicationService(deps ApplicationServiceDeps) (ApplicationService, error) {
	if deps.Tx == nil || deps.Applications == nil || deps.Environments == nil || deps.Endpoints == nil || deps.Deliveries == nil || deps.Audit == nil || deps.Logger == nil {
		return nil, errors.New("services: Tx, Applications, Environments, Endpoints, Deliveries, Audit and Logger are required")
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
	err = s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.deps.Applications.WithTx(tx).CreateApplication(ctx, a); err != nil {
			return err
		}
		return recordAudit(ctx, s.deps.Audit.WithTx(tx), scope, AuditApplicationCreate, "application", a.ID, map[string]any{"name": a.Name, "environment_id": a.EnvironmentID})
	})
	if err != nil {
		if errors.Is(err, apperr.ErrConflict) {
			return nil, apperr.NewConflict("an application with this name exists in the environment", err)
		}
		return nil, apperr.NewInternal(err)
	}
	return a, nil
}

func (s *applicationService) UpdateApplication(ctx context.Context, scope Scope, id int64, in UpdateApplicationInput) (*models.Application, error) {
	if scope.WorkspaceID == "" {
		return nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	if in.ResponsePreviewsEnabled == nil {
		return nil, apperr.NewValidation(map[string]string{"responsePreviewsEnabled": "at least one setting is required"})
	}
	var a *models.Application
	err := s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		if a, err = s.deps.Applications.WithTx(tx).SetResponsePreviews(ctx, scope.WorkspaceID, id, *in.ResponsePreviewsEnabled); err != nil {
			return err
		}
		return recordAudit(ctx, s.deps.Audit.WithTx(tx), scope, AuditApplicationUpdate, "application", a.ID, map[string]any{"response_previews_enabled": *in.ResponsePreviewsEnabled})
	})
	if err != nil {
		return nil, endpointError("application", err)
	}
	return a, nil
}

func (s *applicationService) ArchiveApplication(ctx context.Context, scope Scope, id int64) (*models.Application, int64, int64, error) {
	if scope.WorkspaceID == "" {
		return nil, 0, 0, apperr.NewUnauthorized("a workspace is required", nil)
	}
	var a *models.Application
	var endpoints, cancelled int64
	err := s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		if a, err = s.deps.Applications.WithTx(tx).ArchiveApplication(ctx, scope.WorkspaceID, id); err != nil {
			return err
		}
		if endpoints, err = s.deps.Endpoints.WithTx(tx).ArchiveEndpointsForApplication(ctx, a.ID); err != nil {
			return err
		}
		if cancelled, err = s.deps.Deliveries.WithTx(tx).CancelDeliveriesForApplication(ctx, a.ID, ReasonEndpointArchived); err != nil {
			return err
		}
		return recordAudit(ctx, s.deps.Audit.WithTx(tx), scope, AuditApplicationArchive, "application", a.ID, map[string]any{"archived_endpoints": endpoints, "cancelled_deliveries": cancelled})
	})
	if errors.Is(err, apperr.ErrNotFound) {
		// Already archived, or not ours.
		if a, err = s.deps.Applications.GetApplication(ctx, scope.WorkspaceID, id); err != nil {
			return nil, 0, 0, endpointError("application", err)
		}
		return a, 0, 0, nil
	}
	if err != nil {
		return nil, 0, 0, apperr.NewInternal(err)
	}
	return a, endpoints, cancelled, nil
}

func (s *applicationService) GetApplication(ctx context.Context, scope Scope, id int64) (*models.Application, error) {
	if scope.WorkspaceID == "" {
		return nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	a, err := s.deps.Applications.GetApplication(ctx, scope.WorkspaceID, id)
	if err != nil {
		return nil, endpointError("application", err)
	}
	return a, nil
}

func (s *applicationService) ListApplications(ctx context.Context, scope Scope, environmentID int64) ([]models.Application, error) {
	if scope.WorkspaceID == "" {
		return nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	if _, err := s.deps.Environments.GetEnvironment(ctx, scope.WorkspaceID, environmentID); err != nil {
		return nil, endpointError("environment", err)
	}
	list, err := s.deps.Applications.ListApplications(ctx, scope.WorkspaceID, environmentID)
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	if list == nil {
		list = []models.Application{}
	}
	return list, nil
}
