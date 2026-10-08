// Package gen holds the types and server interface generated from docs/openapi.yaml.
// Only adapters and platform code may import it.
package gen

//go:generate go tool oapi-codegen -config oapi-codegen.yaml ../../../../docs/openapi.yaml
