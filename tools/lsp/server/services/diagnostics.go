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
	"regexp"
	"strings"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

// DiagnosticGenerator creates LSP diagnostics from validation errors with accurate positioning
type DiagnosticGenerator struct {
	positions map[string]protocol.Range
}

// NewDiagnosticGenerator creates a new diagnostic generator
func NewDiagnosticGenerator(positions map[string]protocol.Range) *DiagnosticGenerator {
	return &DiagnosticGenerator{
		positions: positions,
	}
}

// GenerateFromError converts a validation error to LSP diagnostics with accurate positions
func (dg *DiagnosticGenerator) GenerateFromError(err error) []protocol.Diagnostic {
	if err == nil {
		return nil
	}

	errorMsg := err.Error()

	// Try to extract context and find position
	position := dg.findPositionFromError(errorMsg)

	severity := protocol.DiagnosticSeverityError
	diagnostic := protocol.Diagnostic{
		Range:    position,
		Severity: &severity,
		Message:  errorMsg,
		Source:   stringPtr("kro-lsp"),
	}

	return []protocol.Diagnostic{diagnostic}
}

// findPositionFromError attempts to extract location info from error message and find position
func (dg *DiagnosticGenerator) findPositionFromError(errorMsg string) protocol.Range {
	// Try various patterns to extract context from error messages

	// Pattern 1: resource "resourceId"
	if match := regexp.MustCompile(`resource ["\']([^"\']+)["\']`).FindStringSubmatch(errorMsg); len(match) > 1 {
		resourceID := match[1]
		if pos := dg.findResourceIDPosition(resourceID); !isZeroRange(pos) {
			return pos
		}
	}

	// Pattern 2: id resourceId
	if match := regexp.MustCompile(`id ([a-zA-Z0-9]+)`).FindStringSubmatch(errorMsg); len(match) > 1 {
		resourceID := match[1]
		if pos := dg.findResourceIDPosition(resourceID); !isZeroRange(pos) {
			return pos
		}
	}

	// Pattern 3: kind 'KindName'
	if match := regexp.MustCompile(`kind ["\']([^"\']+)["\']`).FindStringSubmatch(errorMsg); len(match) > 1 {
		if pos, ok := dg.positions["spec.schema.kind"]; ok {
			return pos
		}
	}

	// Pattern 4: metadata.namespace or other field paths
	if match := regexp.MustCompile(`(metadata\.[a-zA-Z]+|spec\.[a-zA-Z\.]+)`).FindStringSubmatch(errorMsg); len(match) > 0 {
		fieldPath := match[0]
		// Try to find this path in any resource template
		for path, pos := range dg.positions {
			if strings.Contains(path, "template") && strings.Contains(path, fieldPath) {
				return pos
			}
		}
	}

	// Pattern 5: forEach iterator name
	if match := regexp.MustCompile(`forEach iterator name ["\']([^"\']+)["\']`).FindStringSubmatch(errorMsg); len(match) > 1 {
		iterName := match[1]
		// Search for forEach dimension with this name
		for path, pos := range dg.positions {
			if strings.Contains(path, "forEach") && strings.HasSuffix(path, iterName) {
				return pos
			}
		}
	}

	// Pattern 6: duplicate resource IDs - try to find the second occurrence
	if strings.Contains(errorMsg, "duplicate resource ID") {
		if match := regexp.MustCompile(`duplicate resource IDs? ([a-zA-Z0-9]+)`).FindStringSubmatch(errorMsg); len(match) > 1 {
			resourceID := match[1]
			// Find all occurrences and return the second one
			occurrences := dg.findAllResourceIDPositions(resourceID)
			if len(occurrences) > 1 {
				return occurrences[1]
			}
		}
	}

	// Fallback: try to find any resource mentioned in the error
	for id := range dg.getAllResourceIDs() {
		if strings.Contains(errorMsg, id) {
			if pos := dg.findResourceIDPosition(id); !isZeroRange(pos) {
				return pos
			}
		}
	}

	// Default to (0,0) if no position found
	return protocol.Range{
		Start: protocol.Position{Line: 0, Character: 0},
		End:   protocol.Position{Line: 0, Character: 10},
	}
}

// findResourceIDPosition finds the position of a resource ID declaration
func (dg *DiagnosticGenerator) findResourceIDPosition(resourceID string) protocol.Range {
	// Look for spec.resources[i].id patterns
	for path, pos := range dg.positions {
		if strings.Contains(path, ".id") && strings.Contains(path, "spec.resources") {
			// We need to check if this is the right resource
			// For now, return the first match - can be improved by checking value
			_ = resourceID // TODO: match actual ID value
			return pos
		}
	}
	return protocol.Range{}
}

// findAllResourceIDPositions finds all positions where a resource ID appears
func (dg *DiagnosticGenerator) findAllResourceIDPositions(resourceID string) []protocol.Range {
	var positions []protocol.Range
	for path, pos := range dg.positions {
		if strings.Contains(path, ".id") && strings.Contains(path, "spec.resources") {
			positions = append(positions, pos)
		}
	}
	return positions
}

// getAllResourceIDs extracts all resource IDs from position paths
func (dg *DiagnosticGenerator) getAllResourceIDs() map[string]bool {
	ids := make(map[string]bool)
	for path := range dg.positions {
		if strings.Contains(path, "spec.resources[") && strings.Contains(path, "].id") {
			// Extract from path like "spec.resources[0].id"
			ids[path] = true
		}
	}
	return ids
}


// CreateDiagnostic creates a diagnostic at a specific position
func CreateDiagnostic(position protocol.Range, severity protocol.DiagnosticSeverity, message string) protocol.Diagnostic {
	source := "kro-lsp"
	return protocol.Diagnostic{
		Range:    position,
		Severity: &severity,
		Message:  message,
		Source:   &source,
	}
}

// CreateErrorDiagnostic creates an error diagnostic at the given line and character
func CreateErrorDiagnostic(line, char uint32, message string) protocol.Diagnostic {
	return CreateDiagnostic(
		protocol.Range{
			Start: protocol.Position{Line: line, Character: char},
			End:   protocol.Position{Line: line, Character: char + 10},
		},
		protocol.DiagnosticSeverityError,
		message,
	)
}

// CreateWarningDiagnostic creates a warning diagnostic at the given position
func CreateWarningDiagnostic(position protocol.Range, message string) protocol.Diagnostic {
	return CreateDiagnostic(position, protocol.DiagnosticSeverityWarning, message)
}

// CreateHintDiagnostic creates a hint diagnostic at the given position
func CreateHintDiagnostic(position protocol.Range, message string) protocol.Diagnostic {
	return CreateDiagnostic(position, protocol.DiagnosticSeverityHint, fmt.Sprintf("Hint: %s", message))
}
