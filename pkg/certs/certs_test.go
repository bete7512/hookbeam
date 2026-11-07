package certs_test

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/certs"
)

func TestGenerate(t *testing.T) {
	dir := t.TempDir()
	files, err := certs.Generate(dir, []string{"localhost", "127.0.0.1"}, time.Hour)
	require.NoError(t, err)
	require.True(t, files.Exist())
	require.Equal(t, certs.Paths(dir), files)

	caPEM, err := os.ReadFile(files.CA)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(caPEM))

	server, err := tls.LoadX509KeyPair(files.ServerCert, files.ServerKey)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(server.Certificate[0])
	require.NoError(t, err)
	_, err = leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "localhost", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	require.NoError(t, err, "server certificate chains to the CA for localhost")
	_, err = leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "other", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	require.Error(t, err, "server certificate is bound to its hosts")

	client, err := tls.LoadX509KeyPair(files.ClientCert, files.ClientKey)
	require.NoError(t, err)
	leaf, err = x509.ParseCertificate(client.Certificate[0])
	require.NoError(t, err)
	_, err = leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	require.NoError(t, err, "client certificate chains to the CA")

	info, err := os.Stat(files.ServerKey)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	require.False(t, certs.Paths(t.TempDir()).Exist())
}
