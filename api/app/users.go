package app

import (
	"context"
	"errors"
	"fmt"

	goauthmodels "github.com/bete7512/goauth/pkg/models"
	"github.com/bete7512/goauth/pkg/types"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

// users answers a user's contact details from goauth's core storage.
type users struct {
	store types.CoreStorage
}

var _ services.Users = users{}

// Contact returns the email, its verification state and the name of a user, or ErrNotFound.
func (u users) Contact(ctx context.Context, userID string) (models.UserContact, error) {
	user, err := u.store.Users().FindByID(ctx, userID)
	if errors.Is(err, goauthmodels.ErrNotFound) || (err == nil && user == nil) {
		return models.UserContact{}, fmt.Errorf("%w: user %s", apperr.ErrNotFound, userID)
	}
	if err != nil {
		return models.UserContact{}, err
	}
	return models.UserContact{Email: user.Email, EmailVerified: user.EmailVerified, Name: user.Name}, nil
}
