package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/goauth/pkg/types"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/httpx"
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

func TestCancelDelivery(t *testing.T) {
	reason := "cancelled"
	cancelled := &models.Delivery{ID: 5, MessageID: 1, EndpointID: 2, State: "cancelled", FailureReason: &reason, DeadlineAt: ada.ExpiresAt, CreatedAt: ada.AuthenticatedAt, CompletedAt: &ada.ExpiresAt}
	runEndpoints(t, []endpointCase{
		{"cancel", http.MethodPost, "/v1/deliveries/5/cancel", "", func(f *fixture) {
			f.delivery.EXPECT().CancelDelivery(gomock.Any(), adaActor, int64(5)).Return(cancelled, nil)
		}, http.StatusOK, "", true},
		{"unknown delivery", http.MethodPost, "/v1/deliveries/5/cancel", "", func(f *fixture) {
			f.delivery.EXPECT().CancelDelivery(gomock.Any(), adaActor, int64(5)).Return(nil, apperr.NewNotFound("delivery", nil))
		}, http.StatusNotFound, apperr.CodeNotFound, true},
		{"delivery service down", http.MethodPost, "/v1/deliveries/5/cancel", "", func(f *fixture) {
			f.delivery.EXPECT().CancelDelivery(gomock.Any(), adaActor, int64(5)).Return(nil, apperr.NewUnavailable("delivery service unavailable", nil))
		}, http.StatusServiceUnavailable, apperr.CodeUnavailable, true},
		{"bad id", http.MethodPost, "/v1/deliveries/x/cancel", "", nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
	})

	t.Run("body", func(t *testing.T) {
		f := newFixture(t)
		f.delivery.EXPECT().CancelDelivery(gomock.Any(), adaActor, int64(5)).Return(cancelled, nil)
		rec := f.do(http.MethodPost, "/v1/deliveries/5/cancel", "")
		require.Equal(t, http.StatusOK, rec.Code)
		var got openapi.Delivery
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, openapi.DeliveryState("cancelled"), got.State)
		assert.Equal(t, "cancelled", *got.FailureReason)
	})
	t.Run("api key is refused", func(t *testing.T) {
		f := newFixture(t)
		f.apiKeys.EXPECT().Authenticate(gomock.Any(), "hb_key").Return(types.Principal{OrganizationID: workspace, Methods: []string{"api_key"}}, &models.APIKey{ID: 9, Scopes: []string{"messages:write"}}, nil).AnyTimes()
		req := httptest.NewRequest(http.MethodPost, "/v1/deliveries/5/cancel", nil)
		req.Header.Set("Authorization", "Bearer hb_key")
		rec := f.send(req)
		require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	})
}

func TestReplayDelivery(t *testing.T) {
	original := int64(5)
	replay := &models.Delivery{ID: 9, MessageID: 1, EndpointID: 2, State: "queued", DeadlineAt: ada.ExpiresAt, CreatedAt: ada.AuthenticatedAt, ReplayOfDeliveryID: &original}
	withKey := func(f *fixture, err error) {
		f.delivery.EXPECT().ReplayDelivery(gomock.Any(), adaActor, int64(5), "r1").Return(replay, err)
	}
	runEndpoints(t, []endpointCase{
		{"missing key is a binding error", http.MethodPost, "/v1/deliveries/5/replay", "", nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
		{"bad id", http.MethodPost, "/v1/deliveries/x/replay", "", nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
	})
	tests := []struct {
		name       string
		setup      func(f *fixture)
		wantStatus int
		wantError  string
	}{
		{name: "replayed", setup: func(f *fixture) { withKey(f, nil) }, wantStatus: http.StatusCreated},
		{name: "conflict", setup: func(f *fixture) {
			f.delivery.EXPECT().ReplayDelivery(gomock.Any(), adaActor, int64(5), "r1").Return(nil, apperr.NewConflict("active", nil))
		}, wantStatus: http.StatusConflict, wantError: apperr.CodeConflict},
		{name: "not found", setup: func(f *fixture) {
			f.delivery.EXPECT().ReplayDelivery(gomock.Any(), adaActor, int64(5), "r1").Return(nil, apperr.NewNotFound("delivery", nil))
		}, wantStatus: http.StatusNotFound, wantError: apperr.CodeNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			tt.setup(f)
			req := httptest.NewRequest(http.MethodPost, "/v1/deliveries/5/replay", nil)
			req.Header.Set("Authorization", "Bearer good")
			req.Header.Set("Idempotency-Key", "r1")
			rec := f.send(req)
			require.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			if tt.wantError != "" {
				var body httpx.ErrorResponse
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
				assert.Equal(t, tt.wantError, body.Error)
				return
			}
			var got openapi.Delivery
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
			assert.Equal(t, int64(9), got.Id)
			assert.Equal(t, int64(5), *got.ReplayOfDeliveryId)
		})
	}
	t.Run("without a token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/deliveries/5/replay", nil)
		req.Header.Set("Idempotency-Key", "r1")
		require.Equal(t, http.StatusUnauthorized, newFixture(t).send(req).Code)
	})
	t.Run("an api key with deliveries:replay replays as the workspace", func(t *testing.T) {
		f := newFixture(t)
		f.apiKeys.EXPECT().Authenticate(gomock.Any(), "hb_key").Return(types.Principal{OrganizationID: workspace, Methods: []string{"api_key"}}, &models.APIKey{ID: 9, Scopes: []string{models.ScopeDeliveriesReplay}}, nil)
		f.delivery.EXPECT().ReplayDelivery(gomock.Any(), services.Actor{WorkspaceID: workspace}, int64(5), "r1").Return(replay, nil)
		req := httptest.NewRequest(http.MethodPost, "/v1/deliveries/5/replay", nil)
		req.Header.Set("Authorization", "Bearer hb_key")
		req.Header.Set("Idempotency-Key", "r1")
		require.Equal(t, http.StatusCreated, f.send(req).Code)
	})
	t.Run("an api key without the scope is refused", func(t *testing.T) {
		f := newFixture(t)
		f.apiKeys.EXPECT().Authenticate(gomock.Any(), "hb_key").Return(types.Principal{OrganizationID: workspace, Methods: []string{"api_key"}}, &models.APIKey{ID: 9, Scopes: []string{models.ScopeMessagesWrite}}, nil)
		req := httptest.NewRequest(http.MethodPost, "/v1/deliveries/5/replay", nil)
		req.Header.Set("Authorization", "Bearer hb_key")
		req.Header.Set("Idempotency-Key", "r1")
		rec := f.send(req)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	})
}
