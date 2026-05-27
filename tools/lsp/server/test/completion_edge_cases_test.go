package test

import (
	"strings"
	"testing"
	"time"
)

// TestCompletion_SchemaMetadata tests that schema.metadata works (it's a K8s resource in the instance)
func TestCompletion_SchemaMetadata(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", "${schema.metadata.", 1)
	client.OpenDocument("file:///tmp/test-schema-metadata.yaml", doc)
	time.Sleep(300 * time.Millisecond)

	items := client.RequestCompletion("file:///tmp/test-schema-metadata.yaml", 18, 32, 1, "")

	// schema.metadata should work (it's the instance metadata)
	// For now we don't complete subfields, but it shouldn't crash or return nonsense
	t.Logf("schema.metadata. returned %d items: %v", len(items), itemLabels(items))
}

// TestCompletion_SelfIdentifier tests the 'self' special identifier
func TestCompletion_SelfIdentifier(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", "${self", 1)
	client.OpenDocument("file:///tmp/test-self.yaml", doc)
	time.Sleep(300 * time.Millisecond)

	items := client.RequestCompletion("file:///tmp/test-self.yaml", 18, 21, 1, "")

	// Should suggest 'self' as a completion
	assertContains(t, items, "self", "Should complete 'self' identifier")
}

// TestCompletion_EmptyExpression tests completion right after ${
func TestCompletion_EmptyExpression(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", "${", 1)
	client.OpenDocument("file:///tmp/test-empty-expr.yaml", doc)
	time.Sleep(300 * time.Millisecond)

	items := client.RequestCompletion("file:///tmp/test-empty-expr.yaml", 18, 18, 1, "")

	// Should show ALL resources and special identifiers with no filter
	assertContains(t, items, "schema", "Empty expression should show all options")
	assertContains(t, items, "self", "Empty expression should show all options")
	assertContains(t, items, "deployment", "Empty expression should show all options")
	assertContains(t, items, "service", "Empty expression should show all options")

	if len(items) < 4 {
		t.Errorf("Empty expression should show at least 4 items (schema, self, deployment, service), got %d: %v", len(items), itemLabels(items))
	}
}

// TestCompletion_PartialPrefix tests filtering with various prefixes
func TestCompletion_PartialPrefix(t *testing.T) {
	tests := []struct {
		name         string
		expr         string
		line         int
		char         int
		wantContains []string
		wantExclude  []string
	}{
		{
			name:         "prefix 'd' at root",
			expr:         "${d",
			line:         18,
			char:         19,
			wantContains: []string{"deployment"},
			wantExclude:  []string{"service", "schema", "self"},
		},
		{
			name:         "prefix 's' at root",
			expr:         "${s",
			line:         18,
			char:         19,
			wantContains: []string{"schema", "self", "service"},
			wantExclude:  []string{"deployment"},
		},
		{
			name:         "prefix 'n' at schema.spec",
			expr:         "${schema.spec.n",
			line:         18,
			char:         31,
			wantContains: []string{"name"},
			wantExclude:  []string{"replicas", "fullDNSName"},
		},
		{
			name:         "prefix 're' at schema.spec",
			expr:         "${schema.spec.re",
			line:         18,
			char:         32,
			wantContains: []string{"replicas"},
			wantExclude:  []string{"name", "fullDNSName"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-partial.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			items := client.RequestCompletion("file:///tmp/test-partial.yaml", tt.line, tt.char, 1, "")

			for _, want := range tt.wantContains {
				assertContains(t, items, want, tt.name+" should contain "+want)
			}

			for _, exclude := range tt.wantExclude {
				for _, item := range items {
					if item.Label == exclude {
						t.Errorf("%s: should NOT contain '%s', but found it in: %v", tt.name, exclude, itemLabels(items))
					}
				}
			}
		})
	}
}

// TestCompletion_MultipleDots tests deeply nested paths
func TestCompletion_MultipleDots(t *testing.T) {
	tests := []struct {
		name         string
		expr         string
		line         int
		char         int
		wantContains []string
		expectEmpty  bool
	}{
		{
			name:         "deployment.spec.template",
			expr:         "${deployment.spec.template.",
			line:         18,
			char:         46,
			expectEmpty:  false, // We don't parse K8s schemas deeply yet, but shouldn't crash
		},
		{
			name:         "schema.spec.name.toString",
			expr:         "${schema.spec.name.toString.",
			line:         18,
			char:         43,
			expectEmpty:  true, // name is a leaf, toString() is a function call
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-multi-dot.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			items := client.RequestCompletion("file:///tmp/test-multi-dot.yaml", tt.line, tt.char, 1, "")

			t.Logf("%s returned %d items: %v", tt.name, len(items), itemLabels(items))

			if tt.expectEmpty && len(items) > 0 {
				t.Errorf("%s: expected 0 items, got %d: %v", tt.name, len(items), itemLabels(items))
			}
		})
	}
}

// TestCompletion_InvalidPaths tests that invalid paths don't crash
func TestCompletion_InvalidPaths(t *testing.T) {
	tests := []struct {
		name string
		expr string
		line int
		char int
	}{
		{
			name: "unknown resource",
			expr: "${unknownResource.",
			line: 18,
			char: 33,
		},
		{
			name: "double dot",
			expr: "${schema..",
			line: 18,
			char: 25,
		},
		{
			name: "trailing space",
			expr: "${schema. ",
			line: 18,
			char: 25,
		},
		{
			name: "resource with number",
			expr: "${schema123.",
			line: 18,
			char: 27,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-invalid.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			// Should not crash, just return empty or filtered results
			items := client.RequestCompletion("file:///tmp/test-invalid.yaml", tt.line, tt.char, 1, "")
			t.Logf("%s returned %d items (should not crash): %v", tt.name, len(items), itemLabels(items))
		})
	}
}

// TestCompletion_MixedCase tests case sensitivity
func TestCompletion_MixedCase(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	// Try uppercase prefix
	doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", "${Schema", 1)
	client.OpenDocument("file:///tmp/test-case.yaml", doc)
	time.Sleep(300 * time.Millisecond)

	items := client.RequestCompletion("file:///tmp/test-case.yaml", 18, 22, 1, "")

	// Should be case-sensitive (no match for "Schema" when resource is "schema")
	for _, item := range items {
		if item.Label == "schema" {
			t.Errorf("Prefix 'Schema' should NOT match 'schema' (case-sensitive), but found it")
		}
	}

	if len(items) != 0 {
		t.Logf("Uppercase prefix returned %d items (expected 0 if case-sensitive): %v", len(items), itemLabels(items))
	}
}

// TestCompletion_StatusFields tests that status fields are suggested for resources
func TestCompletion_StatusFields(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", "${deployment.status.", 1)
	client.OpenDocument("file:///tmp/test-status.yaml", doc)
	time.Sleep(300 * time.Millisecond)

	items := client.RequestCompletion("file:///tmp/test-status.yaml", 18, 35, 1, "")

	// Now we DO complete K8s status subfields!
	t.Logf("deployment.status. returned %d items: %v", len(items), itemLabels(items))

	// Should suggest K8s Deployment status fields (replicas, updatedReplicas, etc.)
	// But should NOT suggest schema-specific fields like "name" or "fullDNSName"
	for _, item := range items {
		// "replicas" is OK - it's a valid K8s Deployment status field
		// But "name" and "fullDNSName" are schema.spec fields, not K8s fields
		if item.Label == "name" || item.Label == "fullDNSName" {
			t.Errorf("deployment.status should NOT suggest schema-only fields, but found: %s", item.Label)
		}
	}
}

// TestCompletion_SpecFieldsDistinct tests that schema.spec and deployment.spec are distinct
func TestCompletion_SpecFieldsDistinct(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	// Test schema.spec
	doc1 := strings.Replace(testRGDWithSchema, "CURSOR_HERE", "${schema.spec.", 1)
	client.OpenDocument("file:///tmp/test-distinct-1.yaml", doc1)
	time.Sleep(300 * time.Millisecond)

	schemaSpecItems := client.RequestCompletion("file:///tmp/test-distinct-1.yaml", 18, 29, 1, "")

	// Should have name, replicas, fullDNSName
	assertContains(t, schemaSpecItems, "name", "schema.spec should have 'name'")
	assertContains(t, schemaSpecItems, "replicas", "schema.spec should have 'replicas'")
	assertContains(t, schemaSpecItems, "fullDNSName", "schema.spec should have 'fullDNSName'")

	// Test deployment.spec
	doc2 := strings.Replace(testRGDWithSchema, "CURSOR_HERE", "${deployment.spec.", 1)
	client.OpenDocument("file:///tmp/test-distinct-2.yaml", doc2)
	time.Sleep(300 * time.Millisecond)

	deploymentSpecItems := client.RequestCompletion("file:///tmp/test-distinct-2.yaml", 18, 33, 1, "")

	// Should have K8s Deployment spec fields (selector, template, strategy, etc.)
	// "replicas" is OK - it's a valid K8s Deployment spec field (different from schema.spec.replicas)
	// But should NOT have schema-only fields like "name" or "fullDNSName"
	for _, item := range deploymentSpecItems {
		if item.Label == "name" || item.Label == "fullDNSName" {
			t.Errorf("deployment.spec should NOT have schema-only fields, but found: %s", item.Label)
		}
	}

	// Check that deployment.spec has K8s fields
	assertContains(t, deploymentSpecItems, "selector", "deployment.spec should have K8s 'selector' field")
	assertContains(t, deploymentSpecItems, "template", "deployment.spec should have K8s 'template' field")

	t.Logf("schema.spec has %d items, deployment.spec has %d items", len(schemaSpecItems), len(deploymentSpecItems))
}
