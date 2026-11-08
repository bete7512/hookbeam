package models

import "time"

// Health states an endpoint read reports.
const (
	HealthUnknown   = "unknown" // no attempt observed yet
	HealthHealthy   = "healthy"
	HealthUnhealthy = "unhealthy" // an episode of consecutive failures is open
)

// EndpointHealth is the failure streak of one endpoint, kept in the outcome transaction.
type EndpointHealth struct {
	EndpointID          int64     `db:"endpoint_id"`
	WorkspaceID         string    `db:"workspace_id"`
	ApplicationID       int64     `db:"application_id"`
	Revision            int32     `db:"revision"` // highest endpoint revision observed; older observations are ignored
	ConsecutiveFailures int32     `db:"consecutive_failures"`
	LastState           string    `db:"last_state"` // succeeded or failed
	ObservedAt          time.Time `db:"observed_at"`
	OpenEpisodeID       *int64    `db:"open_episode_id"` // the unresolved episode, if any
}

// HealthObservation is one attempt's verdict on an endpoint.
type HealthObservation struct {
	EndpointID    int64
	WorkspaceID   string
	ApplicationID int64
	Revision      int32
	Failed        bool
	ObservedAt    time.Time
}

// HealthEpisode is a stretch of consecutive failures; ResolvedAt is nil while it is open.
type HealthEpisode struct {
	ID                  int64      `db:"id"`
	EndpointID          int64      `db:"endpoint_id"`
	WorkspaceID         string     `db:"workspace_id"`
	Revision            int32      `db:"revision"`
	ConsecutiveFailures int32      `db:"consecutive_failures"`
	OpenedAt            time.Time  `db:"opened_at"`
	ResolvedAt          *time.Time `db:"resolved_at"`
	CreatedAt           time.Time  `db:"created_at"`
}

// HealthStatus is what an endpoint read shows; paused and archived are separate endpoint fields.
type HealthStatus struct {
	State               string
	ObservedAt          *time.Time
	Stale               bool // the last observation is older than the staleness window
	ConsecutiveFailures int32
}
