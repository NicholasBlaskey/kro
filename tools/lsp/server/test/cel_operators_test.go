package test

import (
	"strings"
	"testing"
	"time"
)

// TestCELOperators_Completion tests that completion works after CEL operators
func TestCELOperators_Completion(t *testing.T) {
	tests := []struct {
		name         string
		expr         string
		line         int
		char         int
		wantContains []string
		description  string
	}{
		{
			name:         "after plus operator",
			expr:         "${schema.spec.name+schema.spec.",
			line:         18,
			char:         46,
			wantContains: []string{"name", "replicas", "fullDNSName"},
			description:  "Should complete second schema.spec after +",
		},
		{
			name:         "after minus operator",
			expr:         "${schema.spec.replicas-deployment.spec.",
			line:         18,
			char:         54,
			wantContains: []string{"replicas", "selector", "template"},
			description:  "Should complete deployment.spec after -",
		},
		{
			name:         "after multiply operator",
			expr:         "${schema.spec.replicas*schema.spec.",
			line:         18,
			char:         50,
			wantContains: []string{"name", "replicas", "fullDNSName"},
			description:  "Should complete schema.spec after *",
		},
		{
			name:         "after comparison operator",
			expr:         "${schema.spec.replicas>5?schema.spec.",
			line:         18,
			char:         52,
			wantContains: []string{"name", "replicas", "fullDNSName"},
			description:  "Should complete schema.spec in ternary true branch",
		},
		{
			name:         "after colon in ternary",
			expr:         "${schema.spec.replicas>5?1:schema.spec.",
			line:         18,
			char:         54,
			wantContains: []string{"name", "replicas", "fullDNSName"},
			description:  "Should complete schema.spec in ternary false branch",
		},
		{
			name:         "after logical AND",
			expr:         "${schema.spec.replicas>0&&deployment.spec.",
			line:         18,
			char:         58,
			wantContains: []string{"replicas", "selector", "template"},
			description:  "Should complete deployment.spec after &&",
		},
		{
			name:         "after logical OR",
			expr:         "${schema.spec.replicas>0||deployment.spec.",
			line:         18,
			char:         58,
			wantContains: []string{"replicas", "selector", "template"},
			description:  "Should complete deployment.spec after ||",
		},
		{
			name:         "in parentheses",
			expr:         "${(schema.spec.",
			line:         18,
			char:         30,
			wantContains: []string{"name", "replicas", "fullDNSName"},
			description:  "Should complete schema.spec inside parentheses",
		},
		{
			name:         "after comma in function call",
			expr:         "${hash(schema.spec.name)+schema.spec.",
			line:         18,
			char:         52,
			wantContains: []string{"name", "replicas", "fullDNSName"},
			description:  "Should complete after function call",
		},
		{
			name:         "nested operators",
			expr:         "${schema.spec.name+\"-\"+deployment.spec.",
			line:         18,
			char:         54,
			wantContains: []string{"replicas", "selector", "template"},
			description:  "Should complete with nested string concatenation",
		},
		{
			name:         "after equality check",
			expr:         "${schema.spec.name==\"test\"&&deployment.spec.",
			line:         18,
			char:         61,
			wantContains: []string{"replicas", "selector", "template"},
			description:  "Should complete after == operator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-operators.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			items := client.RequestCompletion("file:///tmp/test-operators.yaml", tt.line, tt.char, 1, "")

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

// TestCELOperators_ResourceCompletion tests that we complete resource names after operators
func TestCELOperators_ResourceCompletion(t *testing.T) {
	tests := []struct {
		name         string
		expr         string
		line         int
		char         int
		wantContains []string
		description  string
	}{
		{
			name:         "resource after plus",
			expr:         "${schema.spec.name+dep",
			line:         18,
			char:         38,
			wantContains: []string{"deployment"},
			description:  "Should complete deployment after +",
		},
		{
			name:         "resource after comparison",
			expr:         "${schema.spec.replicas>0?dep",
			line:         18,
			char:         44,
			wantContains: []string{"deployment"},
			description:  "Should complete deployment in ternary",
		},
		{
			name:         "resource after AND",
			expr:         "${schema.spec.replicas>0&&ser",
			line:         18,
			char:         45,
			wantContains: []string{"service"},
			description:  "Should complete service after &&",
		},
		{
			name:         "resource in function",
			expr:         "${hash(sch",
			line:         18,
			char:         25,
			wantContains: []string{"schema"},
			description:  "Should complete schema inside function",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-resource-ops.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			items := client.RequestCompletion("file:///tmp/test-resource-ops.yaml", tt.line, tt.char, 1, "")

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

// TestCELOperators_StringConcatenation tests completion in complex string concatenation
func TestCELOperators_StringConcatenation(t *testing.T) {
	tests := []struct {
		name         string
		expr         string
		line         int
		char         int
		wantContains []string
	}{
		{
			name:         "string literal concatenation",
			expr:         `${"prefix-"+schema.spec.`,
			line:         18,
			char:         39,
			wantContains: []string{"name", "replicas", "fullDNSName"},
		},
		{
			name:         "multiple concatenations",
			expr:         `${schema.spec.name+"-"+deployment.spec.`,
			line:         18,
			char:         54,
			wantContains: []string{"replicas", "selector", "template"},
		},
		{
			name:         "nested quotes",
			expr:         `${schema.spec.name+"suffix"+deployment.spec.`,
			line:         18,
			char:         61,
			wantContains: []string{"replicas", "selector", "template"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-concat.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			items := client.RequestCompletion("file:///tmp/test-concat.yaml", tt.line, tt.char, 1, "")

			for _, want := range tt.wantContains {
				assertContains(t, items, want, tt.name+" should have "+want)
			}
		})
	}
}
