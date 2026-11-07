package handlers_test

import (
	"net/http"
	"testing"

	"github.com/bete7512/hookbeam/pkg/apperr"
)

func TestRouter(t *testing.T) {
	runEndpoints(t, []endpointCase{
		{"unknown route", http.MethodGet, "/nope", "", nil, http.StatusNotFound, apperr.CodeNotFound, false},
		{"openapi", http.MethodGet, "/openapi.json", "", nil, http.StatusOK, "", false},
		{"docs", http.MethodGet, "/docs", "", nil, http.StatusOK, "", false},
	})
}
