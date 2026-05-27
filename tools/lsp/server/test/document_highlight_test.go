package test

import (
	"strings"
	"testing"
	"time"
)

// TestDocumentHighlight_SingleSegment tests that document highlight only highlights the segment under the cursor
func TestDocumentHighlight_SingleSegment(t *testing.T) {
	tests := []struct {
		name           string
		expr           string
		line           int
		char           int
		expectedStart  uint32
		expectedEnd    uint32
		segmentName    string
		description    string
	}{
		{
			name:          "highlight 'schema' in schema.spec.name",
			expr:          "${schema.spec.name}",
			line:          18,
			char:          20, // cursor on "schema"
			expectedStart: 18, // character position of 's' in "schema"
			expectedEnd:   24, // character position after 'a' in "schema"
			segmentName:   "schema",
			description:   "Should highlight ONLY 'schema', not the entire expression",
		},
		{
			name:          "highlight 'spec' in schema.spec.name",
			expr:          "${schema.spec.name}",
			line:          18,
			char:          27, // cursor on "spec"
			expectedStart: 25, // character position of 's' in "spec"
			expectedEnd:   29, // character position after 'c' in "spec"
			segmentName:   "spec",
			description:   "Should highlight ONLY 'spec', not the entire expression",
		},
		{
			name:          "highlight 'name' in schema.spec.name",
			expr:          "${schema.spec.name}",
			line:          18,
			char:          32, // cursor on "name"
			expectedStart: 30, // character position of 'n' in "name"
			expectedEnd:   34, // character position after 'e' in "name"
			segmentName:   "name",
			description:   "Should highlight ONLY 'name', not the entire expression",
		},
		{
			name:          "highlight 'deployment' in deployment.spec.replicas",
			expr:          "${deployment.spec.replicas}",
			line:          18,
			char:          20, // cursor on "deployment"
			expectedStart: 18, // character position of 'd' in "deployment"
			expectedEnd:   28, // character position after 't' in "deployment"
			segmentName:   "deployment",
			description:   "Should highlight ONLY 'deployment', not the entire expression",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			// Use testRGDWithSchema which has schema, deployment, service, etc.
			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-document-highlight.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			// Request document highlight at cursor position
			highlights := client.RequestDocumentHighlight("file:///tmp/test-document-highlight.yaml", tt.line, tt.char)

			t.Logf("%s: %s - cursor on '%s', got %d highlights", tt.name, tt.description, tt.segmentName, len(highlights))

			// We should get exactly 1 highlight (the segment under cursor)
			if len(highlights) != 1 {
				t.Errorf("%s: expected 1 highlight, got %d", tt.name, len(highlights))
				return
			}

			highlight := highlights[0]

			// Check that the highlight range matches the expected segment boundaries
			if highlight.Range.Start.Character != tt.expectedStart {
				t.Errorf("%s: expected start character %d, got %d", tt.name, tt.expectedStart, highlight.Range.Start.Character)
			}

			if highlight.Range.End.Character != tt.expectedEnd {
				t.Errorf("%s: expected end character %d, got %d", tt.name, tt.expectedEnd, highlight.Range.End.Character)
			}

			// Calculate the length of the highlighted text
			highlightedLength := highlight.Range.End.Character - highlight.Range.Start.Character
			expectedLength := uint32(len(tt.segmentName))

			if highlightedLength != expectedLength {
				t.Errorf("%s: highlighted text has length %d, expected %d for '%s'",
					tt.name, highlightedLength, expectedLength, tt.segmentName)
			}

			t.Logf("✅ %s: correctly highlighted only '%s' (chars %d-%d)",
				tt.name, tt.segmentName, highlight.Range.Start.Character, highlight.Range.End.Character)
		})
	}
}

// TestDocumentHighlight_NotInExpression tests that no highlight is returned outside CEL expressions
func TestDocumentHighlight_NotInExpression(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	doc := testRGDWithSchema
	client.OpenDocument("file:///tmp/test-no-highlight.yaml", doc)
	time.Sleep(300 * time.Millisecond)

	// Request highlight on a line that doesn't have a CEL expression
	highlights := client.RequestDocumentHighlight("file:///tmp/test-no-highlight.yaml", 5, 10)

	if len(highlights) != 0 {
		t.Errorf("Expected 0 highlights outside CEL expression, got %d", len(highlights))
	} else {
		t.Log("✅ Correctly returned no highlights outside CEL expression")
	}
}
