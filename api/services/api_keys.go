package services

//go:generate mockgen -source=api_keys.go -destination=service_mocks/mock_api_keys.go -package=service_mocks

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bete7512/goauth/pkg/types"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

// Actor is the caller of a workspace operation: a user session with an active workspace.
type Actor struct {
	UserID      string
	WorkspaceID string
}

// Memberships answers which role a user holds in a workspace; "" when not a member.
type Memberships interface {
	Role(ctx context.Context, workspaceID, userID string) (string, error)
}

// APIKeyService issues, lists, revokes and authenticates sender API keys.
type APIKeyService interface {
	// CreateAPIKey stores the key and returns the raw secret, shown once.
	CreateAPIKey(ctx context.Context, actor Actor, key *models.APIKey) (string, error)
	ListAPIKeys(ctx context.Context, actor Actor, opts repos.ListAPIKeysOpts) ([]models.APIKeyForList, int, error)
	RevokeAPIKey(ctx context.Context, actor Actor, id int64) error
	// Authenticate resolves a raw key to a workspace principal with method api_key.
	Authenticate(ctx context.Context, raw string) (types.Principal, error)
}

// APIKeyServiceDeps are the APIKeyService dependencies; all are required.
type APIKeyServiceDeps struct {
	Keys        repos.APIKeyRepo
	Memberships Memberships
	Logger      *slog.Logger
}

type apiKeyService struct {
	deps APIKeyServiceDeps
}

// NewAPIKeyService returns an APIKeyService or an error if a dependency is missing.
func NewAPIKeyService(deps APIKeyServiceDeps) (APIKeyService, error) {
	if deps.Keys == nil || deps.Memberships == nil || deps.Logger == nil {
		return nil, errors.New("services: Keys, Memberships and Logger are required")
	}
	return &apiKeyService{deps: deps}, nil
}

const (
	apiKeySecretBytes = 32
	apiKeyPrefixLen   = 8
	apiKeyNameMax     = 100
)

func (s *apiKeyService) CreateAPIKey(ctx context.Context, actor Actor, key *models.APIKey) (string, error) {
	if err := s.requireAdmin(ctx, actor); err != nil {
		return "", err
	}
	key.Name = strings.TrimSpace(key.Name)
	if len(key.Scopes) == 0 {
		key.Scopes = []string{models.ScopeEventsWrite}
	}
	fields := map[string]string{}
	switch n := utf8.RuneCountInString(key.Name); {
	case n == 0:
		fields["name"] = "is required"
	case n > apiKeyNameMax:
		fields["name"] = "must be at most 100 characters"
	}
	for _, scope := range key.Scopes {
		if !slices.Contains(models.APIKeyScopes, scope) {
			fields["scopes"] = "unknown scope " + scope
		}
	}
	if key.ExpiresAt != nil && !key.ExpiresAt.After(time.Now()) {
		fields["expiresAt"] = "must be in the future"
	}
	if len(fields) > 0 {
		return "", apperr.NewValidation(fields)
	}

	raw, err := newRawAPIKey()
	if err != nil {
		return "", apperr.NewInternal(err)
	}
	key.WorkspaceID = actor.WorkspaceID
	key.CreatedBy = actor.UserID
	key.Prefix = raw[:len(models.APIKeyPrefix)+apiKeyPrefixLen]
	key.KeyHash = hashAPIKey(raw)
	key.LastUsedAt, key.RevokedAt = nil, nil
	if err := s.deps.Keys.CreateAPIKey(ctx, key); err != nil {
		return "", apiKeyError(err)
	}
	return raw, nil
}

func (s *apiKeyService) ListAPIKeys(ctx context.Context, actor Actor, opts repos.ListAPIKeysOpts) ([]models.APIKeyForList, int, error) {
	if actor.WorkspaceID == "" {
		return nil, 0, apperr.NewForbidden("select a workspace", nil)
	}
	fields := map[string]string{}
	if opts.Limit < 1 || opts.Limit > 100 {
		fields["limit"] = "must be between 1 and 100"
	}
	if opts.Offset < 0 {
		fields["offset"] = "must not be negative"
	}
	maps.Copy(fields, repos.APIKeySortColumns.Validate(opts.SortBy, opts.SortDir))
	if len(fields) > 0 {
		return nil, 0, apperr.NewValidation(fields)
	}
	opts.WorkspaceID = actor.WorkspaceID
	opts.Search = strings.TrimSpace(opts.Search)
	keys, err := s.deps.Keys.ListAPIKeys(ctx, opts)
	if err != nil {
		return nil, 0, apiKeyError(err)
	}
	total, err := s.deps.Keys.CountAPIKeys(ctx, opts)
	if err != nil {
		return nil, 0, apiKeyError(err)
	}
	return keys, total, nil
}

func (s *apiKeyService) RevokeAPIKey(ctx context.Context, actor Actor, id int64) error {
	if err := s.requireAdmin(ctx, actor); err != nil {
		return err
	}
	if err := s.deps.Keys.RevokeAPIKey(ctx, actor.WorkspaceID, id); err != nil {
		return apiKeyError(err)
	}
	return nil
}

func (s *apiKeyService) Authenticate(ctx context.Context, raw string) (types.Principal, error) {
	if !strings.HasPrefix(raw, models.APIKeyPrefix) {
		return types.Principal{}, apperr.NewUnauthorized("invalid api key", nil)
	}
	key, err := s.deps.Keys.FindActiveAPIKeyByHash(ctx, hashAPIKey(raw))
	if errors.Is(err, apperr.ErrNotFound) {
		return types.Principal{}, apperr.NewUnauthorized("invalid api key", err)
	}
	if err != nil {
		return types.Principal{}, apperr.NewInternal(err)
	}
	// Recording the use is best effort: a failed touch must not reject a valid key.
	_ = s.deps.Keys.TouchAPIKey(ctx, key.ID)
	now := time.Now()
	p := types.Principal{OrganizationID: key.WorkspaceID, Methods: []string{"api_key"}, AuthenticatedAt: now}
	if key.ExpiresAt != nil {
		p.ExpiresAt = *key.ExpiresAt
	}
	return p, nil
}

// requireAdmin admits an owner or admin of the actor's workspace.
func (s *apiKeyService) requireAdmin(ctx context.Context, actor Actor) error {
	if actor.UserID == "" {
		return apperr.NewForbidden("a user session is required", nil)
	}
	if actor.WorkspaceID == "" {
		return apperr.NewForbidden("select a workspace", nil)
	}
	role, err := s.deps.Memberships.Role(ctx, actor.WorkspaceID, actor.UserID)
	if err != nil {
		return apperr.NewInternal(err)
	}
	if !types.HasMinimumRole(role, types.OrgRoleAdmin) {
		return apperr.NewForbidden("workspace admin required", nil)
	}
	return nil
}

func newRawAPIKey() (string, error) {
	secret := make([]byte, apiKeySecretBytes)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	return models.APIKeyPrefix + base64.RawURLEncoding.EncodeToString(secret), nil
}

func hashAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func apiKeyError(err error) error {
	if errors.Is(err, apperr.ErrNotFound) {
		return apperr.NewNotFound("api key", err)
	}
	return apperr.NewInternal(err)
}
