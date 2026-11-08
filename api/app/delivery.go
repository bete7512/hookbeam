package app

import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	"github.com/bete7512/hookbeam/api/config"
	"github.com/bete7512/hookbeam/pkg/grpcx"
)

// newDeliveryConn dials the delivery service lazily over mTLS; the first RPC connects.
func newDeliveryConn(cfg config.Delivery) (*grpc.ClientConn, error) {
	creds, err := cfg.TLS.ClientCredentials(cfg.ServerName)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(cfg.Addr, grpc.WithTransportCredentials(creds), grpcx.TracingClient(), grpcx.ClientInterceptor())
	if err != nil {
		return nil, fmt.Errorf("app: delivery client: %w", err)
	}
	return conn, nil
}

// deliveryPinger reports the delivery service's health for /readyz.
type deliveryPinger struct {
	conn *grpc.ClientConn
}

func (p deliveryPinger) Ping(ctx context.Context) error {
	return grpcx.Ping(ctx, p.conn)
}
