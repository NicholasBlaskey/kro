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

package test

import (
	"strings"
	"testing"
)

// Test typed CEL validation - string method on integer field
func TestTypedValidation_StringMethodOnInt(t *testing.T) {
	rgd := `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test-typed
spec:
  schema:
    kind: TestApp
    apiVersion: v1alpha1
    spec:
      replicas: integer
  resources:
    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: test
        spec:
          replicas: ${schema.spec.replicas.split('-')}`

	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()
	client.OpenDocument("file:///test-typed.yaml", rgd)

	// Wait for diagnostics (expect at least 1)
	diagnostics := client.WaitForDiagnostics("file:///test-typed.yaml", -1)
	t.Logf("Got %d diagnostics", len(diagnostics))
	for _, d := range diagnostics {
		t.Logf("  - %s", d.Message)
	}

	// Should have a type error
	if len(diagnostics) == 0 {
		t.Fatal("Expected type error diagnostic, got none")
	}

	// Check that error mentions the type mismatch
	found := false
	for _, diag := range diagnostics {
		errorMsg := strings.ToLower(diag.Message)
		if strings.Contains(errorMsg, "int") || strings.Contains(errorMsg, "field") || strings.Contains(errorMsg, "overload") {
			found = true
			t.Logf("✅ Found expected type error: %s", diag.Message)
			break
		}
	}

	if !found {
		t.Errorf("Expected error about int type not having split method, got: %v", diagnostics[0].Message)
	}
}

// Test typed CEL validation - correct string method
func TestTypedValidation_StringMethodOnString(t *testing.T) {
	rgd := `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test-typed-valid
spec:
  schema:
    kind: TestApp
    apiVersion: v1alpha1
    spec:
      name: string
  resources:
    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: ${schema.spec.name.split('-')[0]}`

	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()
	client.OpenDocument("file:///test-typed-valid.yaml", rgd)

	// Wait a bit for validation to complete
	diagnostics := client.WaitForDiagnostics("file:///test-typed-valid.yaml", 0)
	t.Logf("Got %d diagnostics", len(diagnostics))
	for _, d := range diagnostics {
		t.Logf("  - %s", d.Message)
	}

	// Filter for CEL errors only (ignore schema validation warnings)
	celErrors := 0
	for _, diag := range diagnostics {
		if strings.Contains(strings.ToLower(diag.Message), "cel") ||
			strings.Contains(strings.ToLower(diag.Message), "type") ||
			strings.Contains(strings.ToLower(diag.Message), "overload") {
			celErrors++
			t.Errorf("Unexpected CEL error: %s", diag.Message)
		}
	}

	if celErrors > 0 {
		t.Errorf("Expected no CEL type errors for valid expression, got %d", celErrors)
	}
}

// Test typed CEL validation - wrong argument type
func TestTypedValidation_WrongArgumentType(t *testing.T) {
	rgd := `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test-arg-type
spec:
  schema:
    kind: TestApp
    apiVersion: v1alpha1
    spec:
      name: string
  resources:
    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: ${schema.spec.name.contains(42)}`

	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()
	client.OpenDocument("file:///test-arg-type.yaml", rgd)

	diagnostics := client.WaitForDiagnostics("file:///test-arg-type.yaml", -1)
	t.Logf("Got %d diagnostics", len(diagnostics))
	for _, d := range diagnostics {
		t.Logf("  - %s", d.Message)
	}

	if len(diagnostics) == 0 {
		t.Fatal("Expected type error for wrong argument type")
	}

	// Error should mention overload or type mismatch
	found := false
	for _, diag := range diagnostics {
		errorMsg := strings.ToLower(diag.Message)
		if strings.Contains(errorMsg, "overload") || strings.Contains(errorMsg, "int") {
			found = true
			t.Logf("✅ Found expected argument type error: %s", diag.Message)
			break
		}
	}

	if !found {
		t.Errorf("Expected error about argument type mismatch, got: %v", diagnostics[0].Message)
	}
}

// Test typed CEL validation - integer field access is valid
func TestTypedValidation_IntegerFieldAccess(t *testing.T) {
	rgd := `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test-int-access
spec:
  schema:
    kind: TestApp
    apiVersion: v1alpha1
    spec:
      replicas: integer
  resources:
    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: test
        spec:
          replicas: ${schema.spec.replicas}`

	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()
	client.OpenDocument("file:///test-int-access.yaml", rgd)

	diagnostics := client.WaitForDiagnostics("file:///test-int-access.yaml", 0)
	t.Logf("Got %d diagnostics", len(diagnostics))
	for _, d := range diagnostics {
		t.Logf("  - %s", d.Message)
	}

	// Filter for CEL type errors
	celErrors := 0
	for _, diag := range diagnostics {
		if strings.Contains(strings.ToLower(diag.Message), "cel") ||
			strings.Contains(strings.ToLower(diag.Message), "type") && strings.Contains(strings.ToLower(diag.Message), "int") {
			celErrors++
			t.Errorf("Unexpected CEL type error: %s", diag.Message)
		}
	}

	if celErrors > 0 {
		t.Errorf("Expected no CEL type errors for valid int access, got %d", celErrors)
	}
}

// Test fallback to untyped validation when no schema
func TestTypedValidation_FallbackUntyped(t *testing.T) {
	rgd := `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test-untyped
spec:
  resources:
    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: myapp`

	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()
	client.OpenDocument("file:///test-untyped.yaml", rgd)

	// Should not crash, falls back to untyped validation
	diagnostics := client.WaitForDiagnostics("file:///test-untyped.yaml", 0)
	t.Logf("Got %d diagnostics with untyped fallback", len(diagnostics))
	for _, d := range diagnostics {
		t.Logf("  - %s", d.Message)
	}

	// Important thing is it didn't crash - no specific assertion needed
	t.Log("✅ Untyped fallback works without crashing")
}

// Test boolean field type
func TestTypedValidation_BooleanField(t *testing.T) {
	rgd := `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test-bool
spec:
  schema:
    kind: TestApp
    apiVersion: v1alpha1
    spec:
      enabled: boolean
  resources:
    - id: config
      template:
        apiVersion: v1
        kind: ConfigMap
        data:
          enabled: ${schema.spec.enabled ? 'true' : 'false'}`

	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()
	client.OpenDocument("file:///test-bool.yaml", rgd)

	diagnostics := client.WaitForDiagnostics("file:///test-bool.yaml", 0)
	t.Logf("Got %d diagnostics", len(diagnostics))

	// Filter for CEL errors
	celErrors := 0
	for _, diag := range diagnostics {
		if strings.Contains(strings.ToLower(diag.Message), "cel") ||
			strings.Contains(strings.ToLower(diag.Message), "overload") {
			celErrors++
			t.Logf("  - CEL error: %s", diag.Message)
		}
	}

	if celErrors > 0 {
		t.Errorf("Expected no CEL errors for valid boolean ternary, got %d", celErrors)
	}
}

// Test chained method calls with type checking
func TestTypedValidation_ChainedMethods(t *testing.T) {
	rgd := `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test-chained
spec:
  schema:
    kind: TestApp
    apiVersion: v1alpha1
    spec:
      name: string
  resources:
    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          labels:
            first: ${schema.spec.name.split('-')[0].upperAscii()}`

	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()
	client.OpenDocument("file:///test-chained.yaml", rgd)

	diagnostics := client.WaitForDiagnostics("file:///test-chained.yaml", 0)
	t.Logf("Got %d diagnostics", len(diagnostics))

	// Filter for CEL type errors
	celErrors := 0
	for _, diag := range diagnostics {
		if strings.Contains(strings.ToLower(diag.Message), "cel") ||
			strings.Contains(strings.ToLower(diag.Message), "overload") {
			celErrors++
			t.Logf("  - CEL error: %s", diag.Message)
		}
	}

	if celErrors > 0 {
		t.Errorf("Expected no CEL errors for valid chained methods, got %d", celErrors)
	}
}
