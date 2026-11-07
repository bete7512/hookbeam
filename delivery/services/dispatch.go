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

const (
	userAgent = "Hookbeam/1"
	// ErrorClassUnknown marks an attempt whose result was never recorded: the worker lost its lease mid-attempt.
	ErrorClassUnknown = "unknown"
)

// DispatchService claims due deliveries and attempts them.
type DispatchService interface {
	// Dispatch claims one batch under a fresh lease, attempts every delivery and records the outcomes.
	// It returns how many deliveries it claimed, so the caller knows whether more may be due.
	Dispatch(ctx context.Context) (int, error)
	// Sweep recovers deliveries whose lease expired without an outcome (the lost attempt is recorded with an
	// unknown result and the policy decides the next state) and fails scheduled work past its deadline.
	// It returns how many deliveries it touched.
	Sweep(ctx context.Context) (int, error)
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
// A failure before anything is sent releases the delivery back to the schedule; a failure after leaves
// the lease to expire for Sweep.
func (s *dispatchService) attempt(ctx context.Context, d *models.Delivery, token string) error {
	msg, endpoint, active, err := s.load(ctx, d)
	if err != nil {
		return errors.Join(err, s.release(ctx, d, token))
	}
	if endpoint.ArchivedAt != nil {
		reason := ReasonEndpointArchived
		return s.deps.Deliveries.RecordOutcome(ctx, models.DeliveryOutcome{DeliveryID: d.ID, LeaseToken: token, State: models.DeliveryCancelled, FailureReason: &reason})
	}

	started := time.Now()
	headers, err := s.sign(active, msg, started)
	if err != nil {
		return errors.Join(err, s.release(ctx, d, token))
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
	if err := s.record(ctx, a, token, decision); err != nil {
		return err
	}
	s.deps.Logger.InfoContext(ctx, "delivery attempt", "delivery_id", d.ID, "attempt", d.AttemptCount, "status", res.StatusCode, "error_class", res.ErrorClass, "state", decision.State)
	return nil
}

// load reads what an attempt needs; active is nil for an archived endpoint.
func (s *dispatchService) load(ctx context.Context, d *models.Delivery) (*models.Message, *models.Endpoint, []models.EndpointSecret, error) {
	msg, err := s.deps.Messages.GetMessageByID(ctx, d.MessageID)
	if err != nil {
		return nil, nil, nil, err
	}
	endpoint, err := s.deps.Endpoints.GetEndpointByID(ctx, d.EndpointID)
	if err != nil {
		return nil, nil, nil, err
	}
	if endpoint.ArchivedAt != nil {
		return msg, endpoint, nil, nil
	}
	active, err := s.deps.Secrets.ListActiveEndpointSecrets(ctx, endpoint.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(active) == 0 {
		return nil, nil, nil, fmt.Errorf("endpoint %d has no active signing secret", endpoint.ID)
	}
	return msg, endpoint, active, nil
}

// release hands a claimed delivery back before any bytes were sent; a lease already gone is not an error.
func (s *dispatchService) release(ctx context.Context, d *models.Delivery, token string) error {
	err := s.deps.Deliveries.ReleaseDelivery(ctx, d.ID, token)
	if err != nil && !errors.Is(err, apperr.ErrNotFound) {
		return fmt.Errorf("release delivery %d: %w", d.ID, err)
	}
	return nil
}

// record writes the attempt and the outcome in one transaction; a lost lease is logged and dropped.
func (s *dispatchService) record(ctx context.Context, a *models.Attempt, token string, decision Decision) error {
	err := s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.deps.Attempts.WithTx(tx).CreateAttempt(ctx, a); err != nil {
			return err
		}
		return s.deps.Deliveries.WithTx(tx).RecordOutcome(ctx, models.DeliveryOutcome{DeliveryID: a.DeliveryID, LeaseToken: token, State: decision.State, DueAt: decision.DueAt, FailureReason: decision.Reason})
	})
	if errors.Is(err, apperr.ErrNotFound) {
		s.deps.Logger.WarnContext(ctx, "lease lost before the outcome was recorded", "delivery_id", a.DeliveryID, "attempt", a.Number)
		return nil
	}
	return err
}

func (s *dispatchService) Sweep(ctx context.Context) (int, error) {
	token := uuid.NewString()
	expired, err := s.deps.Deliveries.ClaimExpiredLeases(ctx, s.deps.BatchSize, token, s.deps.LeaseTTL)
	if err != nil {
		return 0, apperr.NewInternal(err)
	}
	var errs []error
	for i := range expired {
		if err := s.recover(ctx, &expired[i], token); err != nil {
			errs = append(errs, fmt.Errorf("delivery %d: %w", expired[i].ID, err))
		}
	}
	overdue, err := s.deps.Deliveries.ExpireDeadlines(ctx, s.deps.BatchSize)
	if err != nil {
		errs = append(errs, fmt.Errorf("expire deadlines: %w", err))
	} else if overdue > 0 {
		s.deps.Logger.InfoContext(ctx, "deliveries past their deadline failed", "count", overdue)
	}
	return len(expired) + int(overdue), errors.Join(errs...)
}

// recover records the attempt the lost lease stood for with an unknown result and applies the policy.
func (s *dispatchService) recover(ctx context.Context, d *models.ExpiredDelivery, token string) error {
	endpoint, err := s.deps.Endpoints.GetEndpointByID(ctx, d.EndpointID)
	if err != nil {
		return err
	}
	now := time.Now()
	res := SendResult{ErrorClass: ErrorClassUnknown}
	decision := s.deps.Policy.Decide(res, d.AttemptCount, d.DeadlineAt, now)
	a := &models.Attempt{
		DeliveryID:       d.ID,
		Number:           d.AttemptCount,
		EndpointRevision: d.EndpointRevision,
		RequestURL:       endpoint.URL,
		StartedAt:        d.LeaseExpiredAt.Add(-s.deps.LeaseTTL), // the claim time, within the lease
		ErrorClass:       &res.ErrorClass,
	}
	if err := s.record(ctx, a, token, decision); err != nil {
		return err
	}
	s.deps.Logger.WarnContext(ctx, "expired lease recovered", "delivery_id", d.ID, "attempt", d.AttemptCount, "lease_expired_at", d.LeaseExpiredAt, "state", decision.State)
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
