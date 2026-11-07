package services

//go:generate mockgen -source=endpoints.go -destination=service_mocks/mock_endpoints.go -package=service_mocks

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/secrets"
)

const (
	maxURLLength         = 2048
	maxDescriptionLength = 500
	maxEventTypeLength   = 100
	maxEventTypes        = 100
	// SecretPrefix starts every signing secret, as in Standard Webhooks.
	SecretPrefix = "whsec_"
)

// CreateEndpointInput is what a caller provides for a new endpoint.
type CreateEndpointInput struct {
	ApplicationID int64
	URL           string
	Description   string
	// EventTypes the endpoint receives; empty subscribes it to every type.
	EventTypes []string
}

// EndpointService manages endpoints, their subscriptions and their signing secrets.
type EndpointService interface {
	// CreateEndpoint stores the endpoint, its subscriptions and its first secret in one transaction;
	// the returned secret is shown once.
	CreateEndpoint(ctx context.Context, scope Scope, in CreateEndpointInput) (endpoint *models.Endpoint, secret string, version int32, err error)
	GetEndpoint(ctx context.Context, scope Scope, id int64) (*models.Endpoint, error)
	// PauseEndpoint stops new attempts; pausing a paused endpoint is a no-op, an archived one is a conflict.
	PauseEndpoint(ctx context.Context, scope Scope, id int64) (*models.Endpoint, error)
}

// EndpointServiceDeps are the dependencies; all are required.
type EndpointServiceDeps struct {
	Tx            db.Transactor
	Applications  repos.ApplicationRepo
	Endpoints     repos.EndpointRepo
	Secrets       repos.EndpointSecretRepo
	Subscriptions repos.SubscriptionRepo
	Cipher        *secrets.Cipher
	KeyID         string // id of the key Cipher holds, stored next to each ciphertext
	Logger        *slog.Logger
}

type endpointService struct {
	deps EndpointServiceDeps
}

// NewEndpointService returns an EndpointService or an error if a dependency is missing.
func NewEndpointService(deps EndpointServiceDeps) (EndpointService, error) {
	if deps.Tx == nil || deps.Applications == nil || deps.Endpoints == nil || deps.Secrets == nil || deps.Subscriptions == nil || deps.Cipher == nil || deps.KeyID == "" || deps.Logger == nil {
		return nil, errors.New("services: Tx, Applications, Endpoints, Secrets, Subscriptions, Cipher, KeyID and Logger are required")
	}
	return &endpointService{deps: deps}, nil
}

func (s *endpointService) CreateEndpoint(ctx context.Context, scope Scope, in CreateEndpointInput) (*models.Endpoint, string, int32, error) {
	if scope.WorkspaceID == "" {
		return nil, "", 0, apperr.NewUnauthorized("a workspace is required", nil)
	}
	in.URL, in.Description = strings.TrimSpace(in.URL), strings.TrimSpace(in.Description)
	in.EventTypes = normalizeEventTypes(in.EventTypes)
	if fields := validateEndpoint(in); len(fields) > 0 {
		return nil, "", 0, apperr.NewValidation(fields)
	}
	if _, err := s.deps.Applications.GetApplication(ctx, scope.WorkspaceID, in.ApplicationID); err != nil {
		return nil, "", 0, endpointError("application", err)
	}

	raw, err := newSecret()
	if err != nil {
		return nil, "", 0, apperr.NewInternal(err)
	}
	ciphertext, err := s.deps.Cipher.Encrypt([]byte(raw))
	if err != nil {
		return nil, "", 0, apperr.NewInternal(err)
	}
	endpoint := &models.Endpoint{
		WorkspaceID:   scope.WorkspaceID,
		ApplicationID: in.ApplicationID,
		URL:           in.URL,
		Description:   in.Description,
		CreatedBy:     scope.ActorID,
		EventTypes:    in.EventTypes,
	}
	secret := &models.EndpointSecret{Ciphertext: ciphertext, KeyID: s.deps.KeyID, Fingerprint: fingerprint(raw)}
	err = s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.deps.Endpoints.WithTx(tx).CreateEndpoint(ctx, endpoint); err != nil {
			return err
		}
		secret.EndpointID = endpoint.ID
		if err := s.deps.Secrets.WithTx(tx).CreateEndpointSecret(ctx, secret); err != nil {
			return err
		}
		return s.deps.Subscriptions.WithTx(tx).CreateSubscriptions(ctx, endpoint.ID, in.EventTypes)
	})
	if err != nil {
		return nil, "", 0, apperr.NewInternal(err)
	}
	return endpoint, raw, secret.Version, nil
}

func (s *endpointService) GetEndpoint(ctx context.Context, scope Scope, id int64) (*models.Endpoint, error) {
	if scope.WorkspaceID == "" {
		return nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	e, err := s.deps.Endpoints.GetEndpoint(ctx, scope.WorkspaceID, id)
	if err != nil {
		return nil, endpointError("endpoint", err)
	}
	return s.withEventTypes(ctx, e)
}

func (s *endpointService) PauseEndpoint(ctx context.Context, scope Scope, id int64) (*models.Endpoint, error) {
	if scope.WorkspaceID == "" {
		return nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	e, err := s.deps.Endpoints.PauseEndpoint(ctx, scope.WorkspaceID, id)
	if errors.Is(err, apperr.ErrNotFound) {
		// Nothing to pause: either the endpoint is unknown, already paused or archived.
		if e, err = s.deps.Endpoints.GetEndpoint(ctx, scope.WorkspaceID, id); err != nil {
			return nil, endpointError("endpoint", err)
		}
		if e.ArchivedAt != nil {
			return nil, apperr.NewConflict("endpoint is archived", nil)
		}
	} else if err != nil {
		return nil, apperr.NewInternal(err)
	}
	return s.withEventTypes(ctx, e)
}

func (s *endpointService) withEventTypes(ctx context.Context, e *models.Endpoint) (*models.Endpoint, error) {
	types, err := s.deps.Subscriptions.ListEventTypes(ctx, e.ID)
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	e.EventTypes = types
	return e, nil
}

func validateEndpoint(in CreateEndpointInput) map[string]string {
	fields := map[string]string{}
	if in.ApplicationID <= 0 {
		fields["application_id"] = "is required"
	}
	if !validEndpointURL(in.URL) {
		fields["url"] = "must be an absolute http(s) URL without credentials, at most 2048 characters"
	}
	if utf8.RuneCountInString(in.Description) > maxDescriptionLength {
		fields["description"] = "must be at most 500 characters"
	}
	if len(in.EventTypes) > maxEventTypes {
		fields["event_types"] = "at most 100 event types"
	}
	for _, t := range in.EventTypes {
		if !validEventType(t) {
			fields["event_types"] = "each event type is 1 to 100 characters without whitespace; '*' is not allowed, leave the list empty for every type"
			break
		}
	}
	return fields
}

func validEndpointURL(raw string) bool {
	if raw == "" || len(raw) > maxURLLength {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil && u.Fragment == ""
}

func validEventType(t string) bool {
	if t == "" || t == "*" || utf8.RuneCountInString(t) > maxEventTypeLength {
		return false
	}
	return strings.IndexFunc(t, unicode.IsSpace) < 0
}

// normalizeEventTypes trims, drops empties and duplicates, and sorts.
func normalizeEventTypes(types []string) []string {
	out := make([]string, 0, len(types))
	for _, t := range types {
		if t = strings.TrimSpace(t); t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	slices.Sort(out)
	return out
}

// newSecret returns whsec_ + base64 of 32 random bytes.
func newSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return SecretPrefix + base64.StdEncoding.EncodeToString(b), nil
}

// fingerprint is the first 8 bytes of the secret's SHA-256, for display.
func fingerprint(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:8])
}

func endpointError(resource string, err error) error {
	if errors.Is(err, apperr.ErrNotFound) {
		return apperr.NewNotFound(resource, err)
	}
	return apperr.NewInternal(err)
}
