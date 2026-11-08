package grpcx

import (
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
)

// TracingServer makes every RPC a server span that joins the caller's trace through the metadata.
func TracingServer() grpc.ServerOption { return grpc.StatsHandler(otelgrpc.NewServerHandler()) }

// TracingClient propagates the caller's trace on every outgoing RPC and records a client span.
func TracingClient() grpc.DialOption { return grpc.WithStatsHandler(otelgrpc.NewClientHandler()) }
