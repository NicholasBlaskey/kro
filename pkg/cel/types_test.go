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

package cel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

// TestStatusFieldAccess tests that .status and .metadata fields are accessible
// on typed resources even when not present in the OpenAPI schema
func TestStatusFieldAccess(t *testing.T) {
	// Create a Deployment-like schema WITHOUT status fields
	deploymentSchema := &spec.Schema{
		SchemaProps: spec.SchemaProps{
			Type: []string{"object"},
			Properties: map[string]spec.Schema{
				"apiVersion": {
					SchemaProps: spec.SchemaProps{Type: []string{"string"}},
				},
				"kind": {
					SchemaProps: spec.SchemaProps{Type: []string{"string"}},
				},
				"metadata": {
					SchemaProps: spec.SchemaProps{
						Type: []string{"object"},
						Properties: map[string]spec.Schema{
							"name": {
								SchemaProps: spec.SchemaProps{Type: []string{"string"}},
							},
						},
					},
				},
				"spec": {
					SchemaProps: spec.SchemaProps{
						Type: []string{"object"},
						Properties: map[string]spec.Schema{
							"replicas": {
								SchemaProps: spec.SchemaProps{Type: []string{"integer"}},
							},
						},
					},
				},
				// NOTE: status is NOT in the schema - this simulates real K8s schemas
			},
		},
	}

	schemas := map[string]*spec.Schema{
		"deployment": deploymentSchema,
	}

	env, err := TypedEnvironment(schemas)
	require.NoError(t, err, "failed to create typed environment")

	tests := []struct {
		name    string
		expr    string
		wantErr bool
	}{
		{
			name:    "access status field",
			expr:    "deployment.status",
			wantErr: false, // Should work even though status not in schema
		},
		{
			name:    "access status.conditions",
			expr:    "deployment.status.conditions",
			wantErr: false, // Status returns dyn, so any field access is valid
		},
		{
			name:    "access status.availableReplicas",
			expr:    "deployment.status.availableReplicas",
			wantErr: false,
		},
		{
			name:    "access metadata.name",
			expr:    "deployment.metadata.name",
			wantErr: false, // Metadata IS in schema, should work
		},
		{
			name:    "access metadata.uid",
			expr:    "deployment.metadata.uid",
			wantErr: true, // uid is not in the schema - strict typing still applies within metadata
		},
		{
			name:    "access spec.replicas",
			expr:    "deployment.spec.replicas",
			wantErr: false, // This IS in the schema
		},
		{
			name:    "access nonexistent top-level field",
			expr:    "deployment.foobar",
			wantErr: true, // Random fields should still error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Parse(tt.expr)
			require.NoError(t, issues.Err(), "parse should succeed")

			_, issues = env.Check(ast)
			if tt.wantErr {
				assert.Error(t, issues.Err(), "expected type check to fail for %s", tt.expr)
			} else {
				assert.NoError(t, issues.Err(), "expected type check to succeed for %s", tt.expr)
			}
		})
	}
}

// TestStatusFieldAccessOnUntypedResource tests that status works on untyped resources too
func TestStatusFieldAccessOnUntypedResource(t *testing.T) {
	// Create env with untyped resource (just a variable of type any)
	env, err := DefaultEnvironment(WithResourceIDs([]string{"deployment"}))
	require.NoError(t, err)

	// With untyped resources, everything should work since they're type 'any'
	expr := "deployment.status.conditions"
	ast, issues := env.Parse(expr)
	require.NoError(t, issues.Err())

	_, issues = env.Check(ast)
	assert.NoError(t, issues.Err(), "untyped resources should allow any field access")
}
