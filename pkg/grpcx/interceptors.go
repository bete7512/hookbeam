package grpcx

import (
	"context"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/bete7512/hookbeam/pkg/logger"
)

const healthServicePrefix = "/grpc.health.v1.Health/"

// ServerInterceptors is the standard unary chain: request id, access log, panic recovery, scope metadata.
// Health checks are exempt from the scope requirement.
func ServerInterceptors(l *slog.Logger) grpc.ServerOption {
	return grpc.ChainUnaryInterceptor(requestID(), accessLog(l), recoverPanics(l), requireScope())
}

func requestID() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		id := first(md, MetadataRequestID)
		if id == "" || len(id) > 128 {
			id = uuid.NewString()
		}
		return handler(logger.WithRequestID(ctx, id), req)
	}
}

// accessLog writes one line per call: Info for codes a client caused, Error for the rest.
func accessLog(l *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		st, _ := status.FromError(err)
		level := slog.LevelInfo
		if st.Code() == codes.Internal || st.Code() == codes.Unknown || st.Code() == codes.DataLoss {
			level = slog.LevelError
		}
		attrs := []slog.Attr{
			slog.String("method", info.FullMethod),
			slog.String("code", st.Code().String()),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
		}
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if ws := first(md, MetadataWorkspaceID); ws != "" {
				attrs = append(attrs, slog.String("workspace_id", ws))
			}
		}
		if err != nil {
			attrs = append(attrs, slog.String("error", err.Error()))
		}
		l.LogAttrs(ctx, level, "grpc request", attrs...)
		return resp, err
	}
}

func recoverPanics(l *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if p := recover(); p != nil {
				l.ErrorContext(ctx, "panic in grpc handler", "method", info.FullMethod, "panic", p, "stack", string(debug.Stack()))
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

// requireScope rejects calls without a workspace id and stores the scope for handlers.
func requireScope() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if strings.HasPrefix(info.FullMethod, healthServicePrefix) {
			return handler(ctx, req)
		}
		md, _ := metadata.FromIncomingContext(ctx)
		s := Scope{WorkspaceID: first(md, MetadataWorkspaceID), ActorID: first(md, MetadataActorID)}
		if s.WorkspaceID == "" {
			return nil, status.Error(codes.Unauthenticated, "missing "+MetadataWorkspaceID+" metadata")
		}
		return handler(withScope(ctx, s), req)
	}
}

// ClientInterceptor forwards the request id from the context on every outgoing call.
func ClientInterceptor() grpc.DialOption {
	return grpc.WithChainUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if id := logger.RequestIDFrom(ctx); id != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, MetadataRequestID, id)
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	})
}
