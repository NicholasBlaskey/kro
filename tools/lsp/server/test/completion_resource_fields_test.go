package test

import (
	"strings"
	"testing"
	"time"
)

// TestCompletion_ResourceFieldDepth tests that we don't suggest infinite nesting for resource fields
func TestCompletion_ResourceFieldDepth(t *testing.T) {
	tests := []struct {
		name        string
		expr        string
		line        int
		char        int
		wantCount   int // Expected number of completions
		description string
	}{
		{
			name:        "deployment root",
			expr:        "${deployment.",
			line:        18,
			char:        28,
			wantCount:   3, // metadata, spec, status
			description: "Root resource should show metadata, spec, status",
		},
		{
			name:        "deployment.metadata nested",
			expr:        "${deployment.metadata.",
			line:        18,
			char:        37,
			wantCount:   0, // No K8s metadata schema (yet)
			description: "deployment.metadata should show no completions (no metadata schema)",
		},
		{
			name:        "deployment.spec nested",
			expr:        "${deployment.spec.",
			line:        18,
			char:        33,
			wantCount:   8, // Should show K8s Deployment spec fields
			description: "deployment.spec should show K8s Deployment spec fields",
		},
		{
			name:        "deployment.status nested",
			expr:        "${deployment.status.",
			line:        18,
			char:        35,
			wantCount:   8, // Should show K8s Deployment status fields
			description: "deployment.status should show K8s Deployment status fields",
		},
		{
			name:        "deployment.spec.replicas (leaf field)",
			expr:        "${deployment.spec.replicas.",
			line:        18,
			char:        42,
			wantCount:   0,
			description: "deployment.spec.replicas is a leaf field (no subfields)",
		},
		{
			name:        "service root",
			expr:        "${service.",
			line:        18,
			char:        25,
			wantCount:   3, // metadata, spec, status
			description: "Root resource should show metadata, spec, status",
		},
		{
			name:        "service.spec nested",
			expr:        "${service.spec.",
			line:        18,
			char:        30,
			wantCount:   13, // Should show K8s Service spec fields
			description: "service.spec should show K8s Service spec fields",
		},
		{
			name:        "service.spec.ports (nested array)",
			expr:        "${service.spec.ports.",
			line:        18,
			char:        36,
			wantCount:   5, // Should show Service port fields (name, protocol, port, targetPort, nodePort)
			description: "service.spec.ports should show K8s Service port fields",
		},
		{
			name:        "service.spec.type (leaf field)",
			expr:        "${service.spec.type.",
			line:        18,
			char:        35,
			wantCount:   0,
			description: "service.spec.type is a leaf field (no subfields)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-resource-depth.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			items := client.RequestCompletion("file:///tmp/test-resource-depth.yaml", tt.line, tt.char, 1, "")

			if len(items) != tt.wantCount {
				t.Errorf("%s: %s\nExpected %d items, got %d: %v",
					tt.name, tt.description, tt.wantCount, len(items), itemLabels(items))
			}
		})
	}
}

// TestCompletion_CrossResourceReferences tests referencing one resource from another
func TestCompletion_CrossResourceReferences(t *testing.T) {
	tests := []struct {
		name         string
		expr         string
		line         int
		char         int
		wantContains []string
		wantCount    int
		description  string
	}{
		{
			name:         "reference service from deployment",
			expr:         "${service.",
			line:         18,
			char:         25,
			wantContains: []string{"metadata", "spec", "status"},
			wantCount:    3,
			description:  "Should suggest standard K8s fields for service resource",
		},
		{
			name:        "service.spec shows K8s fields",
			expr:        "${service.spec.",
			line:        18,
			char:        30,
			wantCount:   13,
			description: "service.spec should show K8s Service spec fields",
		},
		{
			name:         "reference deployment from service",
			expr:         "${deployment.",
			line:         18,
			char:         28,
			wantContains: []string{"metadata", "spec", "status"},
			wantCount:    3,
			description:  "Should suggest standard K8s fields for deployment resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-cross-ref.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			items := client.RequestCompletion("file:///tmp/test-cross-ref.yaml", tt.line, tt.char, 1, "")

			if len(items) != tt.wantCount {
				t.Errorf("%s: %s\nExpected %d items, got %d: %v",
					tt.name, tt.description, tt.wantCount, len(items), itemLabels(items))
			}

			for _, want := range tt.wantContains {
				found := false
				for _, item := range items {
					if item.Label == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s: expected to find '%s' in completions, got: %v",
						tt.name, want, itemLabels(items))
				}
			}
		})
	}
}

// TestCompletion_SchemaVsResource ensures schema fields and resource fields remain distinct
func TestCompletion_SchemaVsResource(t *testing.T) {
	tests := []struct {
		name            string
		expr            string
		line            int
		char            int
		wantContains    []string
		wantNotContain  []string
		description     string
	}{
		{
			name:           "schema.spec shows SimpleSchema fields",
			expr:           "${schema.spec.",
			line:           18,
			char:           29,
			wantContains:   []string{"name", "replicas", "fullDNSName"},
			wantNotContain: []string{"metadata", "spec", "status"},
			description:    "schema.spec should show actual SimpleSchema fields, not K8s fields",
		},
		{
			name:           "deployment.spec shows K8s fields, NOT SimpleSchema fields",
			expr:           "${deployment.spec.",
			line:           18,
			char:           33,
			wantContains:   []string{"selector", "template", "strategy"}, // K8s Deployment spec fields
			wantNotContain: []string{"name", "fullDNSName"}, // SimpleSchema fields shouldn't appear
			description:    "deployment.spec should show K8s Deployment fields, not schema fields",
		},
		{
			name:           "service.spec shows K8s fields, NOT SimpleSchema fields",
			expr:           "${service.spec.",
			line:           18,
			char:           30,
			wantContains:   []string{"type", "selector", "ports", "clusterIP"}, // K8s Service spec fields
			wantNotContain: []string{"name", "fullDNSName"}, // SimpleSchema fields shouldn't appear
			description:    "service.spec should show K8s Service fields, not schema fields",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-schema-vs-resource.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			items := client.RequestCompletion("file:///tmp/test-schema-vs-resource.yaml", tt.line, tt.char, 1, "")

			for _, want := range tt.wantContains {
				found := false
				for _, item := range items {
					if item.Label == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s: %s\nExpected to find '%s' in completions, got: %v",
						tt.name, tt.description, want, itemLabels(items))
				}
			}

			for _, unwanted := range tt.wantNotContain {
				for _, item := range items {
					if item.Label == unwanted {
						t.Errorf("%s: %s\nShould NOT contain '%s', but found it in: %v",
							tt.name, tt.description, unwanted, itemLabels(items))
					}
				}
			}
		})
	}
}
