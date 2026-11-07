package handlers_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/bete7512/hookbeam/pkg/apperr"
)

func TestHealth(t *testing.T) {
	runEndpoints(t, []endpointCase{
		{"healthz", http.MethodGet, "/healthz", "", nil, http.StatusOK, "", false},
		{"readyz", http.MethodGet, "/readyz", "", nil, http.StatusOK, "", false},
		{"readyz db down", http.MethodGet, "/readyz", "", func(f *fixture) { f.dbErr = errors.New("refused") }, http.StatusServiceUnavailable, apperr.CodeUnavailable, false},
		{"readyz delivery down", http.MethodGet, "/readyz", "", func(f *fixture) { f.deliveryErr = errors.New("refused") }, http.StatusServiceUnavailable, apperr.CodeUnavailable, false},
	})
}
