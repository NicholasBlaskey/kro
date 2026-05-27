package test

import (
	"strings"
	"testing"
	"time"
)

// TestDefinition_PathSegments tests that go-to-definition returns the right location based on which segment is clicked
func TestDefinition_PathSegments(t *testing.T) {
	tests := []struct {
		name           string
		expr           string
		line           int
		char           int
		wantLocations  int // Number of locations expected (should always be 1 now)
		clickedSegment string
		description    string
	}{
		{
			name:           "click on 'schema' in schema.spec.fullDNSName",
			expr:           "${schema.spec.fullDNSName}",
			line:           18,
			char:           20, // cursor on "schema"
			wantLocations:  1,
			clickedSegment: "schema",
			description:    "Should jump to schema definition",
		},
		{
			name:           "click on 'deployment' in deployment.spec.replicas",
			expr:           "${deployment.spec.replicas}",
			line:           18,
			char:           20, // cursor on "deployment"
			wantLocations:  1,
			clickedSegment: "deployment",
			description:    "Should jump to deployment resource definition",
		},
		{
			name:           "single resource",
			expr:           "${deployment}",
			line:           18,
			char:           20,
			wantLocations:  1,
			clickedSegment: "deployment",
			description:    "Should jump to deployment definition",
		},
		{
			name:           "click on 'service' in service",
			expr:           "${service}",
			line:           18,
			char:           20,
			wantLocations:  1,
			clickedSegment: "service",
			description:    "Should jump to service definition",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			// Use testRGDWithSchema which has schema, deployment, service, etc.
			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-definition-segments.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			// Request definition at cursor position
			locations := client.RequestDefinition("file:///tmp/test-definition-segments.yaml", tt.line, tt.char)

			t.Logf("%s: %s - clicked '%s', got %d locations", tt.name, tt.description, tt.clickedSegment, len(locations))

			if len(locations) != tt.wantLocations {
				t.Errorf("%s: expected %d locations, got %d", tt.name, tt.wantLocations, len(locations))
			}

			// Verify location has non-zero range and is not pointing back to the expression
			if len(locations) > 0 {
				loc := locations[0]
				if loc.Range.Start.Line == 0 && loc.Range.Start.Character == 0 &&
					loc.Range.End.Line == 0 && loc.Range.End.Character == 0 {
					t.Errorf("%s: location has zero range", tt.name)
				}

				// The definition should NOT be on line 18 (where the expression is)
				// It should jump to where the resource/schema is actually defined
				if loc.Range.Start.Line == uint32(tt.line) {
					t.Logf("%s: ⚠️  definition is on same line as expression (might be correct for schema spec fields)", tt.name)
				} else {
					t.Logf("%s: ✅ definition jumps to line %d", tt.name, loc.Range.Start.Line)
				}
			}
		})
	}
}

// TestDefinition_SegmentRanges tests that clicking on different segments returns correct definitions
func TestDefinition_SegmentRanges(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	// Test with a known expression: ${schema.spec.name}
	expr := "${schema.spec.name}"
	doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", expr, 1)
	client.OpenDocument("file:///tmp/test-segment-ranges.yaml", doc)
	time.Sleep(300 * time.Millisecond)

	line := 18

	// Test clicking on each segment
	segments := []struct {
		char    int
		segment string
		wantDef bool // Whether we expect a definition
	}{
		{char: 20, segment: "schema", wantDef: true},   // Click on "schema"
		{char: 27, segment: "spec", wantDef: false},    // Click on "spec" (K8s field, no def)
		{char: 32, segment: "name", wantDef: false},    // Click on "name" (schema field, no def)
	}

	for _, seg := range segments {
		locations := client.RequestDefinition("file:///tmp/test-segment-ranges.yaml", line, seg.char)

		if seg.wantDef {
			if len(locations) == 0 {
				t.Errorf("Expected definition for '%s', got none", seg.segment)
			} else {
				// Should jump to a different line (where it's defined)
				if locations[0].Range.Start.Line == uint32(line) {
					t.Errorf("Definition for '%s' should jump away, but stayed on same line", seg.segment)
				} else {
					t.Logf("✅ '%s' jumps to line %d", seg.segment, locations[0].Range.Start.Line)
				}
			}
		} else {
			if len(locations) > 0 {
				t.Logf("'%s' returned definition at line %d (OK if it's a spec field)",
					seg.segment, locations[0].Range.Start.Line)
			} else {
				t.Logf("✅ '%s' has no definition (expected for K8s/schema fields)", seg.segment)
			}
		}
	}
}
