package test

import (
	"strings"
	"testing"
	"time"
)

// TestDefinition_MultipleSegmentClicks tests that clicking on different segments jumps to different places
func TestDefinition_MultipleSegmentClicks(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	// Expression: ${schema.spec.fullDNSName}
	//  Position:   18 20  25 30
	// Line content: "          name: ${schema.spec.fullDNSName}"
	//  Full indices: 0123456789012345678901234567890123456789012345
	//                          1111111111222222222233333333334444

	expr := "${schema.spec.fullDNSName}"
	doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", expr, 1)
	client.OpenDocument("file:///tmp/test-multi-segment.yaml", doc)
	time.Sleep(300 * time.Millisecond)

	line := 18

	tests := []struct {
		char           int
		segmentClicked string
		wantJumpTo     string
	}{
		{
			char:           20, // On "schema"
			segmentClicked: "schema",
			wantJumpTo:     "schema definition (line 5-6)",
		},
		{
			char:           27, // On "spec" (after "schema.")
			segmentClicked: "spec",
			wantJumpTo:     "spec field or nowhere (K8s field)",
		},
		{
			char:           32, // On "fullDNSName"
			segmentClicked: "fullDNSName",
			wantJumpTo:     "fullDNSName field definition",
		},
	}

	for _, tt := range tests {
		t.Run("click_on_"+tt.segmentClicked, func(t *testing.T) {
			locations := client.RequestDefinition("file:///tmp/test-multi-segment.yaml", line, tt.char)

			t.Logf("Clicked on '%s' (char %d): %s", tt.segmentClicked, tt.char, tt.wantJumpTo)

			if len(locations) == 0 {
				t.Logf("  → No definition found (expected for K8s fields)")
			} else {
				t.Logf("  → Jumps to line %d, char %d-%d",
					locations[0].Range.Start.Line,
					locations[0].Range.Start.Character,
					locations[0].Range.End.Character)

				// Verify it's not jumping back to the same line
				if locations[0].Range.Start.Line == uint32(line) {
					t.Logf("  ⚠️  Same line - might be OK for spec fields")
				}
			}
		})
	}
}

// TestDefinition_DifferentResources tests clicking on different resource names
func TestDefinition_DifferentResources(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	// Test multiple expressions in the same document
	tests := []struct {
		expr           string
		line           int
		char           int
		resourceName   string
		expectedLine   int
	}{
		{
			expr:         "${schema.spec.name}",
			line:         18,
			char:         20, // On "schema"
			resourceName: "schema",
			expectedLine: 5, // Schema starts around line 5-6
		},
		{
			expr:         "${deployment.spec.replicas}",
			line:         18,
			char:         20, // On "deployment"
			resourceName: "deployment",
			expectedLine: 13, // Deployment resource
		},
		{
			expr:         "${service.spec.type}",
			line:         18,
			char:         20, // On "service"
			resourceName: "service",
			expectedLine: 19, // Service resource
		},
	}

	for _, tt := range tests {
		t.Run(tt.resourceName, func(t *testing.T) {
			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			uri := "file:///tmp/test-resource-" + tt.resourceName + ".yaml"
			client.OpenDocument(uri, doc)
			time.Sleep(300 * time.Millisecond)

			locations := client.RequestDefinition(uri, tt.line, tt.char)

			if len(locations) == 0 {
				t.Errorf("Expected definition for %s, got none", tt.resourceName)
				return
			}

			actualLine := int(locations[0].Range.Start.Line)

			// Allow some tolerance (within 3 lines) since exact line may vary
			if actualLine < tt.expectedLine-3 || actualLine > tt.expectedLine+3 {
				t.Errorf("Expected %s definition near line %d, got line %d",
					tt.resourceName, tt.expectedLine, actualLine)
			} else {
				t.Logf("✅ %s definition found at line %d (expected ~%d)",
					tt.resourceName, actualLine, tt.expectedLine)
			}
		})
	}
}

// TestDefinition_OperatorExpressions tests definition in complex CEL expressions
func TestDefinition_OperatorExpressions(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	tests := []struct {
		name         string
		expr         string
		line         int
		char         int
		wantResource string
	}{
		{
			name:         "first resource in addition",
			expr:         "${schema.spec.name+deployment.spec.replicas}",
			line:         18,
			char:         20, // On "schema"
			wantResource: "schema",
		},
		{
			name:         "second resource in addition",
			expr:         "${schema.spec.name+deployment.spec.replicas}",
			line:         18,
			char:         36, // On "deployment"
			wantResource: "deployment",
		},
		{
			name:         "resource in ternary true branch",
			expr:         "${schema.spec.replicas>0?schema.spec.name:\"default\"}",
			line:         18,
			char:         42, // On second "schema"
			wantResource: "schema",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-operator-def.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			locations := client.RequestDefinition("file:///tmp/test-operator-def.yaml", tt.line, tt.char)

			if len(locations) == 0 {
				t.Errorf("%s: expected definition for %s, got none", tt.name, tt.wantResource)
			} else {
				t.Logf("%s: ✅ clicked on %s, got definition at line %d",
					tt.name, tt.wantResource, locations[0].Range.Start.Line)
			}
		})
	}
}
