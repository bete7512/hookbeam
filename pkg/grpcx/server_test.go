package grpcx_test

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/bete7512/hookbeam/pkg/certs"
	"github.com/bete7512/hookbeam/pkg/grpcx"
	"github.com/bete7512/hookbeam/pkg/logger"
)

// echo is a one-method service registered by hand so the scope interceptor has something to guard.
const echoMethod = "/test.Echo/Ping"

type echoServer struct {
	gotScope grpcx.Scope
	gotReqID string
	panics   bool
}

var echoDesc = grpc.ServiceDesc{
	ServiceName: "test.Echo",
	HandlerType: (*any)(nil),
	Methods: []grpc.MethodDesc{{
		MethodName: "Ping",
		Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			in := new(healthpb.HealthCheckRequest)
			if err := dec(in); err != nil {
				return nil, err
			}
			handler := func(ctx context.Context, _ any) (any, error) {
				e := srv.(*echoServer)
				if e.panics {
					panic("boom")
				}
				e.gotScope, _ = grpcx.ScopeFromContext(ctx)
				e.gotReqID = logger.RequestIDFrom(ctx)
				return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
			}
			if interceptor == nil {
				return handler(ctx, in)
			}
			return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: echoMethod}, handler)
		},
	}},
}

// start serves health and echo on a loopback listener with the given credentials.
func start(t *testing.T, creds credentials.TransportCredentials, logs *bytes.Buffer) (string, *echoServer) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	l := logger.New(logger.Options{Level: "info", Format: "json", Writer: logs})
	srv := grpc.NewServer(grpc.Creds(creds), grpcx.ServerInterceptors(l))
	grpcx.RegisterHealth(srv).Resume()
	echo := &echoServer{}
	srv.RegisterService(&echoDesc, echo)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String(), echo
}

func dial(t *testing.T, addr string, creds credentials.TransportCredentials) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds), grpcx.ClientInterceptor())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func ping(ctx context.Context, conn *grpc.ClientConn) error {
	return conn.Invoke(ctx, echoMethod, &healthpb.HealthCheckRequest{}, &healthpb.HealthCheckResponse{})
}

func TestMutualTLS(t *testing.T) {
	files, err := certs.Generate(t.TempDir(), []string{"localhost", "127.0.0.1"}, time.Hour)
	require.NoError(t, err)
	server := grpcx.TLS{CertFile: files.ServerCert, KeyFile: files.ServerKey, CAFile: files.CA}
	client := grpcx.TLS{CertFile: files.ClientCert, KeyFile: files.ClientKey, CAFile: files.CA}
	serverCreds, err := server.ServerCredentials()
	require.NoError(t, err)
	addr, _ := start(t, serverCreds, &bytes.Buffer{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientCreds, err := client.ClientCredentials("")
	require.NoError(t, err)
	require.NoError(t, grpcx.Ping(ctx, dial(t, addr, clientCreds)), "client certificate signed by the CA is accepted")

	// A second CA's client certificate is rejected by the server.
	other, err := certs.Generate(t.TempDir(), []string{"localhost"}, time.Hour)
	require.NoError(t, err)
	strangerCreds, err := grpcx.TLS{CertFile: other.ClientCert, KeyFile: other.ClientKey, CAFile: files.CA}.ClientCredentials("")
	require.NoError(t, err)
	require.Error(t, grpcx.Ping(ctx, dial(t, addr, strangerCreds)), "client certificate from another CA is refused")

	// A client that trusts the wrong CA refuses the server.
	wrongRoot, err := grpcx.TLS{CertFile: files.ClientCert, KeyFile: files.ClientKey, CAFile: other.CA}.ClientCredentials("")
	require.NoError(t, err)
	require.Error(t, grpcx.Ping(ctx, dial(t, addr, wrongRoot)))
}

func TestTLSValidate(t *testing.T) {
	require.NoError(t, grpcx.TLS{Insecure: true}.Validate("X_", true))
	require.ErrorContains(t, grpcx.TLS{Insecure: true}.Validate("X_", false), "X_INSECURE is not allowed")
	require.NoError(t, grpcx.TLS{CertFile: "a", KeyFile: "b", CAFile: "c"}.Validate("X_", false))
	err := grpcx.TLS{CertFile: "a"}.Validate("X_", true)
	require.ErrorContains(t, err, "X_KEY_FILE is required")
	require.ErrorContains(t, err, "X_CA_FILE is required")

	_, err = grpcx.TLS{CertFile: "missing", KeyFile: "missing", CAFile: "missing"}.ServerCredentials()
	require.Error(t, err)
	_, err = grpcx.TLS{CertFile: "missing", KeyFile: "missing", CAFile: "missing"}.ClientCredentials("")
	require.Error(t, err)
}

func TestInterceptors(t *testing.T) {
	var logs bytes.Buffer
	insecureCreds, err := grpcx.TLS{Insecure: true}.ServerCredentials()
	require.NoError(t, err)
	addr, echo := start(t, insecureCreds, &logs)
	clientCreds, err := grpcx.TLS{Insecure: true}.ClientCredentials("")
	require.NoError(t, err)
	conn := dial(t, addr, clientCreds)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, grpcx.Ping(ctx, conn), "health needs no scope")

	err = ping(ctx, conn)
	require.Equal(t, codes.Unauthenticated, status.Code(err), "scoped method without workspace metadata")

	scoped := grpcx.WithScope(logger.WithRequestID(ctx, "req-1"), grpcx.Scope{WorkspaceID: "ws_1", ActorID: "u_1"})
	require.NoError(t, ping(scoped, conn))
	require.Equal(t, grpcx.Scope{WorkspaceID: "ws_1", ActorID: "u_1"}, echo.gotScope)
	require.Equal(t, "req-1", echo.gotReqID, "request id forwarded by the client interceptor")

	echo.gotReqID = ""
	require.NoError(t, ping(metadata.AppendToOutgoingContext(ctx, grpcx.MetadataWorkspaceID, "ws_2"), conn))
	require.NotEmpty(t, echo.gotReqID, "a request id is generated when none is sent")

	echo.panics = true
	err = ping(scoped, conn)
	require.Equal(t, codes.Internal, status.Code(err))
	require.Contains(t, logs.String(), `"msg":"panic in grpc handler"`)
	require.Contains(t, logs.String(), `"method":"/test.Echo/Ping"`)
	require.Contains(t, logs.String(), `"code":"Unauthenticated"`)
	require.Contains(t, logs.String(), `"workspace_id":"ws_1"`)
}

func TestScopeFromContext(t *testing.T) {
	_, ok := grpcx.ScopeFromContext(context.Background())
	require.False(t, ok)
}
