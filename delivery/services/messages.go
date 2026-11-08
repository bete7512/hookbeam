package services

//go:generate mockgen -source=messages.go -destination=service_mocks/mock_messages.go -package=service_mocks

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	apievents "github.com/bete7512/hookbeam/delivery/events"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/db"
)

const (
	maxIdempotencyKeyLength = 255
	publicIDPrefix          = "msg_"
)

// AcceptInput is a submitted event; Payload is kept byte for byte as the outbound body.
type AcceptInput struct {
	ApplicationID  int64
	EventType      string
	IdempotencyKey string
	SenderEventID  *string
	Payload        []byte
}

// AcceptResult is the accepted message with its fan-out size; Duplicate means an earlier submission was returned.
type AcceptResult struct {
	Message       *models.Message
	DeliveryCount int32
	Duplicate     bool
}

// MessageService accepts events and exposes their deliveries and attempts.
type MessageService interface {
	// Accept durably stores the message, one delivery per subscribed endpoint and a wake-up, in one transaction.
	Accept(ctx context.Context, scope Scope, in AcceptInput) (*AcceptResult, error)
	GetMessage(ctx context.Context, scope Scope, id int64) (*models.Message, *models.Application, []models.Delivery, error)
	ListAttempts(ctx context.Context, scope Scope, deliveryID int64) ([]models.Attempt, error)
	// CancelDelivery stops further attempts of a delivery in the workspace and returns its current row; a finished
	// delivery is returned unchanged, so repeating the call is safe.
	CancelDelivery(ctx context.Context, scope Scope, deliveryID int64) (*models.Delivery, error)
	// ReplayDelivery starts a new cycle for a failed or cancelled delivery under the current endpoint revision,
	// keeping the message's webhook-id. The same idempotency key returns the same new cycle.
	ReplayDelivery(ctx context.Context, scope Scope, deliveryID int64, idempotencyKey string) (*models.Delivery, error)
	// ListMessages pages the explorer: the application or the environment must belong to the workspace.
	ListMessages(ctx context.Context, scope Scope, opts repos.ListMessagesOpts) ([]models.MessageForList, int, error)
	// ExpirePayloads empties every payload past its retention, batch by batch, and returns how many.
	ExpirePayloads(ctx context.Context) (int64, error)
	// ExpireIdempotencyKeys clears every idempotency key past its window, batch by batch, and returns how many.
	ExpireIdempotencyKeys(ctx context.Context) (int64, error)
}

// MessageServiceDeps are the dependencies; all are required.
type MessageServiceDeps struct {
	Tx                db.Transactor
	Applications      repos.ApplicationRepo
	Environments      repos.EnvironmentRepo
	Endpoints         repos.EndpointRepo
	Messages          repos.MessageRepo
	Deliveries        repos.DeliveryRepo
	Attempts          repos.AttemptRepo
	Outbox            repos.OutboxRepo
	Audit             repos.AuditRepo
	Policy            Policy
	PayloadRetention  time.Duration // how long the payload stays readable and replayable
	IdempotencyWindow time.Duration // how long a key dedupes
	MaxPayloadBytes   int
	ExpiryBatchSize   int // rows per retention batch
	Logger            *slog.Logger
}

type messageService struct {
	deps MessageServiceDeps
}

// NewMessageService returns a MessageService or an error if a dependency is missing.
func NewMessageService(deps MessageServiceDeps) (MessageService, error) {
	if deps.Tx == nil || deps.Applications == nil || deps.Environments == nil || deps.Endpoints == nil || deps.Messages == nil || deps.Deliveries == nil || deps.Attempts == nil || deps.Outbox == nil || deps.Audit == nil ||
		deps.Policy.CycleMaxAge <= 0 || deps.PayloadRetention <= 0 || deps.IdempotencyWindow <= 0 || deps.MaxPayloadBytes <= 0 || deps.ExpiryBatchSize <= 0 || deps.Logger == nil {
		return nil, errors.New("services: Tx, repos, Policy, PayloadRetention, IdempotencyWindow, MaxPayloadBytes, ExpiryBatchSize and Logger are required")
	}
	return &messageService{deps: deps}, nil
}

func (s *messageService) Accept(ctx context.Context, scope Scope, in AcceptInput) (*AcceptResult, error) {
	if scope.WorkspaceID == "" {
		return nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	in.EventType, in.IdempotencyKey = strings.TrimSpace(in.EventType), strings.TrimSpace(in.IdempotencyKey)
	if fields := s.validate(in); len(fields) > 0 {
		return nil, apperr.NewValidation(fields)
	}
	app, err := s.deps.Applications.GetApplication(ctx, scope.WorkspaceID, in.ApplicationID)
	if err != nil {
		return nil, endpointError("application", err)
	}
	if app.ArchivedAt != nil {
		return nil, apperr.NewConflict("application is archived", nil)
	}
	sum := sha256.Sum256(in.Payload)

	if existing, err := s.deps.Messages.FindMessageByIdempotencyKey(ctx, in.ApplicationID, in.IdempotencyKey); err == nil {
		return s.duplicate(ctx, existing, in, sum[:])
	} else if !errors.Is(err, apperr.ErrNotFound) {
		return nil, apperr.NewInternal(err)
	}

	endpoints, err := s.deps.Endpoints.ListSubscribedEndpoints(ctx, in.ApplicationID, in.EventType)
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	if len(endpoints) == 0 {
		return nil, apperr.NewValidation(map[string]string{"type": "no endpoint of the application subscribes to this event type"})
	}

	publicID, err := newPublicID()
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	now := time.Now()
	keyExpiry := now.Add(s.deps.IdempotencyWindow)
	msg := &models.Message{
		PublicID:             publicID,
		WorkspaceID:          scope.WorkspaceID,
		ApplicationID:        in.ApplicationID,
		EventType:            in.EventType,
		SenderEventID:        in.SenderEventID,
		IdempotencyKey:       &in.IdempotencyKey,
		IdempotencyExpiresAt: &keyExpiry,
		Payload:              in.Payload,
		PayloadSHA256:        sum[:],
		PayloadExpiresAt:     now.Add(s.deps.PayloadRetention),
	}
	err = s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.deps.Messages.WithTx(tx).CreateMessage(ctx, msg); err != nil {
			return err
		}
		deliveries := s.deps.Deliveries.WithTx(tx)
		for i := range endpoints {
			due := now
			d := &models.Delivery{MessageID: msg.ID, EndpointID: endpoints[i].ID, EndpointRevision: endpoints[i].Revision, State: models.DeliveryQueued, DueAt: &due, DeadlineAt: now.Add(s.deps.Policy.CycleMaxAge)}
			if err := deliveries.CreateDelivery(ctx, d); err != nil {
				return err
			}
		}
		payload, err := json.Marshal(apievents.DeliveryDuePayload{MessageID: msg.ID})
		if err != nil {
			return err
		}
		return s.deps.Outbox.WithTx(tx).CreateOutboxEvent(ctx, &models.OutboxEvent{Type: apievents.DeliveryDue, Payload: payload})
	})
	if errors.Is(err, apperr.ErrConflict) {
		// A concurrent submission with the same key won the insert; answer with its result.
		existing, findErr := s.deps.Messages.FindMessageByIdempotencyKey(ctx, in.ApplicationID, in.IdempotencyKey)
		if findErr != nil {
			return nil, apperr.NewInternal(err)
		}
		return s.duplicate(ctx, existing, in, sum[:])
	}
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	return &AcceptResult{Message: msg, DeliveryCount: int32(len(endpoints))}, nil //nolint:gosec // bounded by the endpoint count
}

// duplicate answers a repeated submission: the same bytes and type return the first result, anything else conflicts.
func (s *messageService) duplicate(ctx context.Context, existing *models.Message, in AcceptInput, sum []byte) (*AcceptResult, error) {
	if !bytes.Equal(existing.PayloadSHA256, sum) || existing.EventType != in.EventType {
		return nil, apperr.NewConflict("idempotency key was already used with a different event", nil)
	}
	n, err := s.deps.Deliveries.CountDeliveriesByMessage(ctx, existing.ID)
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	return &AcceptResult{Message: existing, DeliveryCount: n, Duplicate: true}, nil
}

func (s *messageService) GetMessage(ctx context.Context, scope Scope, id int64) (*models.Message, *models.Application, []models.Delivery, error) {
	if scope.WorkspaceID == "" {
		return nil, nil, nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	m, err := s.deps.Messages.GetMessage(ctx, scope.WorkspaceID, id)
	if err != nil {
		return nil, nil, nil, endpointError("message", err)
	}
	app, err := s.deps.Applications.GetApplication(ctx, scope.WorkspaceID, m.ApplicationID)
	if err != nil {
		return nil, nil, nil, endpointError("application", err)
	}
	deliveries, err := s.deps.Deliveries.ListDeliveriesByMessage(ctx, m.ID)
	if err != nil {
		return nil, nil, nil, apperr.NewInternal(err)
	}
	return m, app, deliveries, nil
}

func (s *messageService) ListAttempts(ctx context.Context, scope Scope, deliveryID int64) ([]models.Attempt, error) {
	if scope.WorkspaceID == "" {
		return nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	d, err := s.deps.Deliveries.GetDelivery(ctx, scope.WorkspaceID, deliveryID)
	if err != nil {
		return nil, endpointError("delivery", err)
	}
	attempts, err := s.deps.Attempts.ListAttempts(ctx, d.ID)
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	return attempts, nil
}

func (s *messageService) CancelDelivery(ctx context.Context, scope Scope, deliveryID int64) (*models.Delivery, error) {
	if scope.WorkspaceID == "" {
		return nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	var d *models.Delivery
	err := s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		if d, err = s.deps.Deliveries.WithTx(tx).CancelDelivery(ctx, scope.WorkspaceID, deliveryID); err != nil {
			return err
		}
		if d.State != models.DeliveryCancelled {
			return nil // in flight: the attempt's outcome announces the end
		}
		reason := ReasonCancelled
		payload, err := json.Marshal(apievents.DeliveryStatePayload{
			WorkspaceID: scope.WorkspaceID, EndpointID: d.EndpointID, MessageID: d.MessageID, DeliveryID: d.ID,
			Attempt: d.AttemptCount, State: d.State, Reason: &reason, OccurredAt: time.Now(),
		})
		if err != nil {
			return err
		}
		if err := s.deps.Outbox.WithTx(tx).CreateOutboxEvent(ctx, &models.OutboxEvent{Type: apievents.DeliveryCancelled, Payload: payload}); err != nil {
			return err
		}
		return recordAudit(ctx, s.deps.Audit.WithTx(tx), scope, AuditDeliveryCancel, "delivery", d.ID, map[string]any{"message_id": d.MessageID, "endpoint_id": d.EndpointID})
	})
	if errors.Is(err, apperr.ErrNotFound) {
		// Already finished, or not ours: the read tells which.
		d, err = s.deps.Deliveries.GetDelivery(ctx, scope.WorkspaceID, deliveryID)
	}
	if err != nil {
		return nil, endpointError("delivery", err)
	}
	return d, nil
}

func (s *messageService) ReplayDelivery(ctx context.Context, scope Scope, deliveryID int64, idempotencyKey string) (*models.Delivery, error) {
	if scope.WorkspaceID == "" {
		return nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if !validIdempotencyKey(idempotencyKey) {
		return nil, apperr.NewValidation(map[string]string{"Idempotency-Key": "header is required: 1 to 255 printable characters"})
	}
	original, err := s.deps.Deliveries.GetDelivery(ctx, scope.WorkspaceID, deliveryID)
	if err != nil {
		return nil, endpointError("delivery", err)
	}
	if existing, err := s.deps.Deliveries.FindReplay(ctx, original.ID, idempotencyKey); err == nil {
		return existing, nil
	} else if !errors.Is(err, apperr.ErrNotFound) {
		return nil, apperr.NewInternal(err)
	}
	if original.State != models.DeliveryFailed && original.State != models.DeliveryCancelled {
		return nil, apperr.NewConflict("only a failed or cancelled delivery can be replayed", nil)
	}
	endpoint, err := s.deps.Endpoints.GetEndpointByID(ctx, original.EndpointID)
	if err != nil {
		return nil, endpointError("endpoint", err)
	}
	if endpoint.ArchivedAt != nil {
		return nil, apperr.NewConflict("endpoint is archived", nil)
	}
	msg, err := s.deps.Messages.GetMessageByID(ctx, original.MessageID)
	if err != nil {
		return nil, endpointError("message", err)
	}
	now := time.Now()
	if len(msg.Payload) == 0 || !msg.PayloadExpiresAt.After(now) {
		return nil, apperr.NewConflict("the payload has expired, so the delivery cannot be replayed", nil)
	}
	deadline := now.Add(s.deps.Policy.CycleMaxAge)
	if msg.PayloadExpiresAt.Before(deadline) {
		deadline = msg.PayloadExpiresAt
	}
	replay := &models.Delivery{
		MessageID: msg.ID, EndpointID: endpoint.ID, EndpointRevision: endpoint.Revision, State: models.DeliveryQueued,
		DueAt: &now, DeadlineAt: deadline, ReplayOfDeliveryID: &original.ID, ReplayIdempotencyKey: &idempotencyKey,
	}
	err = s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.deps.Deliveries.WithTx(tx).CreateDelivery(ctx, replay); err != nil {
			return err
		}
		payload, err := json.Marshal(apievents.DeliveryDuePayload{MessageID: msg.ID})
		if err != nil {
			return err
		}
		if err := s.deps.Outbox.WithTx(tx).CreateOutboxEvent(ctx, &models.OutboxEvent{Type: apievents.DeliveryDue, Payload: payload}); err != nil {
			return err
		}
		return recordAudit(ctx, s.deps.Audit.WithTx(tx), scope, AuditDeliveryReplay, "delivery", replay.ID, map[string]any{"replay_of_delivery_id": original.ID, "message_id": msg.ID, "endpoint_id": endpoint.ID, "endpoint_revision": endpoint.Revision})
	})
	if errors.Is(err, apperr.ErrConflict) {
		// Either a concurrent replay with the same key won, or another cycle is still active for this endpoint.
		if existing, findErr := s.deps.Deliveries.FindReplay(ctx, original.ID, idempotencyKey); findErr == nil {
			return existing, nil
		}
		return nil, apperr.NewConflict("an active delivery cycle already exists for this endpoint", err)
	}
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	return replay, nil
}

func (s *messageService) ListMessages(ctx context.Context, scope Scope, opts repos.ListMessagesOpts) ([]models.MessageForList, int, error) {
	if scope.WorkspaceID == "" {
		return nil, 0, apperr.NewUnauthorized("a workspace is required", nil)
	}
	fields := map[string]string{}
	if opts.Limit < 1 || opts.Limit > 100 {
		fields["limit"] = "must be between 1 and 100"
	}
	if opts.Offset < 0 {
		fields["offset"] = "must not be negative"
	}
	if opts.ApplicationID == 0 && opts.EnvironmentID == 0 {
		fields["application_id"] = "an application or an environment is required"
	}
	if opts.Outcome != "" && !slices.Contains(deliveryStatesList, opts.Outcome) {
		fields["outcome"] = "must be one of: " + strings.Join(deliveryStatesList, ", ")
	}
	if opts.AcceptedAfter != nil && opts.AcceptedBefore != nil && !opts.AcceptedBefore.After(*opts.AcceptedAfter) {
		fields["accepted_before"] = "must be after accepted_after"
	}
	maps.Copy(fields, repos.MessageSortColumns.Validate(opts.SortBy, opts.SortDir))
	if len(fields) > 0 {
		return nil, 0, apperr.NewValidation(fields)
	}
	if opts.ApplicationID != 0 {
		if _, err := s.deps.Applications.GetApplication(ctx, scope.WorkspaceID, opts.ApplicationID); err != nil {
			return nil, 0, endpointError("application", err)
		}
	}
	if opts.EnvironmentID != 0 {
		if _, err := s.deps.Environments.GetEnvironment(ctx, scope.WorkspaceID, opts.EnvironmentID); err != nil {
			return nil, 0, endpointError("environment", err)
		}
	}
	opts.WorkspaceID = scope.WorkspaceID
	opts.EventType, opts.SenderEventID = strings.TrimSpace(opts.EventType), strings.TrimSpace(opts.SenderEventID)
	items, err := s.deps.Messages.ListMessages(ctx, opts)
	if err != nil {
		return nil, 0, apperr.NewInternal(err)
	}
	total, err := s.deps.Messages.CountMessages(ctx, opts)
	if err != nil {
		return nil, 0, apperr.NewInternal(err)
	}
	return items, total, nil
}

// deliveryStatesList is every delivery state, for the outcome filter.
var deliveryStatesList = []string{models.DeliveryQueued, models.DeliveryInFlight, models.DeliveryRetryScheduled, models.DeliverySucceeded, models.DeliveryFailed, models.DeliveryCancelled}

func (s *messageService) ExpirePayloads(ctx context.Context) (int64, error) {
	return s.drain(ctx, s.deps.Messages.ExpirePayloads)
}

func (s *messageService) ExpireIdempotencyKeys(ctx context.Context) (int64, error) {
	return s.drain(ctx, s.deps.Messages.ExpireIdempotencyKeys)
}

// drain calls step batch by batch until a batch comes back short.
func (s *messageService) drain(ctx context.Context, step func(context.Context, int) (int64, error)) (int64, error) {
	var total int64
	for {
		n, err := step(ctx, s.deps.ExpiryBatchSize)
		total += n
		if err != nil {
			return total, apperr.NewInternal(err)
		}
		if n < int64(s.deps.ExpiryBatchSize) {
			return total, nil
		}
	}
}

func (s *messageService) validate(in AcceptInput) map[string]string {
	fields := map[string]string{}
	if in.ApplicationID <= 0 {
		fields["application_id"] = "is required"
	}
	if !validEventType(in.EventType) {
		fields["type"] = "is required: 1 to 100 characters without whitespace"
	}
	if !validIdempotencyKey(in.IdempotencyKey) {
		fields["Idempotency-Key"] = "header is required: 1 to 255 printable characters"
	}
	switch {
	case len(in.Payload) == 0:
		fields["body"] = "is required"
	case len(in.Payload) > s.deps.MaxPayloadBytes:
		fields["body"] = "exceeds the payload limit"
	case !json.Valid(in.Payload) || !utf8.Valid(in.Payload):
		fields["body"] = "must be valid UTF-8 JSON"
	}
	return fields
}

// newPublicID returns msg_ plus 22 url-safe characters from 16 random bytes.
// validIdempotencyKey accepts 1 to 255 printable characters.
func validIdempotencyKey(key string) bool {
	return key != "" && utf8.RuneCountInString(key) <= maxIdempotencyKeyLength && strings.IndexFunc(key, unicode.IsControl) < 0
}

func newPublicID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return publicIDPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}
