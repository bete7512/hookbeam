package grpcx

import (
	"context"

	"google.golang.org/grpc/metadata"
)

// Metadata keys the control plane sends on every call.
const (
	MetadataWorkspaceID = "x-hookbeam-workspace-id"
	MetadataActorID     = "x-hookbeam-actor-id" // user id, empty for an API key
	MetadataRequestID   = "x-request-id"
)

type workspaceKey struct{}

type actorKey struct{}

// Scope is who a call acts for; the server derives it from the metadata.
type Scope struct {
	WorkspaceID string
	ActorID     string
}

// WithScope attaches the scope as outgoing metadata; an empty actor (an API key) sends no actor key.
func WithScope(ctx context.Context, s Scope) context.Context {
	ctx = metadata.AppendToOutgoingContext(ctx, MetadataWorkspaceID, s.WorkspaceID)
	if s.ActorID != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, MetadataActorID, s.ActorID)
	}
	return ctx
}

// ScopeFromContext returns the scope the interceptor stored; ok is false outside a scoped call.
func ScopeFromContext(ctx context.Context) (Scope, bool) {
	ws, ok := ctx.Value(workspaceKey{}).(string)
	if !ok || ws == "" {
		return Scope{}, false
	}
	actor, _ := ctx.Value(actorKey{}).(string)
	return Scope{WorkspaceID: ws, ActorID: actor}, true
}

func withScope(ctx context.Context, s Scope) context.Context {
	ctx = context.WithValue(ctx, workspaceKey{}, s.WorkspaceID)
	return context.WithValue(ctx, actorKey{}, s.ActorID)
}

func first(md metadata.MD, key string) string {
	if values := md.Get(key); len(values) > 0 {
		return values[0]
	}
	return ""
}
