// Command bootstrap writes, once, what the compose stack needs from disk: the local CA with server and client
// certificates for the mTLS between the planes, the ES256 signing key and the random keys goauth and the data
// plane encrypt with. Files that exist are kept, so a restart changes nothing.
//
//	go run ./scripts/bootstrap -dir /secrets
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bete7512/hookbeam/pkg/certs"
)

func main() {
	dir := flag.String("dir", "deploy/secrets", "output directory")
	hosts := flag.String("hosts", "localhost,127.0.0.1,delivery", "names the server certificate is valid for")
	validity := flag.Duration("validity", 365*24*time.Hour, "certificate lifetime")
	flag.Parse()
	if err := run(*dir, strings.Split(*hosts, ","), *validity); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap:", err)
		os.Exit(1)
	}
}

func run(dir string, hosts []string, validity time.Duration) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	certDir := filepath.Join(dir, "certs")
	if certs.Paths(certDir).Exist() {
		fmt.Println("bootstrap: certificates present")
	} else {
		if _, err := certs.Generate(certDir, hosts, validity); err != nil {
			return err
		}
		fmt.Println("bootstrap: wrote the CA, server and client certificates")
	}
	files := []struct {
		name string
		gen  func() ([]byte, error)
	}{
		{"jwt.pem", ecKeyPEM},
		{"auth_secret.key", randomBase64},
		{"auth_encryption.key", randomBase64},
		{"delivery_secrets.key", randomBase64},
	}
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		data, err := f.gen()
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return err
		}
		fmt.Println("bootstrap: wrote", f.name)
	}
	return nil
}

func randomBase64() ([]byte, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	return []byte(base64.StdEncoding.EncodeToString(secret)), nil
}

// ecKeyPEM returns a PKCS#8 PEM of a fresh P-256 key for ES256.
func ecKeyPEM() ([]byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}
