package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

func TestReadBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"a":1}`))
	body, err := httpx.ReadBody(req)
	require.NoError(t, err)
	require.Equal(t, `{"a":1}`, string(body))

	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 100)))
	req.Body = http.MaxBytesReader(httptest.NewRecorder(), req.Body, 10)
	_, err = httpx.ReadBody(req)
	var appErr *apperr.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, http.StatusRequestEntityTooLarge, appErr.Code)
}
