package test

import (
	"strings"
	"testing"
	"time"
)

// TestCELValidation_ValidExpressions tests that valid CEL expressions don't produce diagnostics
func TestCELValidation_ValidExpressions(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{
			name: "simple resource reference",
			expr: "${deployment}",
		},
		{
			name: "field access",
			expr: "${schema.spec.name}",
		},
		{
			name: "function call",
			expr: "${hash(schema.spec.name)}",
		},
		{
			name: "string concatenation",
			expr: `${"prefix-" + schema.spec.name}`,
		},
		{
			name: "conditional",
			expr: "${schema.spec.replicas > 1 ? true : false}",
		},
		{
			name: "size function",
			expr: "${size(schema.spec.name) > 0}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-cel-valid.yaml", doc)
			time.Sleep(500 * time.Millisecond) // Wait for validation

			// We don't have a way to fetch diagnostics from the test client yet
			// For now, just make sure the server doesn't crash
			t.Logf("Validated expression: %s", tt.expr)
		})
	}
}

// TestCELValidation_InvalidExpressions tests that invalid CEL expressions produce diagnostics
func TestCELValidation_InvalidExpressions(t *testing.T) {
	tests := []struct {
		name        string
		expr        string
		expectError string
	}{
		{
			name:        "undefined variable",
			expr:        "${undefined_var}",
			expectError: "undeclared reference",
		},
		{
			name:        "invalid field",
			expr:        "${schema.nonexistent}",
			expectError: "undefined field",
		},
		{
			name:        "type mismatch",
			expr:        `${"string" + 123}`,
			expectError: "no such overload",
		},
		{
			name:        "syntax error",
			expr:        "${schema.spec.}",
			expectError: "Syntax error",
		},
		{
			name:        "unclosed expression",
			expr:        "${schema.spec.name",
			expectError: "", // Might not parse as CEL expression at all
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-cel-invalid.yaml", doc)
			time.Sleep(500 * time.Millisecond) // Wait for validation

			// TODO: Implement diagnostic fetching in test client
			t.Logf("Tested invalid expression: %s (expected error: %s)", tt.expr, tt.expectError)
		})
	}
}

// TestCELCompletion_Functions tests that CEL functions are completed
func TestCELCompletion_Functions(t *testing.T) {
	tests := []struct {
		name         string
		expr         string
		line         int
		char         int
		wantContains []string
	}{
		{
			name:         "hash function",
			expr:         "${has",
			line:         18,
			char:         20,
			wantContains: []string{"hash", "has"},
		},
		{
			name:         "size function",
			expr:         "${siz",
			line:         18,
			char:         20,
			wantContains: []string{"size"},
		},
		{
			name:         "omit function (Kro-specific)",
			expr:         "${omi",
			line:         18,
			char:         20,
			wantContains: []string{"omit"},
		},
		{
			name:         "all functions with no prefix",
			expr:         "${",
			line:         18,
			char:         17,
			wantContains: []string{"hash", "omit", "size", "matches", "schema", "deployment"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-cel-completion.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			items := client.RequestCompletion("file:///tmp/test-cel-completion.yaml", tt.line, tt.char, 1, "")

			for _, want := range tt.wantContains {
				found := false
				for _, item := range items {
					if item.Label == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s: expected to find '%s' in completions, got: %v", tt.name, want, itemLabels(items))
				}
			}
		})
	}
}

// TestCELCompletion_StringTemplates tests completion in string templates with multiple CEL expressions
func TestCELCompletion_StringTemplates(t *testing.T) {
	tests := []struct {
		name         string
		template     string
		line         int
		char         int
		wantContains []string
		description  string
	}{
		{
			name:         "first expression in template",
			template:     "x-${schema.spec.name}-y",
			line:         18,
			char:         30, // After "name"
			wantContains: []string{}, // Leaf field, no completions
			description:  "First CEL expression in string template",
		},
		{
			name:         "second expression in template",
			template:     "x-${schema.spec.name}-${deployment.",
			line:         18,
			char:         51, // After "deployment."
			wantContains: []string{"metadata", "spec", "status"},
			description:  "Second CEL expression in same string template",
		},
		{
			name:         "multiple expressions - first",
			template:     "prefix-${sch",
			line:         18,
			char:         28, // After "sch"
			wantContains: []string{"schema"},
			description:  "Partial 'schema' in first position",
		},
		{
			name:         "multiple expressions - middle",
			template:     "a-${schema.spec.name}-b-${dep",
			line:         18,
			char:         45, // After "dep"
			wantContains: []string{"deployment"},
			description:  "Partial 'deployment' in middle of template",
		},
		{
			name:         "complex template with functions",
			template:     "hash-${hash(schema.spec.name)}-name-${schema.spec.",
			line:         18,
			char:         66, // After "schema.spec."
			wantContains: []string{"name", "replicas", "fullDNSName"},
			description:  "Function in first expr, field completion in second",
		},
		{
			name:         "nested template expressions",
			template:     `prefix-${schema.spec.name + "-" + schema.spec.replicas}`,
			line:         18,
			char:         29, // After first "name"
			wantContains: []string{}, // Leaf field
			description:  "Completion within complex concatenation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.template, 1)
			client.OpenDocument("file:///tmp/test-template.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			items := client.RequestCompletion("file:///tmp/test-template.yaml", tt.line, tt.char, 1, "")

			t.Logf("%s: %s - got %d items: %v", tt.name, tt.description, len(items), itemLabels(items))

			for _, want := range tt.wantContains {
				found := false
				for _, item := range items {
					if item.Label == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s: expected to find '%s' in completions, got: %v", tt.name, want, itemLabels(items))
				}
			}
		})
	}
}

// TestCELCompletion_AfterDot tests that completion works after a dot in CEL expressions
func TestCELCompletion_AfterDot(t *testing.T) {
	tests := []struct {
		name         string
		expr         string
		line         int
		char         int
		wantContains []string
	}{
		{
			name:         "schema dot",
			expr:         "${schema.",
			line:         18,
			char:         24,
			wantContains: []string{"spec"},
		},
		{
			name:         "schema.spec dot",
			expr:         "${schema.spec.",
			line:         18,
			char:         29,
			wantContains: []string{"name", "replicas", "fullDNSName"},
		},
		{
			name:         "deployment dot",
			expr:         "${deployment.",
			line:         18,
			char:         28,
			wantContains: []string{"metadata", "spec", "status"},
		},
		{
			name:         "service.spec dot",
			expr:         "${service.spec.",
			line:         18,
			char:         30,
			wantContains: []string{"type", "selector", "ports", "clusterIP"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-cel-dot.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			items := client.RequestCompletion("file:///tmp/test-cel-dot.yaml", tt.line, tt.char, 2, ".")

			for _, want := range tt.wantContains {
				found := false
				for _, item := range items {
					if item.Label == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s: expected to find '%s' in completions, got: %v", tt.name, want, itemLabels(items))
				}
			}
		})
	}
}

// TestCELCompletion_KroFunctions tests Kro-specific CEL functions
func TestCELCompletion_KroFunctions(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", "${", 1)
	client.OpenDocument("file:///tmp/test-kro-functions.yaml", doc)
	time.Sleep(300 * time.Millisecond)

	items := client.RequestCompletion("file:///tmp/test-kro-functions.yaml", 18, 17, 1, "")

	// Should contain Kro-specific functions
	kroFunctions := []string{"hash", "omit"}
	for _, fn := range kroFunctions {
		found := false
		for _, item := range items {
			if item.Label == fn {
				found = true
				// Should have high priority (sortText starting with "0_")
				if item.SortText != "" && !strings.HasPrefix(item.SortText, "0_") {
					t.Errorf("Kro function '%s' should have high priority, got sortText: %s", fn, item.SortText)
				}
				break
			}
		}
		if !found {
			t.Errorf("Expected to find Kro function '%s' in completions", fn)
		}
	}
}
