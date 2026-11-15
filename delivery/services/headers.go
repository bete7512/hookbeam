package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/bete7512/hookbeam/pkg/secrets"
)

const (
	maxHeaders           = 20
	maxHeaderNameLength  = 64
	maxHeaderValueLength = 1024
)

// reservedHeaders are names an endpoint may not set: the ones Hookbeam sends itself, the ones that frame the
// request and the hop-by-hop ones. Names starting with reservedHeaderPrefixes are refused too.
var (
	reservedHeaders = []string{
		"host", "content-length", "content-type", "content-encoding", "transfer-encoding", "connection",
		"keep-alive", "upgrade", "te", "trailer", "expect", "user-agent",
	}
	reservedHeaderPrefixes = []string{"proxy-", "webhook-"}
)

// normalizeHeaders lower-cases the names and trims the values of a header change and checks each one. The
// message, empty when everything is valid, names the offending header and never quotes a value.
func normalizeHeaders(set map[string]string, remove []string) (map[string]string, []string, string) {
	normalized := make(map[string]string, len(set))
	for _, raw := range slices.Sorted(maps.Keys(set)) {
		name := strings.ToLower(strings.TrimSpace(raw))
		if msg := invalidHeaderName(name); msg != "" {
			return nil, nil, msg
		}
		if _, twice := normalized[name]; twice {
			return nil, nil, fmt.Sprintf("header %q is set twice", name)
		}
		value := strings.Trim(set[raw], " \t")
		if !validHeaderValue(value) {
			return nil, nil, fmt.Sprintf("header %q: the value must be 1 to %d visible ASCII characters", name, maxHeaderValueLength)
		}
		normalized[name] = value
	}
	removed := make([]string, 0, len(remove))
	for _, raw := range remove {
		name := strings.ToLower(strings.TrimSpace(raw))
		if _, both := normalized[name]; both {
			return nil, nil, fmt.Sprintf("header %q is both set and removed", name)
		}
		if name != "" && !slices.Contains(removed, name) {
			removed = append(removed, name)
		}
	}
	return normalized, removed, ""
}

// invalidHeaderName says why a lower-cased name cannot be a custom header, or returns "".
func invalidHeaderName(name string) string {
	if name == "" || len(name) > maxHeaderNameLength || strings.ContainsFunc(name, func(r rune) bool { return !headerTokenChar(r) }) {
		shown := name
		if len(shown) > maxHeaderNameLength {
			shown = shown[:maxHeaderNameLength]
		}
		return fmt.Sprintf("header %q: the name must be 1 to %d letters, digits or any of !#$%%&'*+-.^_`|~", shown, maxHeaderNameLength)
	}
	if slices.Contains(reservedHeaders, name) || slices.ContainsFunc(reservedHeaderPrefixes, func(p string) bool { return strings.HasPrefix(name, p) }) {
		return fmt.Sprintf("header %q is set by Hookbeam or by the connection and cannot be customised", name)
	}
	return ""
}

// headerTokenChar reports a character an HTTP field name may hold (RFC 9110 token).
func headerTokenChar(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	default:
		return strings.ContainsRune("!#$%&'*+-.^_`|~", r)
	}
}

// validHeaderValue accepts visible ASCII with inner spaces and tabs; anything else (CR, LF, NUL, non-ASCII)
// could split or smuggle a header.
func validHeaderValue(value string) bool {
	if value == "" || len(value) > maxHeaderValueLength {
		return false
	}
	for i := range len(value) {
		if c := value[i]; c != '\t' && (c < 0x20 || c > 0x7e) {
			return false
		}
	}
	return true
}

// mergeHeaders applies a normalized change to the stored set and returns the result, or a message when it
// would hold more than maxHeaders.
func mergeHeaders(current, set map[string]string, remove []string) (map[string]string, string) {
	merged := make(map[string]string, len(current)+len(set))
	maps.Copy(merged, current)
	for _, name := range remove {
		delete(merged, name)
	}
	maps.Copy(merged, set)
	if len(merged) > maxHeaders {
		return nil, fmt.Sprintf("an endpoint sends at most %d custom headers", maxHeaders)
	}
	return merged, ""
}

// sealHeaders encrypts the set as one JSON object; an empty set is stored as NULL.
func sealHeaders(cipher *secrets.Cipher, headers map[string]string) ([]byte, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	plain, err := json.Marshal(headers)
	if err != nil {
		return nil, err
	}
	return cipher.Encrypt(plain)
}

// openHeaders decrypts a stored set; no ciphertext is the empty set. Its errors never carry header content.
func openHeaders(cipher *secrets.Cipher, ciphertext []byte) (map[string]string, error) {
	if len(ciphertext) == 0 {
		return map[string]string{}, nil
	}
	plain, err := cipher.Decrypt(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt custom headers: %w", err)
	}
	headers := map[string]string{}
	if json.Unmarshal(plain, &headers) != nil {
		return nil, errors.New("stored custom headers are not a JSON object")
	}
	return headers, nil
}

// headerNames lists a set's names in order; never nil.
func headerNames(headers map[string]string) []string {
	names := slices.AppendSeq(make([]string, 0, len(headers)), maps.Keys(headers))
	slices.Sort(names)
	return names
}
