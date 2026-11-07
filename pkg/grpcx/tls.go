// Package grpcx holds the gRPC plumbing shared by the services: mTLS credentials, error mapping, metadata and interceptors.
package grpcx

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// TLS configures one side of a mutually authenticated connection.
// Insecure disables TLS altogether and is for local development only.
type TLS struct {
	CertFile string `env:"CERT_FILE"`
	KeyFile  string `env:"KEY_FILE"`
	CAFile   string `env:"CA_FILE"` // CA that signed the peer's certificate
	Insecure bool   `env:"INSECURE" envDefault:"false"`
}

// Validate checks that the files are named unless Insecure, and that Insecure is allowed.
func (t TLS) Validate(prefix string, allowInsecure bool) error {
	if t.Insecure {
		if !allowInsecure {
			return fmt.Errorf("%sINSECURE is not allowed in this environment", prefix)
		}
		return nil
	}
	var errs []error
	for name, value := range map[string]string{"CERT_FILE": t.CertFile, "KEY_FILE": t.KeyFile, "CA_FILE": t.CAFile} {
		if value == "" {
			errs = append(errs, fmt.Errorf("%s%s is required unless %sINSECURE=true", prefix, name, prefix))
		}
	}
	return errors.Join(errs...)
}

// ServerCredentials returns credentials that present the server certificate and require a client certificate signed by the CA.
func (t TLS) ServerCredentials() (credentials.TransportCredentials, error) {
	if t.Insecure {
		return insecure.NewCredentials(), nil
	}
	cert, pool, err := t.load()
	if err != nil {
		return nil, err
	}
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}), nil
}

// ClientCredentials returns credentials that present the client certificate and verify the server against the CA.
// serverName must match a name in the server certificate; empty uses the dial target's host.
func (t TLS) ClientCredentials(serverName string) (credentials.TransportCredentials, error) {
	if t.Insecure {
		return insecure.NewCredentials(), nil
	}
	cert, pool, err := t.load()
	if err != nil {
		return nil, err
	}
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ServerName:   serverName,
		MinVersion:   tls.VersionTLS13,
	}), nil
}

func (t TLS) load() (tls.Certificate, *x509.CertPool, error) {
	cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("grpcx: load key pair: %w", err)
	}
	caPEM, err := os.ReadFile(t.CAFile)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("grpcx: read ca: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return tls.Certificate{}, nil, fmt.Errorf("grpcx: %s holds no certificate", t.CAFile)
	}
	return cert, pool, nil
}
