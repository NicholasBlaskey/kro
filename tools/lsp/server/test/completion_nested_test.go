package test

import (
	"strings"
	"testing"
	"time"
)

// testRGDWithSchema is a more complete RGD with schema fields for testing nested completions
const testRGDWithSchema = `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: testapp
spec:
  schema:
    kind: TestApp
    apiVersion: v1alpha1
    spec:
      name: string
      replicas: integer
      fullDNSName: string
  resources:
    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: CURSOR_HERE
    - id: service
      template:
        apiVersion: v1
        kind: Service
        metadata:
          name: service-name`

// TestCompletion_NestedSchema tests completion at various nesting levels
func TestCompletion_NestedSchema(t *testing.T) {
	tests := []struct {
		name           string
		cursorText     string
		line           int
		char           int
		wantContains   []string
		wantNotContain []string
		wantCount      *int // If set, expect exactly this many items
	}{
		{
			name:         "schema root",
			cursorText:   "${schema",
			line:         18,
			char:         22,
			wantContains: []string{"schema"},
			wantCount:    intPtr(1),
		},
		{
			name:         "schema dot",
			cursorText:   "${schema.",
			line:         18,
			char:         24,
			wantContains: []string{"spec"},
			wantCount:    intPtr(1),
		},
		{
			name:         "schema.spec dot",
			cursorText:   "${schema.spec.",
			line:         18,
			char:         29,
			wantContains: []string{"name", "replicas", "fullDNSName"},
			// Should NOT contain metadata, spec, status (those are for resources, not schema fields)
			wantNotContain: []string{"metadata", "status"},
		},
		{
			name:         "schema.spec.name dot (leaf field)",
			cursorText:   "${schema.spec.name.",
			line:         18,
			char:         34,
			wantContains: []string{}, // name is a string, no fields
			wantCount:    intPtr(0),
		},
		{
			name:         "schema.spec.fullDNSName dot (leaf field)",
			cursorText:   "${schema.spec.fullDNSName.",
			line:         18,
			char:         41,
			wantContains: []string{}, // fullDNSName is a string, no fields
			// Should NOT suggest metadata, spec, status
			wantNotContain: []string{"metadata", "spec", "status"},
			wantCount:      intPtr(0),
		},
		{
			name:         "resource root",
			cursorText:   "${dep",
			line:         18,
			char:         20,
			wantContains: []string{"deployment"},
		},
		{
			name:         "resource.metadata",
			cursorText:   "${deployment.",
			line:         18,
			char:         28,
			wantContains: []string{"metadata", "spec", "status"},
		},
		{
			name:           "resource.metadata.name (nested resource field)",
			cursorText:     "${deployment.metadata.",
			line:           18,
			char:           37,
			wantContains:   []string{}, // TODO: Could add metadata subfields later
			wantNotContain: []string{"name", "replicas", "fullDNSName"}, // Schema fields shouldn't appear here
		},
		{
			name:         "partial schema.spec field",
			cursorText:   "${schema.spec.ful",
			line:         18,
			char:         32,
			wantContains: []string{"fullDNSName"},
			// Only fields starting with "ful"
			wantNotContain: []string{"name", "replicas"},
		},
		{
			name:         "all resources at root",
			cursorText:   "${",
			line:         18,
			char:         17,
			wantContains: []string{"schema", "self", "deployment", "service"},
		},
		{
			name:         "partial resource",
			cursorText:   "${ser",
			line:         18,
			char:         20,
			wantContains: []string{"service"},
			// Should NOT contain deployment
			wantNotContain: []string{"deployment"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			// Open document with cursor at test position
			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.cursorText, 1)
			client.OpenDocument("file:///tmp/test-nested.yaml", doc)

			// Wait for validation
			time.Sleep(300 * time.Millisecond)

			// Request completion
			items := client.RequestCompletion("file:///tmp/test-nested.yaml", tt.line, tt.char, 1, "")

			// Check expected items are present
			for _, expected := range tt.wantContains {
				found := false
				for _, item := range items {
					if item.Label == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s: expected to find '%s' in completions, got: %v", tt.name, expected, itemLabels(items))
				}
			}

			// Check unwanted items are NOT present
			for _, unwanted := range tt.wantNotContain {
				for _, item := range items {
					if item.Label == unwanted {
						t.Errorf("%s: should NOT contain '%s', but found it in: %v", tt.name, unwanted, itemLabels(items))
					}
				}
			}

			// Check exact count if specified
			if tt.wantCount != nil {
				if len(items) != *tt.wantCount {
					t.Errorf("%s: expected exactly %d items, got %d: %v", tt.name, *tt.wantCount, len(items), itemLabels(items))
				}
			}
		})
	}
}

// TestCompletion_ResourceVsSchema ensures we don't mix up resource fields and schema fields
func TestCompletion_ResourceVsSchema(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", "${deployment.", 1)
	client.OpenDocument("file:///tmp/test-vs.yaml", doc)
	time.Sleep(300 * time.Millisecond)

	items := client.RequestCompletion("file:///tmp/test-vs.yaml", 18, 28, 1, "")

	// deployment. should suggest metadata, spec, status (Kubernetes resource fields)
	assertContains(t, items, "metadata", "Resource should have metadata field")
	assertContains(t, items, "spec", "Resource should have spec field")
	assertContains(t, items, "status", "Resource should have status field")

	// Should NOT suggest schema spec fields (name, replicas, fullDNSName)
	for _, item := range items {
		if item.Label == "name" || item.Label == "replicas" || item.Label == "fullDNSName" {
			t.Errorf("Resource completion should NOT include schema spec fields, but found: %s", item.Label)
		}
	}
}

// TestCompletion_SchemaSpecFields ensures schema.spec shows the actual spec fields
func TestCompletion_SchemaSpecFields(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", "${schema.spec.", 1)
	client.OpenDocument("file:///tmp/test-schema-spec.yaml", doc)
	time.Sleep(300 * time.Millisecond)

	items := client.RequestCompletion("file:///tmp/test-schema-spec.yaml", 18, 29, 1, "")

	// schema.spec. should suggest the actual spec fields from SimpleSchema
	assertContains(t, items, "name", "Schema spec should include 'name' field")
	assertContains(t, items, "replicas", "Schema spec should include 'replicas' field")
	assertContains(t, items, "fullDNSName", "Schema spec should include 'fullDNSName' field")

	// Should NOT suggest metadata, spec, status (those are for resources)
	for _, item := range items {
		if item.Label == "metadata" || item.Label == "spec" || item.Label == "status" {
			t.Errorf("schema.spec completion should NOT include resource fields, but found: %s", item.Label)
		}
	}
}

// TestCompletion_LeafFields ensures leaf fields don't suggest nonsense
func TestCompletion_LeafFields(t *testing.T) {
	tests := []struct {
		name       string
		cursorText string
		line       int
		char       int
	}{
		{
			name:       "schema.spec.name is a leaf",
			cursorText: "${schema.spec.name.",
			line:       18,
			char:       34,
		},
		{
			name:       "schema.spec.replicas is a leaf",
			cursorText: "${schema.spec.replicas.",
			line:       18,
			char:       38,
		},
		{
			name:       "schema.spec.fullDNSName is a leaf",
			cursorText: "${schema.spec.fullDNSName.",
			line:       18,
			char:       41,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.cursorText, 1)
			client.OpenDocument("file:///tmp/test-leaf.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			items := client.RequestCompletion("file:///tmp/test-leaf.yaml", tt.line, tt.char, 1, "")

			// Leaf fields should return NO completions
			if len(items) > 0 {
				t.Errorf("%s: leaf field should have 0 completions, got %d: %v", tt.name, len(items), itemLabels(items))
			}
		})
	}
}

// Helper to create an int pointer
func intPtr(i int) *int {
	return &i
}
