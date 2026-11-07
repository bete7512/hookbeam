package db_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/db"
)

func TestHandleError(t *testing.T) {
	require.NoError(t, db.HandleError(nil))
	require.ErrorIs(t, db.HandleError(pgx.ErrNoRows), apperr.ErrNotFound)
	require.ErrorIs(t, db.HandleError(&pgconn.PgError{Code: "23505"}), apperr.ErrConflict)

	other := errors.New("connection refused")
	require.Equal(t, other, db.HandleError(other))
}
