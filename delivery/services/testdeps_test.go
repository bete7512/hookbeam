package services_test

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/delivery/services"
)

// testLimits are small quotas the service tests reason about.
var testLimits = services.Limits{EndpointsPerApplication: 20, FanoutPerMessage: 20, BacklogPerWorkspace: 100, BacklogRetryAfter: 30 * time.Second}

// fakeTx runs fn without a database; repos are mocked so no tx is needed.
type fakeTx struct{}

func (fakeTx) WithTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, nil)
}

// ptr returns a pointer to v.
func ptr[T any](v T) *T { return &v }
