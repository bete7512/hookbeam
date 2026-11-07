package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

func TestListDeliveryAttempts(t *testing.T) {
	code := int32(503)
	attempts := []models.Attempt{{ID: 1, DeliveryID: 5, Number: 1, StartedAt: ada.AuthenticatedAt, CompletedAt: &ada.ExpiresAt, StatusCode: &code, LatencyMS: 12, SecretVersion: 1, EndpointRevision: 1, RequestURL: "https://r"}}
	runEndpoints(t, []endpointCase{
		{"list", http.MethodGet, "/v1/deliveries/5/attempts", "", func(f *fixture) {
			f.delivery.EXPECT().ListAttempts(gomock.Any(), workspace, int64(5)).Return(attempts, nil)
		}, http.StatusOK, "", true},
		{"unknown delivery", http.MethodGet, "/v1/deliveries/5/attempts", "", func(f *fixture) {
			f.delivery.EXPECT().ListAttempts(gomock.Any(), workspace, int64(5)).Return(nil, apperr.NewNotFound("delivery", nil))
		}, http.StatusNotFound, apperr.CodeNotFound, true},
		{"bad id", http.MethodGet, "/v1/deliveries/x/attempts", "", nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
	})

	t.Run("body", func(t *testing.T) {
		f := newFixture(t)
		f.delivery.EXPECT().ListAttempts(gomock.Any(), workspace, int64(5)).Return(attempts, nil)
		rec := f.do(http.MethodGet, "/v1/deliveries/5/attempts", "")
		require.Equal(t, http.StatusOK, rec.Code)
		var got openapi.AttemptList
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Len(t, got.Items, 1)
		assert.Equal(t, int32(503), *got.Items[0].StatusCode)
		assert.Nil(t, got.Items[0].ErrorClass)
		assert.Equal(t, "https://r", got.Items[0].RequestUrl)
	})
}
