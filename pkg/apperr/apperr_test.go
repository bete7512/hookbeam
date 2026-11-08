package apperr_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/apperr"
)

func TestConstructors(t *testing.T) {
	cause := errors.New("boom")
	tests := []struct {
		err      *apperr.AppError
		status   int
		code     string
		sentinel error
	}{
		{apperr.NewNotFound("user", cause), http.StatusNotFound, apperr.CodeNotFound, apperr.ErrNotFound},
		{apperr.NewConflict("taken", cause), http.StatusConflict, apperr.CodeConflict, apperr.ErrConflict},
		{apperr.NewValidation(map[string]string{"email": "required"}), http.StatusUnprocessableEntity, apperr.CodeValidation, apperr.ErrValidation},
		{apperr.NewBadRequest("bad", cause), http.StatusBadRequest, apperr.CodeBadRequest, apperr.ErrBadRequest},
		{apperr.NewUnauthorized("no", cause), http.StatusUnauthorized, apperr.CodeUnauthorized, apperr.ErrUnauthorized},
		{apperr.NewForbidden("no", cause), http.StatusForbidden, apperr.CodeForbidden, apperr.ErrForbidden},
		{apperr.NewUnavailable("down", cause), http.StatusServiceUnavailable, apperr.CodeUnavailable, apperr.ErrUnavailable},
		{apperr.NewGone("payload has expired", cause), http.StatusGone, apperr.CodeGone, apperr.ErrGone},
		{apperr.NewInternal(cause), http.StatusInternalServerError, apperr.CodeInternal, apperr.ErrInternal},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			assert.Equal(t, tt.status, tt.err.Code)
			assert.Equal(t, tt.code, tt.err.ApiCode)
			assert.ErrorIs(t, tt.err, tt.sentinel)
		})
	}
}

func TestChainThroughLayers(t *testing.T) {
	repoErr := fmt.Errorf("get user: %w", apperr.ErrNotFound)
	err := fmt.Errorf("handler: %w", apperr.NewNotFound("user", repoErr))

	var appErr *apperr.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusNotFound, appErr.Code)
	require.ErrorIs(t, err, apperr.ErrNotFound)
	assert.Equal(t, "[404] user not found: get user: resource not found", appErr.Error())
}

func TestNilCause(t *testing.T) {
	err := apperr.NewInternal(nil)
	require.ErrorIs(t, err, apperr.ErrInternal)
	assert.Equal(t, "[500] internal server error: internal server error", err.Error())
}
