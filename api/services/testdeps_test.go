package services_test

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// fakeTx runs fn without a database; repos are mocked so no tx is needed.
type fakeTx struct{}

func (fakeTx) WithTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, nil)
}
