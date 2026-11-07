// Package rpc implements the DeliveryService gRPC contract over the services; it is imported only by cmd/server.
package rpc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	deliveryv1 "github.com/bete7512/hookbeam/contracts/gen/go/hookbeam/delivery/v1"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/grpcx"
)

// Deps are the server dependencies; all are required.
type Deps struct {
	Applications services.ApplicationService
	Endpoints    services.EndpointService
	Logger       *slog.Logger
}

// Server serves hookbeam.delivery.v1.DeliveryService.
type Server struct {
	deliveryv1.UnimplementedDeliveryServiceServer
	deps Deps
}

// New returns a Server or an error if a dependency is missing.
func New(deps Deps) (*Server, error) {
	if deps.Applications == nil || deps.Endpoints == nil || deps.Logger == nil {
		return nil, errors.New("rpc: Applications, Endpoints and Logger are required")
	}
	return &Server{deps: deps}, nil
}

// CreateApplication registers a consumer application in the caller's workspace.
func (s *Server) CreateApplication(ctx context.Context, req *deliveryv1.CreateApplicationRequest) (*deliveryv1.CreateApplicationResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.deps.Applications.CreateApplication(ctx, scope, req.GetName())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.CreateApplicationResponse{Application: toApplication(a)}, nil
}

// CreateEndpoint registers a destination and returns its signing secret once.
func (s *Server) CreateEndpoint(ctx context.Context, req *deliveryv1.CreateEndpointRequest) (*deliveryv1.CreateEndpointResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	e, secret, version, err := s.deps.Endpoints.CreateEndpoint(ctx, scope, services.CreateEndpointInput{
		ApplicationID: req.GetApplicationId(),
		URL:           req.GetUrl(),
		Description:   req.GetDescription(),
		EventTypes:    req.GetEventTypes(),
	})
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.CreateEndpointResponse{Endpoint: toEndpoint(e), SigningSecret: secret, SecretVersion: version}, nil
}

// PauseEndpoint stops new attempts for an endpoint.
func (s *Server) PauseEndpoint(ctx context.Context, req *deliveryv1.PauseEndpointRequest) (*deliveryv1.PauseEndpointResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	e, err := s.deps.Endpoints.PauseEndpoint(ctx, scope, req.GetEndpointId())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.PauseEndpointResponse{Endpoint: toEndpoint(e)}, nil
}

func scope(ctx context.Context) (services.Scope, error) {
	sc, ok := grpcx.ScopeFromContext(ctx)
	if !ok {
		return services.Scope{}, status.Error(codes.Unauthenticated, "missing workspace")
	}
	return services.Scope{WorkspaceID: sc.WorkspaceID, ActorID: sc.ActorID}, nil
}

func toApplication(a *models.Application) *deliveryv1.Application {
	return &deliveryv1.Application{
		Id:          a.ID,
		WorkspaceId: a.WorkspaceID,
		Name:        a.Name,
		CreatedBy:   a.CreatedBy,
		CreatedAt:   timestamppb.New(a.CreatedAt),
		ArchivedAt:  optionalTime(a.ArchivedAt),
	}
}

func toEndpoint(e *models.Endpoint) *deliveryv1.Endpoint {
	return &deliveryv1.Endpoint{
		Id:            e.ID,
		ApplicationId: e.ApplicationID,
		Url:           e.URL,
		Description:   e.Description,
		Revision:      e.Revision,
		EventTypes:    e.EventTypes,
		PausedAt:      optionalTime(e.PausedAt),
		ArchivedAt:    optionalTime(e.ArchivedAt),
		CreatedAt:     timestamppb.New(e.CreatedAt),
	}
}

func optionalTime(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}
