// Package deliveryclient is the control plane's gRPC client to the data plane: it scopes, bounds and maps every call.
package deliveryclient

import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	"github.com/bete7512/hookbeam/api/config"
	"github.com/bete7512/hookbeam/pkg/grpcx"
)

// Dial connects lazily to the delivery service over mTLS; the first RPC connects.
func Dial(cfg config.Delivery) (*grpc.ClientConn, error) {
	creds, err := cfg.TLS.ClientCredentials(cfg.ServerName)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(cfg.Addr, grpc.WithTransportCredentials(creds), grpcx.TracingClient(), grpcx.ClientInterceptor())
	if err != nil {
		return nil, fmt.Errorf("deliveryclient: dial: %w", err)
	}
	return conn, nil
}

// Pinger answers /readyz for the delivery service.
type Pinger struct {
	Conn *grpc.ClientConn
}

// Ping asks the server's health service whether it is serving.
func (p Pinger) Ping(ctx context.Context) error {
	return grpcx.Ping(ctx, p.Conn)
}
