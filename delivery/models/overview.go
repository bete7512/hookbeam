package models

import "time"

// Overview is what an environment's overview tiles show: counts over a window, the backlog now, and the health
// of its endpoints. ComputedAt says when the numbers were read, so a lagging view is visible as such.
type Overview struct {
	Since              time.Time        `db:"-"`
	ComputedAt         time.Time        `db:"-"`
	Accepted           int64            `db:"accepted"`  // messages accepted in the window
	Succeeded          int64            `db:"succeeded"` // deliveries that succeeded in the window
	Exhausted          int64            `db:"exhausted"` // deliveries that failed with attempts_exhausted in the window
	Backlog            int64            `db:"backlog"`   // queued and retry-scheduled deliveries right now
	OldestPendingDueAt *time.Time       `db:"oldest_pending_due_at"`
	EndpointsHealthy   int32            `db:"endpoints_healthy"`
	EndpointsUnhealthy int32            `db:"endpoints_unhealthy"`
	EndpointsUnknown   int32            `db:"endpoints_unknown"`
	HealthObservedAt   *time.Time       `db:"health_observed_at"` // the newest health observation among the endpoints
	Series             []OverviewBucket `db:"-"`                  // one bucket per hour of the window, oldest first
}

// OverviewBucket is one hour of the window: messages accepted and deliveries that ended in it.
type OverviewBucket struct {
	Hour      time.Time `db:"hour"`
	Accepted  int64     `db:"accepted"`
	Succeeded int64     `db:"succeeded"`
	Failed    int64     `db:"failed"`
}
