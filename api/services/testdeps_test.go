package services_test

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// fakeTx runs the function with no transaction; the repos are mocks.
type fakeTx struct{}

func (fakeTx) WithTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, nil)
}
