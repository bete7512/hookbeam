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
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

func TestListAuditEvents(t *testing.T) {
	id := int64(3)
	events := []models.AuditEvent{{ID: 1, Source: "delivery", ActorID: "u_ada", Action: "endpoint.pause", ResourceType: "endpoint", ResourceID: &id, RequestID: "r", Details: []byte(`{"a":1}`), OccurredAt: ada.AuthenticatedAt}}
	runEndpoints(t, []endpointCase{
		{"list with filters", http.MethodGet, "/v1/audit?resource_type=endpoint&resource_id=3&limit=5&offset=10", "", func(f *fixture) {
			f.audit.EXPECT().List(gomock.Any(), adaActor, services.ListAuditInput{ResourceType: "endpoint", ResourceID: 3, Limit: 5, Offset: 10}).Return(events, 1, nil)
		}, http.StatusOK, "", true},
		{"list defaults", http.MethodGet, "/v1/audit", "", func(f *fixture) {
			f.audit.EXPECT().List(gomock.Any(), adaActor, services.ListAuditInput{Limit: 20}).Return(nil, 0, nil)
		}, http.StatusOK, "", true},
		{"forbidden outside the workspace", http.MethodGet, "/v1/audit", "", func(f *fixture) {
			f.audit.EXPECT().List(gomock.Any(), adaActor, gomock.Any()).Return(nil, 0, apperr.NewForbidden("not a member of this workspace", nil))
		}, http.StatusForbidden, apperr.CodeForbidden, true},
		{"bad limit binds before the service", http.MethodGet, "/v1/audit?limit=abc", "", nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
	})
	t.Run("body", func(t *testing.T) {
		f := newFixture(t)
		f.audit.EXPECT().List(gomock.Any(), adaActor, gomock.Any()).Return(events, 1, nil)
		rec := f.do(http.MethodGet, "/v1/audit", "")
		require.Equal(t, http.StatusOK, rec.Code)
		var got openapi.AuditEventList
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Len(t, got.Items, 1)
		assert.Equal(t, openapi.AuditEventSource("delivery"), got.Items[0].Source)
		assert.InDelta(t, 1.0, got.Items[0].Details["a"], 0)
		assert.Equal(t, int64(3), *got.Items[0].ResourceId)
	})
}
