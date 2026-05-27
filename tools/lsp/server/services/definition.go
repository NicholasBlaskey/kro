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
	"fmt"
	"strings"

	"github.com/kro-run/kro/tools/lsp/server/analysis"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// DefinitionProvider provides go-to-definition for RGD files
type DefinitionProvider struct {
	symbolTable *analysis.SymbolTable
	positionMap map[string]protocol.Range
	documentURI string
}

// NewDefinitionProvider creates a new definition provider
func NewDefinitionProvider(symbolTable *analysis.SymbolTable, positionMap map[string]protocol.Range, documentURI string) *DefinitionProvider {
	return &DefinitionProvider{
		symbolTable: symbolTable,
		positionMap: positionMap,
		documentURI: documentURI,
	}
}

// ProvideDefinition returns the definition location for the symbol at the given position.
// Returns LocationLink with OriginSelectionRange so the editor only underlines the
// specific segment under the cursor, not the entire path expression.
func (dp *DefinitionProvider) ProvideDefinition(content string, position protocol.Position) []protocol.LocationLink {
	if dp.symbolTable == nil {
		return nil
	}

	pathExpr, segmentRanges := dp.extractPathExpressionAtPosition(content, position)
	if pathExpr == "" {
		return nil
	}

	segmentIndex := -1
	for i, segRange := range segmentRanges {
		if position.Character >= segRange.Start.Character && position.Character <= segRange.End.Character {
			segmentIndex = i
			break
		}
	}

	if segmentIndex == -1 {
		return nil
	}

	segments := strings.Split(pathExpr, ".")
	if segmentIndex >= len(segments) {
		return nil
	}

	segment := segments[segmentIndex]
	originRange := segmentRanges[segmentIndex]

	var targetRange *protocol.Range

	if segmentIndex == 0 {
		if segment == "schema" && dp.symbolTable.Schema != nil {
			if !isZeroRange(dp.symbolTable.Schema.Position) {
				r := dp.symbolTable.Schema.Position
				targetRange = &r
			}
		} else if symbol := dp.symbolTable.LookupResource(segment); symbol != nil {
			if !isZeroRange(symbol.Position) {
				r := symbol.Position
				targetRange = &r
			}
		} else {
			for _, scope := range dp.symbolTable.Scopes {
				if iter, ok := scope.Iterators[segment]; ok {
					if !isZeroRange(iter.Position) {
						r := iter.Position
						targetRange = &r
						break
					}
				}
			}
		}
	} else if segments[0] == "schema" {
		if segmentIndex == 1 && segment == "spec" {
			if pos, ok := dp.positionMap["spec.schema.spec#key"]; ok && !isZeroRange(pos) {
				targetRange = &pos
			} else if pos, ok := dp.positionMap["spec.schema.spec"]; ok && !isZeroRange(pos) {
				targetRange = &pos
			}
		} else if segmentIndex >= 2 {
			fieldName := segment
			posMapKey := "spec.schema.spec." + fieldName + "#key"
			if pos, ok := dp.positionMap[posMapKey]; ok && !isZeroRange(pos) {
				targetRange = &pos
			} else {
				posMapKey = "spec.schema.spec." + fieldName
				if pos, ok := dp.positionMap[posMapKey]; ok && !isZeroRange(pos) {
					targetRange = &pos
				}
			}
		}
	} else {
		// Handle k8s resource field paths like "service.metadata" or "deployment.spec.replicas"
		resourceID := segments[0]
		if symbol := dp.symbolTable.LookupResource(resourceID); symbol != nil && segmentIndex >= 1 {
			// Build the field path relative to the resource template
			// Example: for "service.metadata.labels", fieldPath = "metadata.labels"
			fieldPath := strings.Join(segments[1:segmentIndex+1], ".")

			// Look up the field in the resource's template in positionMap
			// Use the array index from the symbol table
			resourcePath := fmt.Sprintf("spec.resources[%d].template.%s", symbol.ArrayIndex, fieldPath)

			// Try both the key and value positions
			posMapKey := resourcePath + "#key"
			if pos, ok := dp.positionMap[posMapKey]; ok && !isZeroRange(pos) {
				targetRange = &pos
			} else if pos, ok := dp.positionMap[resourcePath]; ok && !isZeroRange(pos) {
				targetRange = &pos
			}
		}
	}

	if targetRange == nil {
		return nil
	}

	return []protocol.LocationLink{
		{
			OriginSelectionRange: &originRange,
			TargetURI:           dp.documentURI,
			TargetRange:         *targetRange,
			TargetSelectionRange: *targetRange,
		},
	}
}

// extractPathExpressionAtPosition extracts the full path expression at cursor position
// Returns the path string and a slice of ranges for each segment
// Example: "schema.spec.fullDNSName" -> ["schema", "spec", "fullDNSName"] with their ranges
func (dp *DefinitionProvider) extractPathExpressionAtPosition(content string, position protocol.Position) (string, []protocol.Range) {
	lines := strings.Split(content, "\n")
	if int(position.Line) >= len(lines) {
		return "", nil
	}

	line := lines[position.Line]
	col := int(position.Character)

	if col >= len(line) {
		col = len(line) - 1
	}

	if col < 0 {
		return "", nil
	}

	// Find the start of the path expression
	// Work backwards from cursor, including dots and identifier chars
	start := col
	for start > 0 {
		c := line[start-1]
		if !isIdentifierChar(c) && c != '.' {
			break
		}
		start--
	}

	// Find the end of the path expression
	// Work forwards from cursor, including dots and identifier chars
	end := col
	for end < len(line) {
		c := line[end]
		if !isIdentifierChar(c) && c != '.' {
			break
		}
		end++
	}

	if start >= end {
		return "", nil
	}

	pathExpr := line[start:end]

	// Parse into segments and compute ranges for each
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
				Line:      position.Line,
				Character: uint32(segmentStart),
			},
			End: protocol.Position{
				Line:      position.Line,
				Character: uint32(segmentEnd),
			},
		})

		// Move past the segment and the dot
		currentPos = segmentEnd + 1 // +1 for the dot
	}

	return pathExpr, ranges
}

// extractIdentifierAtPosition extracts the identifier at the cursor position
func (dp *DefinitionProvider) extractIdentifierAtPosition(content string, position protocol.Position) string {
	lines := strings.Split(content, "\n")
	if int(position.Line) >= len(lines) {
		return ""
	}

	line := lines[position.Line]
	col := int(position.Character)

	if col >= len(line) {
		col = len(line) - 1
	}

	if col < 0 {
		return ""
	}

	// Find the start of the identifier
	start := col
	for start > 0 && isIdentifierChar(line[start-1]) {
		start--
	}

	// Find the end of the identifier
	end := col
	for end < len(line) && isIdentifierChar(line[end]) {
		end++
	}

	if start >= end {
		return ""
	}

	return line[start:end]
}

