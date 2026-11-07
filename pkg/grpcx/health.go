package grpcx

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// RegisterHealth adds the standard health service to s and returns it, serving once the caller marks it so.
func RegisterHealth(s *grpc.Server) *health.Server {
	h := health.NewServer()
	healthpb.RegisterHealthServer(s, h)
	return h
}

// Ping asks the peer's health service whether it is serving.
func Ping(ctx context.Context, conn grpc.ClientConnInterface) error {
	resp, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		return fmt.Errorf("grpcx: health check: %w", err)
	}
	if resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		return fmt.Errorf("grpcx: peer is %s", resp.GetStatus())
	}
	return nil
}
