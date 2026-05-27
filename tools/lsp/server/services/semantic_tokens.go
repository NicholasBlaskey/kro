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

// SemanticTokensProvider provides semantic tokens for syntax highlighting
type SemanticTokensProvider struct{}

// NewSemanticTokensProvider creates a new semantic tokens provider
func NewSemanticTokensProvider() *SemanticTokensProvider {
	return &SemanticTokensProvider{}
}

// ProvideSemanticTokens returns semantic tokens for the entire document
// This tells the editor how to tokenize CEL expressions into separate clickable parts
func (stp *SemanticTokensProvider) ProvideSemanticTokens(content string) []uint32 {
	lines := strings.Split(content, "\n")
	var tokens []uint32

	for lineIdx, line := range lines {
		// Find all CEL expressions in this line
		stp.tokenizeLine(line, uint32(lineIdx), &tokens)
	}

	return tokens
}

// tokenizeLine finds and tokenizes all CEL expressions in a line
func (stp *SemanticTokensProvider) tokenizeLine(line string, lineNum uint32, tokens *[]uint32) {
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

			// Tokenize the CEL expression
			celExpr := line[celStart:celEnd]
			stp.tokenizeCELExpression(celExpr, lineNum, uint32(celStart), tokens)

			// Skip past this CEL expression
			i = celEnd
		}
	}
}

// tokenizeCELExpression breaks a CEL expression into individual tokens
// Each path segment (schema, spec, fullDNSName) becomes a separate token
func (stp *SemanticTokensProvider) tokenizeCELExpression(celExpr string, lineNum uint32, startCol uint32, tokens *[]uint32) {
	pos := 0
	prevLine := lineNum
	prevChar := uint32(0)

	for pos < len(celExpr) {
		// Skip whitespace and operators
		for pos < len(celExpr) && !isIdentifierChar(celExpr[pos]) {
			pos++
		}

		if pos >= len(celExpr) {
			break
		}

		// Extract identifier
		identStart := pos
		for pos < len(celExpr) && isIdentifierChar(celExpr[pos]) {
			pos++
		}

		if pos > identStart {
			identifier := celExpr[identStart:pos]
			col := startCol + uint32(identStart)

			// Add semantic token
			// Format: [deltaLine, deltaStart, length, tokenType, tokenModifiers]
			deltaLine := lineNum - prevLine
			deltaStart := col
			if deltaLine == 0 {
				deltaStart = col - prevChar
			}

			*tokens = append(*tokens,
				deltaLine,                    // line delta
				deltaStart,                   // char delta
				uint32(len(identifier)),      // length
				0,                            // token type (0 = variable)
				0,                            // token modifiers
			)

			prevLine = lineNum
			prevChar = col
		}

		// Skip dot
		if pos < len(celExpr) && celExpr[pos] == '.' {
			pos++
		}
	}
}

// GetSemanticTokensLegend returns the legend for semantic token types
func GetSemanticTokensLegend() protocol.SemanticTokensLegend {
	return protocol.SemanticTokensLegend{
		TokenTypes: []string{
			"variable",  // 0 - for resource IDs and field names
			"property",  // 1 - for properties
			"keyword",   // 2 - for keywords
			"operator",  // 3 - for operators
		},
		TokenModifiers: []string{},
	}
}
