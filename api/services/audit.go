package services

//go:generate mockgen -source=audit.go -destination=service_mocks/mock_audit.go -package=service_mocks

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"time"

	"github.com/bete7512/hookbeam/pkg/depcheck"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/logger"
)

// Audit actions the control plane records.
const (
	AuditKeyCreate     = "api_key.create"
	AuditKeyRevoke     = "api_key.revoke"
	AuditMemberRole    = "member.role"
	AuditOrgInvite     = "org.invite"
	AuditOrgChangeRole = "org.change_role"
	AuditOrgRemove     = "org.remove_member"
)

// Sources of merged audit rows.
const (
	AuditSourceControl  = "control"
	AuditSourceDelivery = "delivery"
)

// recordAudit writes one audit event through repo (bound to the caller's transaction when there is one).
func recordAudit(ctx context.Context, repo repos.AuditRepo, actor Actor, action, resourceType string, resourceID int64, details any) error {
	payload, err := json.Marshal(details)
	if err != nil {
		return err
	}
	e := &models.AuditEvent{
		WorkspaceID:  actor.WorkspaceID,
		ActorID:      actor.UserID,
		Action:       action,
		ResourceType: resourceType,
		RequestID:    logger.RequestIDFrom(ctx),
		Details:      payload,
	}
	if resourceID != 0 {
		e.ResourceID = &resourceID
	}
	return repo.CreateAuditEvent(ctx, e)
}

// ListAuditInput filters and pages the merged audit list.
type ListAuditInput struct {
	ActorID      string
	ResourceType string
	ResourceID   int64
	After        *time.Time
	Before       *time.Time
	Limit        int
	Offset       int
}

// AuditService records the control plane's own events, lists them merged with the data plane's, and prunes them.
type AuditService interface {
	// Record writes an event outside any transaction, for actions another system already committed.
	Record(ctx context.Context, actor Actor, action, resourceType string, resourceID int64, details any) error
	// List returns a page of both planes' events for the actor's workspace, newest first, and the total.
	List(ctx context.Context, actor Actor, in ListAuditInput) ([]models.AuditEvent, int, error)
	// Prune deletes events older than the retention, batch by batch, and returns how many.
	Prune(ctx context.Context) (int64, error)
}

// AuditServiceDeps are the dependencies; all are required.
type AuditServiceDeps struct {
	Audit     repos.AuditRepo
	Delivery  DeliveryService
	Authz     Authorizer
	Retention time.Duration
	BatchSize int
	Logger    *slog.Logger
}

type auditService struct {
	deps AuditServiceDeps
}

// NewAuditService returns an AuditService or an error if a dependency is missing.
func NewAuditService(deps AuditServiceDeps) (AuditService, error) {
	if err := depcheck.Required("services", deps); err != nil {
		return nil, err
	}
	return &auditService{deps: deps}, nil
}

func (s *auditService) Record(ctx context.Context, actor Actor, action, resourceType string, resourceID int64, details any) error {
	if err := recordAudit(ctx, s.deps.Audit, actor, action, resourceType, resourceID, details); err != nil {
		return apperr.NewInternal(err)
	}
	return nil
}

func (s *auditService) List(ctx context.Context, actor Actor, in ListAuditInput) ([]models.AuditEvent, int, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapReadMetadata); err != nil {
		return nil, 0, err
	}
	if in.Limit < 1 || in.Limit > 100 || in.Offset < 0 {
		return nil, 0, apperr.NewValidation(map[string]string{"limit": "must be between 1 and 100 with a non-negative offset"})
	}
	// Both planes are asked for the first offset+limit rows; the merge then takes the page.
	window := in.Offset + in.Limit
	local, err := s.deps.Audit.ListAuditEvents(ctx, repos.ListAuditOpts{
		WorkspaceID:  actor.WorkspaceID,
		ActorID:      in.ActorID,
		ResourceType: in.ResourceType,
		ResourceID:   in.ResourceID,
		After:        in.After,
		Before:       in.Before,
		Limit:        window,
	})
	if err != nil {
		return nil, 0, apperr.NewInternal(err)
	}
	localTotal, err := s.deps.Audit.CountAuditEvents(ctx, repos.ListAuditOpts{
		WorkspaceID:  actor.WorkspaceID,
		ActorID:      in.ActorID,
		ResourceType: in.ResourceType,
		ResourceID:   in.ResourceID,
		After:        in.After,
		Before:       in.Before,
	})
	if err != nil {
		return nil, 0, apperr.NewInternal(err)
	}
	for i := range local {
		local[i].Source = AuditSourceControl
	}
	remote, remoteTotal, err := s.deps.Delivery.ListAuditEvents(ctx, actor.WorkspaceID, ListAuditInput{
		ActorID:      in.ActorID,
		ResourceType: in.ResourceType,
		ResourceID:   in.ResourceID,
		After:        in.After,
		Before:       in.Before,
		Limit:        window,
	})
	if err != nil {
		return nil, 0, err
	}
	for i := range remote {
		remote[i].Source = AuditSourceDelivery
	}
	merged := make([]models.AuditEvent, 0, len(local)+len(remote))
	merged = append(merged, local...)
	merged = append(merged, remote...)
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].OccurredAt.After(merged[j].OccurredAt) })
	if in.Offset >= len(merged) {
		return []models.AuditEvent{}, localTotal + remoteTotal, nil
	}
	end := min(in.Offset+in.Limit, len(merged))
	return merged[in.Offset:end], localTotal + remoteTotal, nil
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
