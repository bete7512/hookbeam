// Command setup-env creates local configuration and fills missing development keys.
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
	"strings"
)

func main() {
	path := flag.String("env", ".env", "local environment file")
	example := flag.String("example", ".env.example", "template used when the environment file is absent")
	flag.Parse()
	if err := setup(*path, *example); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// generators produce a value for each key that is absent or blank.
var generators = map[string]func() (string, error){
	"AUTH_SECRET_KEY":      randomBase64,
	"AUTH_ENCRYPTION_KEY":  randomBase64,
	"AUTH_JWT_PRIVATE_KEY": ecKeyPEM,
	"DELIVERY_SECRETS_KEY": randomBase64,
}

// keys lists the generated variables in the order they are appended when absent.
var keys = []string{"AUTH_SECRET_KEY", "AUTH_ENCRYPTION_KEY", "AUTH_JWT_PRIVATE_KEY", "DELIVERY_SECRETS_KEY"}

func setup(path, example string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		data, err = os.ReadFile(example)
	}
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	for _, key := range keys {
		found := false
		for i, line := range lines {
			name, value, ok := strings.Cut(line, "=")
			if !ok || strings.TrimSpace(name) != key {
				continue
			}
			found = true
			value, _, _ = strings.Cut(value, "#")
			if strings.TrimSpace(value) == "" {
				secret, err := generators[key]()
				if err != nil {
					return err
				}
				lines[i] = key + "=" + secret
			}
		}
		if !found {
			secret, err := generators[key]()
			if err != nil {
				return err
			}
			lines = append(lines, key+"="+secret)
		}
	}
	// The local CLI operator chooses the output path; no server accepts this input.
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600) //nolint:gosec // local CLI path supplied by the operator
}

func randomBase64() (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(secret), nil
}

// ecKeyPEM returns a base64-encoded PKCS#8 PEM of a fresh P-256 key for ES256.
func ecKeyPEM() (string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
}
