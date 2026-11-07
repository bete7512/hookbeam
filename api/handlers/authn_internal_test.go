package handlers

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/api/gen/openapi"
)

func TestAccessModes(t *testing.T) {
	bearer := openapi3.SecurityRequirements{{"bearerAuth": {}}}
	optional := openapi3.SecurityRequirements{{}, {"bearerAuth": {}}}
	public := openapi3.SecurityRequirements{}
	apiKey := openapi3.SecurityRequirements{{"apiKeyAuth": {}}}
	both := openapi3.SecurityRequirements{{"cookieAuth": {}}, {"apiKeyAuth": {}}}
	typo := openapi3.SecurityRequirements{{"bearerAuht": {}}}

	tests := []struct {
		name    string
		rootSec openapi3.SecurityRequirements
		opSec   *openapi3.SecurityRequirements
		want    accessRule
	}{
		{name: "undeclared without root default requires a session", want: accessRule{mode: accessRequired, user: true}},
		{name: "explicit empty security is public", opSec: &public, want: accessRule{mode: accessAnonymous}},
		{name: "bearer requirement", opSec: &bearer, want: accessRule{mode: accessRequired, user: true}},
		{name: "empty alternative is optional", opSec: &optional, want: accessRule{mode: accessOptional, user: true}},
		{name: "api key only", opSec: &apiKey, want: accessRule{mode: accessRequired, apiKey: true}},
		{name: "session or api key", opSec: &both, want: accessRule{mode: accessRequired, user: true, apiKey: true}},
		{name: "unknown scheme name still requires a session", opSec: &typo, want: accessRule{mode: accessRequired, user: true}},
		{name: "undeclared inherits a public root", rootSec: public, want: accessRule{mode: accessAnonymous}},
		{name: "undeclared inherits a protected root", rootSec: bearer, want: accessRule{mode: accessRequired, user: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := &openapi3.T{Security: tt.rootSec, Paths: openapi3.NewPaths()}
			doc.Paths.Set("/things", &openapi3.PathItem{Get: &openapi3.Operation{Security: tt.opSec}})

			assert.Equal(t, tt.want, accessModes(doc)["GET /things"])
		})
	}
}

func TestEveryOperationDeclaresSecurity(t *testing.T) {
	doc, err := openapi.GetSpec()
	require.NoError(t, err)

	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			assert.NotNil(t, op.Security, "%s %s must declare security: [] for public or bearerAuth/cookieAuth/apiKeyAuth for protected", method, path)
		}
	}
}
