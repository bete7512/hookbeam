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
	GetMessage(ctx context.Context, scope Scope, id int64) (*models.Message, []models.Delivery, error)
	ListAttempts(ctx context.Context, scope Scope, deliveryID int64) ([]models.Attempt, error)
}

// MessageServiceDeps are the dependencies; all are required.
type MessageServiceDeps struct {
	Tx                db.Transactor
	Applications      repos.ApplicationRepo
	Endpoints         repos.EndpointRepo
	Messages          repos.MessageRepo
	Deliveries        repos.DeliveryRepo
	Attempts          repos.AttemptRepo
	Outbox            repos.OutboxRepo
	Policy            Policy
	PayloadRetention  time.Duration // how long the payload stays readable and replayable
	IdempotencyWindow time.Duration // how long a key dedupes
	MaxPayloadBytes   int
	Logger            *slog.Logger
}

type messageService struct {
	deps MessageServiceDeps
}

// NewMessageService returns a MessageService or an error if a dependency is missing.
func NewMessageService(deps MessageServiceDeps) (MessageService, error) {
	if deps.Tx == nil || deps.Applications == nil || deps.Endpoints == nil || deps.Messages == nil || deps.Deliveries == nil || deps.Attempts == nil || deps.Outbox == nil ||
		deps.Policy.CycleMaxAge <= 0 || deps.PayloadRetention <= 0 || deps.IdempotencyWindow <= 0 || deps.MaxPayloadBytes <= 0 || deps.Logger == nil {
		return nil, errors.New("services: Tx, repos, Policy, PayloadRetention, IdempotencyWindow, MaxPayloadBytes and Logger are required")
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

func (s *messageService) GetMessage(ctx context.Context, scope Scope, id int64) (*models.Message, []models.Delivery, error) {
	if scope.WorkspaceID == "" {
		return nil, nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	m, err := s.deps.Messages.GetMessage(ctx, scope.WorkspaceID, id)
	if err != nil {
		return nil, nil, endpointError("message", err)
	}
	deliveries, err := s.deps.Deliveries.ListDeliveriesByMessage(ctx, m.ID)
	if err != nil {
		return nil, nil, apperr.NewInternal(err)
	}
	return m, deliveries, nil
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

func (s *messageService) validate(in AcceptInput) map[string]string {
	fields := map[string]string{}
	if in.ApplicationID <= 0 {
		fields["application_id"] = "is required"
	}
	if !validEventType(in.EventType) {
		fields["type"] = "is required: 1 to 100 characters without whitespace"
	}
	if in.IdempotencyKey == "" || utf8.RuneCountInString(in.IdempotencyKey) > maxIdempotencyKeyLength || strings.IndexFunc(in.IdempotencyKey, unicode.IsControl) >= 0 {
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
func newPublicID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return publicIDPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}
