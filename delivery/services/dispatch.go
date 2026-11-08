package services

//go:generate mockgen -source=dispatch.go -destination=service_mocks/mock_dispatch.go -package=service_mocks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apievents "github.com/bete7512/hookbeam/delivery/events"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/secrets"
	"github.com/bete7512/hookbeam/pkg/telemetry"
	"github.com/bete7512/hookbeam/pkg/webhooks"
)

const (
	userAgent = "Hookbeam/1"
	// ErrorClassUnknown marks an attempt whose result was never recorded: the worker lost its lease mid-attempt.
	ErrorClassUnknown = "unknown"
)

// DispatchService claims due deliveries and attempts them.
type DispatchService interface {
	// Dispatch claims up to a batch of due deliveries, bounded by the free attempt slots, and hands each to a
	// goroutine that attempts it and records the outcome; it returns at once with how many it claimed, so the
	// caller knows whether more may be due. A stalling destination therefore never delays the next claim.
	Dispatch(ctx context.Context) (int, error)
	// Drain waits for every attempt in progress to record its outcome; the worker calls it before exiting.
	Drain()
	// Sweep recovers deliveries whose lease expired without an outcome (the lost attempt is recorded with an
	// unknown result and the policy decides the next state) and fails scheduled work past its deadline.
	// It returns how many deliveries it touched.
	Sweep(ctx context.Context) (int, error)
	// Backlog reads the queue's shape for the gauges: the age of the oldest due delivery and the attempts in flight.
	Backlog(ctx context.Context) (BacklogStats, error)
}

// DispatchServiceDeps are the dependencies; all are required.
type DispatchServiceDeps struct {
	Tx         db.Transactor
	Deliveries repos.DeliveryRepo
	Attempts   repos.AttemptRepo
	Messages   repos.MessageRepo
	Endpoints  repos.EndpointRepo
	Secrets    repos.EndpointSecretRepo
	Outbox     repos.OutboxRepo
	Health     HealthService
	Cipher     *secrets.Cipher
	Sender     Sender
	Policy     Policy
	LeaseTTL   time.Duration
	BatchSize  int
	// EndpointConcurrency caps a destination's in-flight attempts across replicas.
	EndpointConcurrency int
	// Concurrency is how many claimed deliveries this worker attempts at the same time.
	Concurrency int
	Metrics     *telemetry.Metrics // optional
	Logger      *slog.Logger
}

type dispatchService struct {
	deps     DispatchServiceDeps
	inFlight atomic.Int64   // attempts in progress in this process
	wg       sync.WaitGroup // the goroutines running them
}

// NewDispatchService returns a DispatchService or an error if a dependency is missing.
func NewDispatchService(deps DispatchServiceDeps) (DispatchService, error) {
	if deps.Tx == nil || deps.Deliveries == nil || deps.Attempts == nil || deps.Messages == nil || deps.Endpoints == nil || deps.Secrets == nil || deps.Outbox == nil || deps.Health == nil ||
		deps.Cipher == nil || deps.Sender == nil || deps.Policy.MaxAttempts <= 0 || deps.LeaseTTL <= 0 || deps.BatchSize <= 0 || deps.EndpointConcurrency <= 0 || deps.Concurrency <= 0 || deps.Logger == nil {
		return nil, errors.New("services: Tx, repos, Health, Cipher, Sender, Policy, positive LeaseTTL, BatchSize, EndpointConcurrency and Concurrency, Logger are required")
	}
	return &dispatchService{deps: deps}, nil
}

func (s *dispatchService) Dispatch(ctx context.Context) (int, error) {
	free := int64(s.deps.Concurrency) - s.inFlight.Load()
	if free <= 0 {
		return 0, nil
	}
	limit := min(int(free), s.deps.BatchSize)
	token := uuid.NewString()
	var claimed []models.Delivery
	err := s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		deliveries := s.deps.Deliveries.WithTx(tx)
		if err := deliveries.LockClaims(ctx); err != nil {
			return err
		}
		var err error
		claimed, err = deliveries.ClaimDueDeliveries(ctx, limit, s.deps.EndpointConcurrency, token, s.deps.LeaseTTL)
		if err == nil {
			s.deps.Metrics.Claimed(ctx, len(claimed))
		}
		return err
	})
	if err != nil {
		return 0, apperr.NewInternal(err)
	}
	for i := range claimed {
		s.inFlight.Add(1)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer s.inFlight.Add(-1)
			// The attempt outlives the caller's context (a shutdown must still record what was sent) but
			// never the lease it holds.
			actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.deps.LeaseTTL)
			defer cancel()
			if err := s.attempt(actx, &claimed[i], token); err != nil {
				s.deps.Logger.WarnContext(actx, "delivery attempt failed", "delivery_id", claimed[i].ID, "error", err)
			}
		}()
	}
	return len(claimed), nil
}

func (s *dispatchService) Drain() { s.wg.Wait() }

// attempt sends one claimed delivery and records the attempt and the outcome in one transaction.
// A failure before anything is sent releases the delivery back to the schedule; a failure after leaves
// the lease to expire for Sweep.
func (s *dispatchService) attempt(ctx context.Context, d *models.Delivery, token string) error {
	msg, endpoint, active, err := s.load(ctx, d)
	if err != nil {
		return errors.Join(err, s.release(ctx, d, token))
	}
	// The attempt links to the trace that accepted the message: ids, numbers and outcome only, never the body,
	// the headers or the signing secret.
	ctx, span := telemetry.StartLinked(ctx, "delivery.attempt", msg.TraceParent)
	defer span.End()
	span.SetAttributes(telemetry.Int64("hookbeam.delivery.id", d.ID), telemetry.Int64("hookbeam.message.id", msg.ID), telemetry.Int64("hookbeam.endpoint.id", endpoint.ID), telemetry.Int64("hookbeam.attempt", int64(d.AttemptCount)), telemetry.Int64("hookbeam.endpoint.revision", int64(endpoint.Revision)))
	if endpoint.ArchivedAt != nil {
		reason := ReasonEndpointArchived
		return s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			state, err := s.deps.Deliveries.WithTx(tx).RecordOutcome(ctx, models.DeliveryOutcome{DeliveryID: d.ID, LeaseToken: token, State: models.DeliveryCancelled, FailureReason: &reason})
			if err != nil {
				return err
			}
			return s.announce(ctx, s.deps.Outbox.WithTx(tx), apievents.DeliveryStatePayload{
				WorkspaceID: msg.WorkspaceID, ApplicationID: msg.ApplicationID, EndpointID: endpoint.ID, MessageID: msg.ID, DeliveryID: d.ID,
				Attempt: d.AttemptCount, State: state, Reason: &reason, OccurredAt: time.Now(),
			})
		})
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
	if err := s.record(ctx, msg, endpoint.ID, a, token, decision); err != nil {
		span.RecordError(err)
		return err
	}
	span.SetAttributes(telemetry.String("hookbeam.outcome", decision.State), telemetry.Int64("http.response.status_code", int64(res.StatusCode)), telemetry.String("hookbeam.error_class", res.ErrorClass))
	s.deps.Metrics.Attempt(ctx, decision.State, res.StatusCode, res.ErrorClass)
	switch {
	case decision.State == models.DeliveryRetryScheduled:
		s.deps.Metrics.Retry(ctx)
	case decision.State == models.DeliveryFailed && decision.Reason != nil:
		s.deps.Metrics.Exhaustion(ctx, *decision.Reason)
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

// record writes the attempt, the outcome and the state event in one transaction; a lost lease is logged and dropped.
// The event is built from values captured before the insert, because CreateAttempt replaces the struct with the row.
func (s *dispatchService) record(ctx context.Context, msg *models.Message, endpointID int64, a *models.Attempt, token string, decision Decision) error {
	event := apievents.DeliveryStatePayload{
		WorkspaceID: msg.WorkspaceID, ApplicationID: msg.ApplicationID, EndpointID: endpointID, MessageID: msg.ID, DeliveryID: a.DeliveryID,
		Attempt: a.Number, Reason: decision.Reason, StatusCode: a.StatusCode, ErrorClass: a.ErrorClass,
	}
	err := s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.deps.Attempts.WithTx(tx).CreateAttempt(ctx, a); err != nil {
			return err
		}
		state, err := s.deps.Deliveries.WithTx(tx).RecordOutcome(ctx, models.DeliveryOutcome{DeliveryID: event.DeliveryID, LeaseToken: token, State: decision.State, DueAt: decision.DueAt, FailureReason: decision.Reason})
		if err != nil {
			return err
		}
		event.State, event.OccurredAt = state, time.Now()
		if obs, ok := healthObservation(msg, endpointID, a, state, event.OccurredAt); ok {
			if err := s.deps.Health.Observe(ctx, tx, obs); err != nil {
				return err
			}
		}
		return s.announce(ctx, s.deps.Outbox.WithTx(tx), event)
	})
	if errors.Is(err, apperr.ErrNotFound) {
		s.deps.Logger.WarnContext(ctx, "lease lost before the outcome was recorded", "delivery_id", a.DeliveryID, "attempt", a.Number)
		return nil
	}
	return err
}

// healthObservation turns a recorded outcome into a health verdict: only an attempt that reached the network
// counts, so a lost attempt (unknown result) and a cancellation say nothing about the receiver.
func healthObservation(msg *models.Message, endpointID int64, a *models.Attempt, state string, at time.Time) (models.HealthObservation, bool) {
	if a.ErrorClass != nil && *a.ErrorClass == ErrorClassUnknown {
		return models.HealthObservation{}, false
	}
	if state != models.DeliverySucceeded && state != models.DeliveryFailed && state != models.DeliveryRetryScheduled {
		return models.HealthObservation{}, false
	}
	return models.HealthObservation{
		EndpointID: endpointID, WorkspaceID: msg.WorkspaceID, ApplicationID: msg.ApplicationID, Revision: a.EndpointRevision,
		Failed: state != models.DeliverySucceeded, ObservedAt: at,
	}, true
}

// announce writes the state event for the control plane; a state nothing announces writes nothing.
func (s *dispatchService) announce(ctx context.Context, outbox repos.OutboxRepo, p apievents.DeliveryStatePayload) error {
	eventType := apievents.DeliveryStateEvent(p.State)
	if eventType == "" {
		return nil
	}
	if p.State == models.DeliveryCancelled {
		reason := ReasonCancelled
		p.Reason = &reason
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return outbox.CreateOutboxEvent(ctx, &models.OutboxEvent{Type: eventType, Payload: payload})
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
	msg, err := s.deps.Messages.GetMessageByID(ctx, d.MessageID)
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
	if err := s.record(ctx, msg, endpoint.ID, a, token, decision); err != nil {
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

// BacklogStats is what the dispatch gauges observe.
type BacklogStats struct {
	OldestDueAge time.Duration
	InFlight     int
}

func (s *dispatchService) Backlog(ctx context.Context) (BacklogStats, error) {
	oldest, err := s.deps.Deliveries.OldestDueAt(ctx)
	if err != nil {
		return BacklogStats{}, apperr.NewInternal(err)
	}
	inFlight, err := s.deps.Deliveries.CountInFlight(ctx)
	if err != nil {
		return BacklogStats{}, apperr.NewInternal(err)
	}
	var age time.Duration
	if oldest != nil {
		age = max(0, time.Since(*oldest))
	}
	return BacklogStats{OldestDueAge: age, InFlight: inFlight}, nil
}
