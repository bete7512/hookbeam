// Package services holds the api service's use-cases.
package services

//go:generate mockgen -source=users.go -destination=service_mocks/mock_users.go -package=service_mocks

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	apievents "github.com/bete7512/hookbeam/api/events"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/auth"
	"github.com/bete7512/hookbeam/pkg/db"
)

// UserService holds the user use-cases.
type UserService interface {
	Register(ctx context.Context, user *models.User, password string) error
	GetUser(ctx context.Context, id int64) (*models.User, error)
	GetUserFor(ctx context.Context, actorID, id int64) (*models.User, error)
	ListUsers(ctx context.Context, actorID int64, opts repos.ListUsersOpts) ([]models.UserForList, int, error)
	UpdateUser(ctx context.Context, actorID int64, user *models.User) error
	ChangePassword(ctx context.Context, actorID, id int64, currentPassword, newPassword string) error
	DeleteUser(ctx context.Context, actorID, id int64) error
}

// UserServiceDeps are the UserService dependencies; all are required.
type UserServiceDeps struct {
	Users    repos.UserRepo
	Sessions repos.SessionRepo
	Hasher   auth.PasswordHasher
	Tx       db.Transactor
	Outbox   repos.OutboxRepo
	Logger   *slog.Logger
}

type userService struct {
	deps UserServiceDeps
}

// NewUserService returns a UserService or an error if a dependency is missing.
func NewUserService(deps UserServiceDeps) (UserService, error) {
	if deps.Users == nil || deps.Sessions == nil || deps.Hasher == nil || deps.Tx == nil || deps.Outbox == nil || deps.Logger == nil {
		return nil, errors.New("services: Users, Sessions, Hasher, Tx, Outbox and Logger are required")
	}
	return &userService{deps: deps}, nil
}

func (s *userService) Register(ctx context.Context, user *models.User, password string) error {
	user.Email = normalizeEmail(user.Email)
	user.Name = strings.TrimSpace(user.Name)

	fields := map[string]string{}
	if msg := validateEmail(user.Email); msg != "" {
		fields["email"] = msg
	}
	if msg := validateName(user.Name); msg != "" {
		fields["name"] = msg
	}
	if msg := validatePassword(password); msg != "" {
		fields["password"] = msg
	}
	if len(fields) > 0 {
		return apperr.NewValidation(fields)
	}

	hash, err := s.deps.Hasher.Hash(password)
	if err != nil {
		return apperr.NewInternal(err)
	}
	user.PasswordHash = &hash
	user.EmailVerifiedAt = nil

	err = s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.deps.Users.WithTx(tx).CreateUser(ctx, user); err != nil {
			return err
		}
		payload, err := json.Marshal(apievents.UserRegisteredPayload{UserID: user.ID})
		if err != nil {
			return err
		}
		return s.deps.Outbox.WithTx(tx).CreateOutboxEvent(ctx, &models.OutboxEvent{Type: apievents.UserRegistered, Payload: payload})
	})
	if errors.Is(err, apperr.ErrConflict) {
		return apperr.NewConflict("email already registered", err)
	}
	if err != nil {
		return apperr.NewInternal(err)
	}
	return nil
}

func (s *userService) GetUser(ctx context.Context, id int64) (*models.User, error) {
	user, err := s.deps.Users.GetUser(ctx, id)
	if err != nil {
		return nil, userError(err)
	}
	return user, nil
}

func (s *userService) GetUserFor(ctx context.Context, actorID, id int64) (*models.User, error) {
	if err := requireSelf(actorID, id); err != nil {
		return nil, err
	}
	return s.GetUser(ctx, id)
}

// ListUsers returns only the caller's user and the matching total.
func (s *userService) ListUsers(ctx context.Context, actorID int64, opts repos.ListUsersOpts) ([]models.UserForList, int, error) {
	if err := requireSelf(actorID, actorID); err != nil {
		return nil, 0, err
	}
	opts.OwnerID = actorID
	fields := map[string]string{}
	if opts.Limit < 1 || opts.Limit > 100 {
		fields["limit"] = "must be between 1 and 100"
	}
	if opts.Offset < 0 {
		fields["offset"] = "must not be negative"
	}
	maps.Copy(fields, repos.UserSortColumns.Validate(opts.SortBy, opts.SortDir))
	if len(fields) > 0 {
		return nil, 0, apperr.NewValidation(fields)
	}

	users, err := s.deps.Users.ListUsers(ctx, opts)
	if err != nil {
		return nil, 0, apperr.NewInternal(err)
	}
	total, err := s.deps.Users.CountUsers(ctx, opts)
	if err != nil {
		return nil, 0, apperr.NewInternal(err)
	}
	return users, total, nil
}

func (s *userService) UpdateUser(ctx context.Context, actorID int64, user *models.User) error {
	if err := requireSelf(actorID, user.ID); err != nil {
		return err
	}
	user.Name = strings.TrimSpace(user.Name)
	if msg := validateName(user.Name); msg != "" {
		return apperr.NewValidation(map[string]string{"name": msg})
	}
	if err := s.deps.Users.UpdateUser(ctx, user); err != nil {
		return userError(err)
	}
	return nil
}

func (s *userService) ChangePassword(ctx context.Context, actorID, id int64, currentPassword, newPassword string) error {
	if err := requireSelf(actorID, id); err != nil {
		return err
	}
	if msg := validatePassword(newPassword); msg != "" {
		return apperr.NewValidation(map[string]string{"newPassword": msg})
	}

	user, err := s.deps.Users.GetUser(ctx, id)
	if err != nil {
		return userError(err)
	}
	if !user.HasPassword() {
		return apperr.NewValidation(map[string]string{"currentPassword": "account has no password"})
	}
	ok, err := s.deps.Hasher.Verify(currentPassword, *user.PasswordHash)
	if err != nil {
		return apperr.NewInternal(err)
	}
	if !ok {
		return apperr.NewValidation(map[string]string{"currentPassword": "is incorrect"})
	}

	hash, err := s.deps.Hasher.Hash(newPassword)
	if err != nil {
		return apperr.NewInternal(err)
	}
	err = s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		users := s.deps.Users.WithTx(tx)
		current, err := users.GetUserForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if current.TokenVersion != user.TokenVersion || current.PasswordHash == nil || *current.PasswordHash != *user.PasswordHash {
			return apperr.ErrConflict
		}
		if err := users.UpdateUserPassword(ctx, id, hash); err != nil {
			return err
		}
		return s.deps.Sessions.WithTx(tx).RevokeUserSessions(ctx, id)
	})
	if errors.Is(err, apperr.ErrConflict) {
		return apperr.NewConflict("credentials changed; try again", err)
	}
	if err != nil {
		return userError(err)
	}
	return nil
}

func (s *userService) DeleteUser(ctx context.Context, actorID, id int64) error {
	if err := requireSelf(actorID, id); err != nil {
		return err
	}
	if err := s.deps.Users.DeleteUser(ctx, id); err != nil {
		return userError(err)
	}
	return nil
}

func requireSelf(actorID, id int64) error {
	if actorID <= 0 {
		return apperr.NewUnauthorized("authentication required", nil)
	}
	if actorID != id {
		return apperr.NewForbidden("access to another user is not permitted", nil)
	}
	return nil
}

// userError maps a repo error for a single user to an AppError.
func userError(err error) error {
	if errors.Is(err, apperr.ErrNotFound) {
		return apperr.NewNotFound("user", err)
	}
	return apperr.NewInternal(err)
}

func validateEmail(email string) string {
	if email == "" {
		return "is required"
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "must be a valid email address"
	}
	return ""
}

func validateName(name string) string {
	if utf8.RuneCountInString(name) > 120 {
		return "must be at most 120 characters"
	}
	return ""
}

func validatePassword(password string) string {
	switch n := utf8.RuneCountInString(password); {
	case n < 12:
		return "must be at least 12 characters"
	case n > 256:
		return "must be at most 256 characters"
	}
	return ""
}
