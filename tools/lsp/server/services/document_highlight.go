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
	"strings"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

// DocumentHighlightProvider provides document highlighting for RGD files
type DocumentHighlightProvider struct{}

// NewDocumentHighlightProvider creates a new document highlight provider
func NewDocumentHighlightProvider() *DocumentHighlightProvider {
	return &DocumentHighlightProvider{}
}

// ProvideDocumentHighlight returns highlights for the symbol at the given position
// This tells the editor which specific segments are clickable in a CEL expression
func (dhp *DocumentHighlightProvider) ProvideDocumentHighlight(content string, position protocol.Position) []protocol.DocumentHighlight {
	lines := strings.Split(content, "\n")
	if int(position.Line) >= len(lines) {
		return nil
	}

	line := lines[position.Line]
	col := int(position.Character)

	// Find if we're inside a CEL expression
	celStart := -1
	celEnd := -1

	// Find ${ before cursor
	for i := col; i >= 0; i-- {
		if i < len(line)-1 && line[i] == '$' && line[i+1] == '{' {
			celStart = i + 2 // Position after ${
			break
		}
	}

	if celStart == -1 {
		return nil // Not in CEL expression
	}

	// Find } after cursor
	for i := celStart; i < len(line); i++ {
		if line[i] == '}' {
			celEnd = i
			break
		}
	}

	if celEnd == -1 {
		return nil // Incomplete CEL expression
	}

	// Extract the CEL expression
	celExpr := line[celStart:celEnd]

	// Find the path expression at cursor
	pathExpr, segmentRanges := extractPathAtPosition(celExpr, col-celStart, position.Line, uint32(celStart))

	if pathExpr == "" || len(segmentRanges) == 0 {
		return nil
	}

	// Find which segment the cursor is on
	cursorSegmentIndex := -1
	for i, segRange := range segmentRanges {
		if position.Character >= segRange.Start.Character && position.Character <= segRange.End.Character {
			cursorSegmentIndex = i
			break
		}
	}

	if cursorSegmentIndex == -1 {
		return nil
	}

	// Return highlight for ONLY the segment the cursor is on
	// This tells the editor to highlight just this one word, not the entire expression
	cursorSegment := segmentRanges[cursorSegmentIndex]
	kind := protocol.DocumentHighlightKindRead

	return []protocol.DocumentHighlight{
		{
			Range: cursorSegment,
			Kind:  &kind,
		},
	}
}

// extractPathAtPosition extracts a path expression and segment ranges at the given position
// Similar to extractPathExpressionAtPosition in definition.go but works with CEL expression directly
func extractPathAtPosition(celExpr string, cursorPos int, line uint32, celStartCol uint32) (string, []protocol.Range) {
	if cursorPos < 0 || cursorPos > len(celExpr) {
		return "", nil
	}

	// Find the start of the path by working backwards
	start := cursorPos
	for start > 0 {
		c := celExpr[start-1]
		if !isIdentifierChar(c) && c != '.' {
			break
		}
		start--
	}

	// Find the end of the path by working forwards
	end := cursorPos
	for end < len(celExpr) {
		c := celExpr[end]
		if !isIdentifierChar(c) && c != '.' {
			break
		}
		end++
	}

	if start >= end {
		return "", nil
	}

	pathExpr := celExpr[start:end]
	if pathExpr == "" {
		return "", nil
	}

	// Parse into segments
	segments := strings.Split(pathExpr, ".")
	ranges := make([]protocol.Range, 0, len(segments))

	currentPos := start
	for _, segment := range segments {
		if segment == "" {
			continue
		}

		segmentStart := currentPos
		segmentEnd := currentPos + len(segment)

		ranges = append(ranges, protocol.Range{
			Start: protocol.Position{
				Line:      line,
				Character: celStartCol + uint32(segmentStart),
			},
			End: protocol.Position{
				Line:      line,
				Character: celStartCol + uint32(segmentEnd),
			},
		})

		// Move past the segment and the dot
		currentPos = segmentEnd + 1
	}

	return pathExpr, ranges
}
