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

// DocumentLinkProvider provides document links for clickable segments
type DocumentLinkProvider struct{}

// NewDocumentLinkProvider creates a new document link provider
func NewDocumentLinkProvider() *DocumentLinkProvider {
	return &DocumentLinkProvider{}
}

// ProvideDocumentLinks returns links for all clickable segments in CEL expressions
// This makes each segment (schema, spec, fullDNSName) visibly clickable
func (dlp *DocumentLinkProvider) ProvideDocumentLinks(content string) []protocol.DocumentLink {
	lines := strings.Split(content, "\n")
	var links []protocol.DocumentLink

	for lineIdx, line := range lines {
		dlp.findLinksInLine(line, uint32(lineIdx), &links)
	}

	return links
}

// findLinksInLine finds all CEL expressions and creates links for each segment
func (dlp *DocumentLinkProvider) findLinksInLine(line string, lineNum uint32, links *[]protocol.DocumentLink) {
	for i := 0; i < len(line); i++ {
		// Look for CEL expression start
		if i < len(line)-1 && line[i] == '$' && line[i+1] == '{' {
			celStart := i + 2 // Position after ${

			// Find the closing }
			celEnd := -1
			for j := celStart; j < len(line); j++ {
				if line[j] == '}' {
					celEnd = j
					break
				}
			}

			if celEnd == -1 {
				continue // Incomplete CEL expression
			}

			// Extract and tokenize the CEL expression
			celExpr := line[celStart:celEnd]
			dlp.createLinksForExpression(celExpr, lineNum, uint32(celStart), links)

			// Skip past this CEL expression
			i = celEnd
		}
	}
}

// createLinksForExpression creates links for each segment in a path expression
func (dlp *DocumentLinkProvider) createLinksForExpression(celExpr string, lineNum uint32, startCol uint32, links *[]protocol.DocumentLink) {
	// Find all path expressions in this CEL expression
	pos := 0

	for pos < len(celExpr) {
		// Skip to next identifier start
		for pos < len(celExpr) && !isIdentifierChar(celExpr[pos]) {
			pos++
		}

		if pos >= len(celExpr) {
			break
		}

		// Extract the path starting at this position
		pathSegments := []struct {
			text  string
			start int
			end   int
		}{}

		// Parse path segments
		for pos < len(celExpr) {
			// Extract identifier
			segStart := pos
			for pos < len(celExpr) && isIdentifierChar(celExpr[pos]) {
				pos++
			}

			if pos > segStart {
				pathSegments = append(pathSegments, struct {
					text  string
					start int
					end   int
				}{
					text:  celExpr[segStart:pos],
					start: segStart,
					end:   pos,
				})
			}

			// Check for dot continuation
			if pos < len(celExpr) && celExpr[pos] == '.' {
				pos++ // Skip the dot
				continue
			}

			// Not a dot - end of path
			break
		}

		// Create links for each segment in the path
		for _, seg := range pathSegments {
			col := startCol + uint32(seg.start)

			// Create a link for this segment
			// The target is intentionally empty - we rely on go-to-definition to handle the click
			*links = append(*links, protocol.DocumentLink{
				Range: protocol.Range{
					Start: protocol.Position{
						Line:      lineNum,
						Character: col,
					},
					End: protocol.Position{
						Line:      lineNum,
						Character: col + uint32(len(seg.text)),
					},
				},
				// No target - editor will use go-to-definition on click
				Target: nil,
			})
		}

		// Move past any remaining non-identifier chars
		for pos < len(celExpr) && !isIdentifierChar(celExpr[pos]) {
			pos++
		}
	}
}
