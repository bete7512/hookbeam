package models

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
}
