// Command setup-env creates local configuration and fills missing development keys.
package main

import (
	"crypto/rand"
	"encoding/base64"
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

func setup(path, example string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		data, err = os.ReadFile(example)
	}
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	for _, key := range []string{"AUTH_JWT_SIGNING_KEY", "AUTH_ACTION_TOKEN_KEY"} {
		found := false
		for i, line := range lines {
			name, value, ok := strings.Cut(line, "=")
			if !ok || strings.TrimSpace(name) != key {
				continue
			}
			found = true
			value, _, _ = strings.Cut(value, "#")
			if strings.TrimSpace(value) == "" {
				secret := make([]byte, 32)
				if _, err := rand.Read(secret); err != nil {
					return err
				}
				lines[i] = key + "=" + base64.StdEncoding.EncodeToString(secret)
			}
		}
		if !found {
			secret := make([]byte, 32)
			if _, err := rand.Read(secret); err != nil {
				return err
			}
			lines = append(lines, key+"="+base64.StdEncoding.EncodeToString(secret))
		}
	}
	// The local CLI operator chooses the output path; no server accepts this input.
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600) //nolint:gosec // local CLI path supplied by the operator
}
