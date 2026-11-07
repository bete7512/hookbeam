package services

//go:generate mockgen -source=dispatch.go -destination=service_mocks/mock_dispatch.go -package=service_mocks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/secrets"
	"github.com/bete7512/hookbeam/pkg/webhooks"
)

const userAgent = "Hookbeam/1"

// DispatchService claims due deliveries and attempts them.
type DispatchService interface {
	// Dispatch claims one batch under a fresh lease, attempts every delivery and records the outcomes.
	// It returns how many deliveries it claimed, so the caller knows whether more may be due.
	Dispatch(ctx context.Context) (int, error)
}

// DispatchServiceDeps are the dependencies; all are required.
type DispatchServiceDeps struct {
	Tx         db.Transactor
	Deliveries repos.DeliveryRepo
	Attempts   repos.AttemptRepo
	Messages   repos.MessageRepo
	Endpoints  repos.EndpointRepo
	Secrets    repos.EndpointSecretRepo
	Cipher     *secrets.Cipher
	Sender     Sender
	Policy     Policy
	LeaseTTL   time.Duration
	BatchSize  int
	Logger     *slog.Logger
}

type dispatchService struct {
	deps DispatchServiceDeps
}

// NewDispatchService returns a DispatchService or an error if a dependency is missing.
func NewDispatchService(deps DispatchServiceDeps) (DispatchService, error) {
	if deps.Tx == nil || deps.Deliveries == nil || deps.Attempts == nil || deps.Messages == nil || deps.Endpoints == nil || deps.Secrets == nil ||
		deps.Cipher == nil || deps.Sender == nil || deps.Policy.MaxAttempts <= 0 || deps.LeaseTTL <= 0 || deps.BatchSize <= 0 || deps.Logger == nil {
		return nil, errors.New("services: Tx, repos, Cipher, Sender, Policy, positive LeaseTTL and BatchSize, Logger are required")
	}
	return &dispatchService{deps: deps}, nil
}

func (s *dispatchService) Dispatch(ctx context.Context) (int, error) {
	token := uuid.NewString()
	claimed, err := s.deps.Deliveries.ClaimDueDeliveries(ctx, s.deps.BatchSize, token, s.deps.LeaseTTL)
	if err != nil {
		return 0, apperr.NewInternal(err)
	}
	var errs []error
	for i := range claimed {
		if err := s.attempt(ctx, &claimed[i], token); err != nil {
			errs = append(errs, fmt.Errorf("delivery %d: %w", claimed[i].ID, err))
		}
	}
	return len(claimed), errors.Join(errs...)
}

// attempt sends one claimed delivery and records the attempt and the outcome in one transaction.
// An error leaves the lease to expire; the sweep reschedules it.
func (s *dispatchService) attempt(ctx context.Context, d *models.Delivery, token string) error {
	msg, err := s.deps.Messages.GetMessageByID(ctx, d.MessageID)
	if err != nil {
		return err
	}
	endpoint, err := s.deps.Endpoints.GetEndpointByID(ctx, d.EndpointID)
	if err != nil {
		return err
	}
	if endpoint.ArchivedAt != nil {
		reason := ReasonEndpointArchived
		return s.deps.Deliveries.RecordOutcome(ctx, models.DeliveryOutcome{DeliveryID: d.ID, LeaseToken: token, State: models.DeliveryCancelled, FailureReason: &reason})
	}
	active, err := s.deps.Secrets.ListActiveEndpointSecrets(ctx, endpoint.ID)
	if err != nil {
		return err
	}
	if len(active) == 0 {
		return fmt.Errorf("endpoint %d has no active signing secret", endpoint.ID)
	}

	started := time.Now()
	headers, err := s.sign(active, msg, started)
	if err != nil {
		return err
	}
	res := s.deps.Sender.Send(ctx, SendRequest{URL: endpoint.URL, Headers: headers, Body: msg.Payload})
	completed := time.Now()
	decision := s.deps.Policy.Decide(res, d.AttemptCount, d.DeadlineAt, completed)

	a := &models.Attempt{
		DeliveryID:       d.ID,
		Number:           d.AttemptCount,
		EndpointRevision: endpoint.Revision,
		SecretVersion:    active[0].Version,
		RequestURL:       endpoint.URL,
		StartedAt:        started,
		CompletedAt:      &completed,
		LatencyMS:        int32(min(completed.Sub(started).Milliseconds(), 1<<31-1)), //nolint:gosec // clamped
		ResponseHeaders:  res.Headers,
	}
	if res.StatusCode > 0 {
		code := int32(res.StatusCode) //nolint:gosec // HTTP status
		a.StatusCode = &code
	}
	if res.ErrorClass != "" {
		a.ErrorClass = &res.ErrorClass
	}
	if res.BodyPreview != "" {
		a.ResponsePreview = &res.BodyPreview
	}
	err = s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.deps.Attempts.WithTx(tx).CreateAttempt(ctx, a); err != nil {
			return err
		}
		return s.deps.Deliveries.WithTx(tx).RecordOutcome(ctx, models.DeliveryOutcome{DeliveryID: d.ID, LeaseToken: token, State: decision.State, DueAt: decision.DueAt, FailureReason: decision.Reason})
	})
	if errors.Is(err, apperr.ErrNotFound) {
		s.deps.Logger.WarnContext(ctx, "lease lost before the outcome was recorded", "delivery_id", d.ID, "attempt", d.AttemptCount)
		return nil
	}
	if err != nil {
		return err
	}
	s.deps.Logger.InfoContext(ctx, "delivery attempt", "delivery_id", d.ID, "attempt", d.AttemptCount, "status", res.StatusCode, "error_class", res.ErrorClass, "state", decision.State)
	return nil
}

// sign builds the Standard Webhooks headers, one signature per active secret (newest first).
func (s *dispatchService) sign(active []models.EndpointSecret, msg *models.Message, at time.Time) (map[string]string, error) {
	sigs := make([]string, 0, len(active))
	for i := range active {
		plain, err := s.deps.Cipher.Decrypt(active[i].Ciphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt secret version %d: %w", active[i].Version, err)
		}
		sig, err := webhooks.Sign(string(plain), msg.PublicID, at, msg.Payload)
		if err != nil {
			return nil, err
		}
		sigs = append(sigs, sig)
	}
	return map[string]string{
		"content-type":           "application/json",
		"user-agent":             userAgent,
		webhooks.HeaderID:        msg.PublicID,
		webhooks.HeaderTimestamp: fmt.Sprint(at.Unix()),
		webhooks.HeaderSignature: strings.Join(sigs, " "),
	}, nil
}
