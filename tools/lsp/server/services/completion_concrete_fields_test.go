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
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestConcreteFieldCompletion(t *testing.T) {
	// Create a symbol table with a deployment resource that has concrete fields
	symbolTable := analysis.NewSymbolTable()
	symbolTable.Resources = map[string]*analysis.ResourceSymbol{
		"deployment": {
			ID:         "deployment",
			K8sKind:    "Deployment",
			ArrayIndex: 0,
			ConcreteFields: map[string][]string{
				"metadata.labels": {"app", "dns", "partials", "first_part"},
				"spec.selector.matchLabels": {"app"},
				"spec.template.metadata.labels": {"app", "version"},
			},
		},
		"service": {
			ID:         "service",
			K8sKind:    "Service",
			ArrayIndex: 1,
			ConcreteFields: map[string][]string{
				"metadata.labels": {"app", "env", "custom"},
				"spec.selector": {"app"},
			},
		},
	}

	// Create completion provider
	provider := NewCompletionProvider(symbolTable, nil)

	tests := []struct {
		name           string
		fieldPath      string
		prefix         string
		expectField    string // One field we expect to find
		expectNotField string // One field we expect NOT to find (to ensure filtering)
	}{
		{
			name:           "deployment.metadata.labels shows concrete keys",
			fieldPath:      "deployment.metadata.labels",
			prefix:         "",
			expectField:    "app",
			expectNotField: "nonexistent",
		},
		{
			name:           "deployment.metadata.labels shows all concrete keys",
			fieldPath:      "deployment.metadata.labels",
			prefix:         "",
			expectField:    "dns",
			expectNotField: "",
		},
		{
			name:           "deployment.metadata.labels with prefix filters",
			fieldPath:      "deployment.metadata.labels",
			prefix:         "d",
			expectField:    "dns",
			expectNotField: "app", // Filtered out by prefix
		},
		{
			name:           "deployment.metadata.labels with prefix 'a' shows app",
			fieldPath:      "deployment.metadata.labels",
			prefix:         "a",
			expectField:    "app",
			expectNotField: "dns", // Filtered out by prefix
		},
		{
			name:           "deployment.spec.selector.matchLabels shows nested concrete keys",
			fieldPath:      "deployment.spec.selector.matchLabels",
			prefix:         "",
			expectField:    "app",
			expectNotField: "dns", // Not in this path
		},
		{
			name:           "service.metadata.labels shows different concrete keys",
			fieldPath:      "service.metadata.labels",
			prefix:         "",
			expectField:    "env",
			expectNotField: "dns", // Belongs to deployment, not service
		},
		{
			name:           "service.metadata.labels shows custom key",
			fieldPath:      "service.metadata.labels",
			prefix:         "c",
			expectField:    "custom",
			expectNotField: "app", // Filtered by prefix
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			position := protocol.Position{Line: 0, Character: 0}
			items := provider.completeFields(tt.fieldPath, tt.prefix, position)

			// Check for expected field
			foundExpected := false
			foundNotExpected := false
			for _, item := range items {
				if item.Label == tt.expectField {
					foundExpected = true
				}
				if tt.expectNotField != "" && item.Label == tt.expectNotField {
					foundNotExpected = true
				}
			}

			if !foundExpected {
				labels := make([]string, len(items))
				for i, item := range items {
					labels[i] = item.Label
				}
				t.Errorf("Expected to find field %q in completions, got: %v", tt.expectField, labels)
			}

			if tt.expectNotField != "" && foundNotExpected {
				t.Errorf("Expected NOT to find field %q in completions, but it was present", tt.expectNotField)
			}
		})
	}
}

func TestConcreteFieldsWithMethods(t *testing.T) {
	// Test that concrete fields are shown TOGETHER with type-aware methods
	// For maps, we should see both map methods (merge, size) AND concrete keys
	symbolTable := analysis.NewSymbolTable()
	symbolTable.Resources = map[string]*analysis.ResourceSymbol{
		"deployment": {
			ID:         "deployment",
			K8sKind:    "Deployment",
			ArrayIndex: 0,
			ConcreteFields: map[string][]string{
				"metadata.labels": {"app", "dns", "env"},
			},
		},
	}

	provider := NewCompletionProvider(symbolTable, nil)
	position := protocol.Position{Line: 0, Character: 0}

	items := provider.completeFields("deployment.metadata.labels", "", position)

	// Should have concrete fields
	hasApp := false
	hasDns := false

	// Should also have map methods
	hasMerge := false
	hasSize := false

	for _, item := range items {
		switch item.Label {
		case "app":
			hasApp = true
		case "dns":
			hasDns = true
		case "merge":
			hasMerge = true
		case "size":
			hasSize = true
		}
	}

	if !hasApp {
		t.Error("Expected concrete field 'app' in completions")
	}
	if !hasDns {
		t.Error("Expected concrete field 'dns' in completions")
	}
	if !hasMerge {
		t.Error("Expected map method 'merge' in completions")
	}
	if !hasSize {
		t.Error("Expected map method 'size' in completions")
	}
}

func TestConcreteFieldsVsK8sSchema(t *testing.T) {
	// Test that concrete fields take priority and are shown alongside k8s schema fields
	symbolTable := analysis.NewSymbolTable()
	symbolTable.Resources = map[string]*analysis.ResourceSymbol{
		"deployment": {
			ID:         "deployment",
			K8sKind:    "Deployment",
			ArrayIndex: 0,
			ConcreteFields: map[string][]string{
				"metadata.labels": {"app", "custom_key", "another_key"},
			},
		},
	}

	provider := NewCompletionProvider(symbolTable, nil)
	position := protocol.Position{Line: 0, Character: 0}

	items := provider.completeFields("deployment.metadata.labels", "", position)

	// Concrete fields should be present
	hasApp := false
	hasCustomKey := false

	for _, item := range items {
		if item.Label == "app" {
			hasApp = true
			// Check that it's marked as from template
			if item.Detail == nil || *item.Detail != "Defined in deployment template" {
				t.Errorf("Concrete field 'app' should have detail 'Defined in deployment template', got: %v", item.Detail)
			}
		}
		if item.Label == "custom_key" {
			hasCustomKey = true
		}
	}

	if !hasApp {
		t.Error("Expected concrete field 'app' in completions")
	}
	if !hasCustomKey {
		t.Error("Expected concrete field 'custom_key' in completions")
	}
}

func TestStringFieldsNoConcreteKeys(t *testing.T) {
	// Test that string fields (like metadata.name) show string methods, not concrete keys
	symbolTable := analysis.NewSymbolTable()
	symbolTable.Resources = map[string]*analysis.ResourceSymbol{
		"deployment": {
			ID:         "deployment",
			K8sKind:    "Deployment",
			ArrayIndex: 0,
			ConcreteFields: map[string][]string{
				"metadata.labels": {"app", "dns"},
				// metadata.name is NOT in concrete fields (it's a string leaf)
			},
		},
	}

	provider := NewCompletionProvider(symbolTable, nil)
	position := protocol.Position{Line: 0, Character: 0}

	items := provider.completeFields("deployment.metadata.name", "", position)

	// Should have string methods
	hasSplit := false
	hasContains := false

	// Should NOT have concrete field keys
	hasApp := false

	for _, item := range items {
		switch item.Label {
		case "split":
			hasSplit = true
		case "contains":
			hasContains = true
		case "app":
			hasApp = true
		}
	}

	if !hasSplit {
		t.Error("Expected string method 'split' for metadata.name")
	}
	if !hasContains {
		t.Error("Expected string method 'contains' for metadata.name")
	}
	if hasApp {
		t.Error("Should NOT have concrete field 'app' for metadata.name (it's a string, not a map)")
	}
}
