package schemas

// Generate Kubernetes OpenAPI schemas for validation
//
// This will generate schemas for Kubernetes versions 1.28 through latest
// and create a schemas.go file with embedded schema data.
//
// To regenerate schemas:
//   go generate ./pkg/schemas/generate_schemas.go

//go:generate sh -c "cd generator && go run generate.go 1.28"
