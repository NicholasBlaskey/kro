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

package analysis

import (
	"strings"
	"testing"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

// TestBuildYAMLPath unit tests the YAML path building
func TestBuildYAMLPath(t *testing.T) {
	tests := []struct {
		name        string
		document    string
		cursorLine  int
		cursorCol   int
		wantPath    string
	}{
		{
			name: "inside spec",
			document: `    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        spec:
          `,
			cursorLine: 5,
			cursorCol:  10,
			wantPath:   "spec",
		},
		{
			name: "inside spec.template",
			document: `    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        spec:
          replicas: 1
          template:
            `,
			cursorLine: 7,
			cursorCol:  12,
			wantPath:   "spec.template",
		},
		{
			name: "inside spec at start of nested",
			document: `    - id: service
      template:
        apiVersion: v1
        kind: Service
        spec:
          ports:
            - `,
			cursorLine: 6,
			cursorCol:  14,
			wantPath:   "spec.ports",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := strings.Split(tt.document, "\n")
			currentIndent := tt.cursorCol
			path := buildYAMLPath(lines, tt.cursorLine, currentIndent)
			if path != tt.wantPath {
				t.Errorf("buildYAMLPath(line=%d, col=%d) = %q; want %q", tt.cursorLine, tt.cursorCol, path, tt.wantPath)
			}
		})
	}
}

// TestAnalyzeK8sYAMLContext unit tests the K8s YAML context detection
func TestAnalyzeK8sYAMLContext(t *testing.T) {
	// Build a symbol table with a deployment resource
	st := &SymbolTable{
		Resources: map[string]*ResourceSymbol{
			"deployment": {
				ID:      "deployment",
				K8sKind: "Deployment",
			},
		},
	}

	document := `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test
spec:
  resources:
    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        spec:
          `

	// Cursor on line 11 (0-indexed), col 10
	pos := protocol.Position{Line: 11, Character: 10}
	ctx := analyzeK8sYAMLContext(document, pos, st)

	if ctx == nil {
		t.Fatal("expected non-nil context")
	}

	if ctx.Type != CompletionContextResourceField {
		t.Errorf("expected CompletionContextResourceField, got %v", ctx.Type)
	}

	expected := "deployment:Deployment:spec"
	if ctx.ResourceID != expected {
		t.Errorf("expected ResourceID %q, got %q", expected, ctx.ResourceID)
	}
}
