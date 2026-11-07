// Package openapi is generated from api/openapi; run `make gen-openapi`, never edit by hand.
package openapi

//go:generate go run ../../../scripts/openapi-bundle -in ../../openapi/openapi.yaml -out openapi.bundle.yaml
//go:generate oapi-codegen -config ../../openapi/oapi-codegen.yaml -o openapi.gen.go openapi.bundle.yaml
