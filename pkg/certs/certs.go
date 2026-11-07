// Package certs generates a local certificate authority with one server and one client certificate for mTLS in development.
package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Files are the paths Generate writes under a directory.
type Files struct {
	CA         string
	ServerCert string
	ServerKey  string
	ClientCert string
	ClientKey  string
}

// Paths returns the file layout under dir.
func Paths(dir string) Files {
	return Files{
		CA:         filepath.Join(dir, "ca.crt"),
		ServerCert: filepath.Join(dir, "server.crt"),
		ServerKey:  filepath.Join(dir, "server.key"),
		ClientCert: filepath.Join(dir, "client.crt"),
		ClientKey:  filepath.Join(dir, "client.key"),
	}
}

// Exist reports whether every file is present.
func (f Files) Exist() bool {
	for _, p := range []string{f.CA, f.ServerCert, f.ServerKey, f.ClientCert, f.ClientKey} {
		if _, err := os.Stat(p); err != nil {
			return false
		}
	}
	return true
}

// Generate writes a CA, a server certificate valid for hosts (DNS names or IPs) and a client certificate into dir.
// Certificates are valid for validity from now; keys are P-256.
func Generate(dir string, hosts []string, validity time.Duration) (Files, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return Files{}, err
	}
	files := Paths(dir)
	now := time.Now()

	caKey, caCert, err := newCA(now, validity)
	if err != nil {
		return Files{}, err
	}
	if err := writePEM(files.CA, "CERTIFICATE", caCert.Raw, 0o644); err != nil {
		return Files{}, err
	}

	serverTemplate := &x509.Certificate{
		Subject:     pkix.Name{CommonName: "hookbeam-delivery", Organization: []string{"hookbeam"}},
		NotBefore:   now.Add(-time.Minute),
		NotAfter:    now.Add(validity),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			serverTemplate.IPAddresses = append(serverTemplate.IPAddresses, ip)
		} else {
			serverTemplate.DNSNames = append(serverTemplate.DNSNames, h)
		}
	}
	if err := issue(files.ServerCert, files.ServerKey, serverTemplate, caCert, caKey); err != nil {
		return Files{}, err
	}

	clientTemplate := &x509.Certificate{
		Subject:     pkix.Name{CommonName: "hookbeam-api", Organization: []string{"hookbeam"}},
		NotBefore:   now.Add(-time.Minute),
		NotAfter:    now.Add(validity),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	if err := issue(files.ClientCert, files.ClientKey, clientTemplate, caCert, caKey); err != nil {
		return Files{}, err
	}
	return files, nil
}

func newCA(now time.Time, validity time.Duration) (*ecdsa.PrivateKey, *x509.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := newSerial()
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "hookbeam local CA", Organization: []string{"hookbeam"}},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(validity),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	return key, cert, nil
}

// issue signs template with the CA and writes the certificate and its key.
func issue(certPath, keyPath string, template, caCert *x509.Certificate, caKey *ecdsa.PrivateKey) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	if template.SerialNumber, err = newSerial(); err != nil {
		return err
	}
	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	if err := writePEM(certPath, "CERTIFICATE", der, 0o644); err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	return writePEM(keyPath, "PRIVATE KEY", keyDER, 0o600)
}

func newSerial() (*big.Int, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("certs: serial: %w", err)
	}
	return serial, nil
}

func writePEM(path, kind string, der []byte, mode os.FileMode) error {
	data := pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der})
	// The operator chooses the directory; no server accepts this path.
	return os.WriteFile(path, data, mode) //nolint:gosec // path comes from the local operator
}
