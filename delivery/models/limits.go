package models

import "time"

// ApplicationEndpointCount is how many active endpoints an application has.
type ApplicationEndpointCount struct {
	ApplicationID int64  `db:"application_id"`
	Name          string `db:"name"`
	Endpoints     int32  `db:"endpoints"`
}

// WorkspaceLimits is the quota a workspace lives under and where it stands.
type WorkspaceLimits struct {
	EndpointsPerApplication int
	FanoutPerMessage        int
	BacklogPerWorkspace     int
	Backlog                 int // non-terminal deliveries right now
	Applications            []ApplicationEndpointCount
	Retention               Retention
}

// Retention is how long the data plane keeps payloads, idempotency keys and audit events.
type Retention struct {
	Payload           time.Duration
	IdempotencyWindow time.Duration
	Audit             time.Duration
}

// Valid reports whether every retention is positive.
func (r Retention) Valid() bool { return r.Payload > 0 && r.IdempotencyWindow > 0 && r.Audit > 0 }
