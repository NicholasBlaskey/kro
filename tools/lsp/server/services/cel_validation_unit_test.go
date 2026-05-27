// Copyright 2025 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package services

import (
	"testing"

	"github.com/kro-run/kro/tools/lsp/server/analysis"
	kcel "github.com/kubernetes-sigs/kro/pkg/cel"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

// TestTypedEnvironmentWithSchema tests that we can create a typed CEL environment
func TestTypedEnvironmentWithSchema(t *testing.T) {
	// Create a simple schema with a string field and an int field
	schema := &spec.Schema{
		SchemaProps: spec.SchemaProps{
			Type: []string{"object"},
			Properties: map[string]spec.Schema{
				"spec": {
					SchemaProps: spec.SchemaProps{
						Type: []string{"object"},
						Properties: map[string]spec.Schema{
							"name": {
								SchemaProps: spec.SchemaProps{Type: []string{"string"}},
							},
							"replicas": {
								SchemaProps: spec.SchemaProps{Type: []string{"integer"}},
							},
						},
					},
				},
			},
		},
	}

	// Create typed environment
	schemas := map[string]*spec.Schema{
		"schema": schema,
	}

	env, err := kcel.TypedEnvironment(schemas)
	if err != nil {
		t.Fatalf("Failed to create typed environment: %v", err)
	}

	// Test 1: Valid string method on string field
	t.Run("valid string method", func(t *testing.T) {
		ast, issues := env.Parse("schema.spec.name.split('-')")
		if issues != nil && issues.Err() != nil {
			t.Fatalf("Parse error: %v", issues.Err())
		}

		_, issues = env.Check(ast)
		if issues != nil && issues.Err() != nil {
			t.Errorf("Type check failed (should succeed): %v", issues.Err())
		}
	})

	// Test 2: Invalid string method on int field
	t.Run("string method on int field", func(t *testing.T) {
		ast, issues := env.Parse("schema.spec.replicas.split('-')")
		if issues != nil && issues.Err() != nil {
			t.Fatalf("Parse error: %v", issues.Err())
		}

		_, issues = env.Check(ast)
		if issues == nil || issues.Err() == nil {
			t.Error("Expected type error for string method on int field, but got none")
		} else {
			t.Logf("Got expected type error: %v", issues.Err())
		}
	})

	// Test 3: Valid int field access
	t.Run("valid int access", func(t *testing.T) {
		ast, issues := env.Parse("schema.spec.replicas")
		if issues != nil && issues.Err() != nil {
			t.Fatalf("Parse error: %v", issues.Err())
		}

		_, issues = env.Check(ast)
		if issues != nil && issues.Err() != nil {
			t.Errorf("Type check failed (should succeed): %v", issues.Err())
		}
	})

	// Test 4: Wrong argument type
	t.Run("wrong argument type", func(t *testing.T) {
		ast, issues := env.Parse("schema.spec.name.contains(42)")
		if issues != nil && issues.Err() != nil {
			t.Fatalf("Parse error: %v", issues.Err())
		}

		_, issues = env.Check(ast)
		if issues == nil || issues.Err() == nil {
			t.Error("Expected type error for wrong argument type, but got none")
		} else {
			t.Logf("Got expected type error: %v", issues.Err())
		}
	})
}

// TestSymbolTableSchemaConversion tests that SymbolTable correctly converts to OpenAPI
func TestSymbolTableSchemaConversion(t *testing.T) {
	st := &analysis.SymbolTable{
		Schema: &analysis.SchemaSymbol{
			Kind:       "TestApp",
			APIVersion: "v1alpha1",
			SpecFields: map[string]*analysis.FieldSymbol{
				"name": {
					Name: "name",
					Type: "string",
					Path: "spec.name",
				},
				"replicas": {
					Name: "replicas",
					Type: "integer",
					Path: "spec.replicas",
				},
				"enabled": {
					Name: "enabled",
					Type: "boolean",
					Path: "spec.enabled",
				},
			},
		},
	}

	schema := st.GetInstanceSchema()
	if schema == nil {
		t.Fatal("GetInstanceSchema returned nil")
	}

	// Check spec object exists
	if schema.Properties == nil {
		t.Fatal("Schema has no properties")
	}

	specSchema, ok := schema.Properties["spec"]
	if !ok {
		t.Fatal("Schema missing 'spec' property")
	}

	// Check field types
	if specSchema.Properties == nil {
		t.Fatal("Spec schema has no properties")
	}

	// Check string field
	nameSchema, ok := specSchema.Properties["name"]
	if !ok {
		t.Error("Missing 'name' field")
	} else if len(nameSchema.Type) == 0 || nameSchema.Type[0] != "string" {
		t.Errorf("Expected name type 'string', got '%v'", nameSchema.Type)
	}

	// Check integer field
	replicasSchema, ok := specSchema.Properties["replicas"]
	if !ok {
		t.Error("Missing 'replicas' field")
	} else if len(replicasSchema.Type) == 0 || replicasSchema.Type[0] != "integer" {
		t.Errorf("Expected replicas type 'integer', got '%v'", replicasSchema.Type)
	}

	// Check boolean field
	enabledSchema, ok := specSchema.Properties["enabled"]
	if !ok {
		t.Error("Missing 'enabled' field")
	} else if len(enabledSchema.Type) == 0 || enabledSchema.Type[0] != "boolean" {
		t.Errorf("Expected enabled type 'boolean', got '%v'", enabledSchema.Type)
	}
}

// TestCELValidatorWithTypedEnvironment tests the full integration
func TestCELValidatorWithTypedEnvironment(t *testing.T) {
	st := &analysis.SymbolTable{
		Schema: &analysis.SchemaSymbol{
			SpecFields: map[string]*analysis.FieldSymbol{
				"name": {Name: "name", Type: "string"},
				"count": {Name: "count", Type: "integer"},
			},
		},
	}

	validator := NewCELValidator(st)

	// Test that buildSchemaMap works
	schemas := validator.buildSchemaMap()
	if len(schemas) == 0 {
		t.Error("Expected schemas to be built")
	}

	if _, ok := schemas["schema"]; !ok {
		t.Error("Expected 'schema' to be in schema map")
	}
}
