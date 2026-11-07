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

	tests := []struct {
		name       string
		rootSec    openapi3.SecurityRequirements
		opSec      *openapi3.SecurityRequirements
		wantAccess access
	}{
		{name: "undeclared without root default requires login", wantAccess: accessRequired},
		{name: "explicit empty security is public", opSec: &public, wantAccess: accessAnonymous},
		{name: "bearer requirement", opSec: &bearer, wantAccess: accessRequired},
		{name: "empty alternative is optional", opSec: &optional, wantAccess: accessOptional},
		{name: "undeclared inherits a public root", rootSec: public, wantAccess: accessAnonymous},
		{name: "undeclared inherits a protected root", rootSec: bearer, wantAccess: accessRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := &openapi3.T{Security: tt.rootSec, Paths: openapi3.NewPaths()}
			doc.Paths.Set("/things", &openapi3.PathItem{Get: &openapi3.Operation{Security: tt.opSec}})

			assert.Equal(t, tt.wantAccess, accessModes(doc)["GET /things"])
		})
	}
}

func TestEveryOperationDeclaresSecurity(t *testing.T) {
	doc, err := openapi.GetSpec()
	require.NoError(t, err)

	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			assert.NotNil(t, op.Security, "%s %s must declare security: [] for public or bearerAuth/cookieAuth for protected", method, path)
		}
	}
}
