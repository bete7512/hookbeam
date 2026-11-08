package services

//go:generate mockgen -source=audit.go -destination=service_mocks/mock_audit.go -package=service_mocks

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/logger"
)

// Audit actions the data plane records.
const (
	AuditEndpointCreate    = "endpoint.create"
	AuditEndpointUpdate    = "endpoint.update"
	AuditEndpointPause     = "endpoint.pause"
	AuditEndpointResume    = "endpoint.resume"
	AuditEndpointArchive   = "endpoint.archive"
	AuditEndpointRotate    = "endpoint.rotate_secret"
	AuditDeliveryCancel    = "delivery.cancel"
	AuditDeliveryReplay    = "delivery.replay"
	AuditMessagePayload    = "message.payload_read"
	AuditEnvironmentCreate = "environment.create"
	AuditApplicationCreate = "application.create"
	AuditApplicationUpdate = "application.update"
)

// recordAudit writes one audit event through repo (bound to the caller's transaction) with the request id from ctx.
// details must hold no secret or payload bytes.
func recordAudit(ctx context.Context, repo repos.AuditRepo, scope Scope, action, resourceType string, resourceID int64, details any) error {
	payload, err := json.Marshal(details)
	if err != nil {
		return err
	}
	e := &models.AuditEvent{WorkspaceID: scope.WorkspaceID, ActorID: scope.ActorID, Action: action, ResourceType: resourceType, RequestID: logger.RequestIDFrom(ctx), Details: payload}
	if resourceID != 0 {
		e.ResourceID = &resourceID
	}
	return repo.CreateAuditEvent(ctx, e)
}

// AuditService reads and prunes the data plane's audit trail.
type AuditService interface {
	ListAuditEvents(ctx context.Context, scope Scope, opts repos.ListAuditOpts) ([]models.AuditEvent, int, error)
	// Prune deletes events older than the retention, batch by batch, and returns how many.
	Prune(ctx context.Context) (int64, error)
}

// AuditServiceDeps are the dependencies; all are required.
type AuditServiceDeps struct {
	Audit     repos.AuditRepo
	Retention time.Duration
	BatchSize int
	Logger    *slog.Logger
}

type auditService struct {
	deps AuditServiceDeps
}

// NewAuditService returns an AuditService or an error if a dependency is missing.
func NewAuditService(deps AuditServiceDeps) (AuditService, error) {
	if deps.Audit == nil || deps.Retention <= 0 || deps.BatchSize <= 0 || deps.Logger == nil {
		return nil, errors.New("services: Audit, positive Retention and BatchSize, Logger are required")
	}
	return &auditService{deps: deps}, nil
}

func (s *auditService) ListAuditEvents(ctx context.Context, scope Scope, opts repos.ListAuditOpts) ([]models.AuditEvent, int, error) {
	if scope.WorkspaceID == "" {
		return nil, 0, apperr.NewUnauthorized("a workspace is required", nil)
	}
	if opts.Limit < 1 || opts.Limit > 100 || opts.Offset < 0 {
		return nil, 0, apperr.NewValidation(map[string]string{"limit": "must be between 1 and 100 with a non-negative offset"})
	}
	opts.WorkspaceID = scope.WorkspaceID
	items, err := s.deps.Audit.ListAuditEvents(ctx, opts)
	if err != nil {
		return nil, 0, apperr.NewInternal(err)
	}
	total, err := s.deps.Audit.CountAuditEvents(ctx, opts)
	if err != nil {
		return nil, 0, apperr.NewInternal(err)
	}
	return items, total, nil
}

func (s *auditService) Prune(ctx context.Context) (int64, error) {
	before := time.Now().Add(-s.deps.Retention)
	var total int64
	for {
		n, err := s.deps.Audit.DeleteAuditEventsBefore(ctx, before, s.deps.BatchSize)
		total += n
		if err != nil {
			return total, apperr.NewInternal(err)
		}
		if n < int64(s.deps.BatchSize) {
			return total, nil
		}
	}
}
