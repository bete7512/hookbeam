package db

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bete7512/hookbeam/pkg/apperr"
)

const uniqueViolation = "23505"

// HandleError maps driver errors to apperr sentinels; other errors are returned unchanged.
func HandleError(err error) error {
	var pgErr *pgconn.PgError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("%w: %w", apperr.ErrNotFound, err)
	case errors.As(err, &pgErr) && pgErr.Code == uniqueViolation:
		return fmt.Errorf("%w: %w", apperr.ErrConflict, err)
	default:
		return err
	}
}
