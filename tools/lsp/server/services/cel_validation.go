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

	"github.com/google/cel-go/cel"
	"github.com/kro-run/kro/tools/lsp/server/analysis"
	kcel "github.com/kubernetes-sigs/kro/pkg/cel"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

// CELValidator validates CEL expressions and provides diagnostics
type CELValidator struct {
	symbolTable *analysis.SymbolTable
}

// NewCELValidator creates a new CEL validator
func NewCELValidator(symbolTable *analysis.SymbolTable) *CELValidator {
	return &CELValidator{
		symbolTable: symbolTable,
	}
}

// ValidateExpression validates a single CEL expression and returns diagnostics
// The expression should be the content INSIDE ${...}, not including the delimiters
func (cv *CELValidator) ValidateExpression(expr string, position protocol.Position, exprStartChar uint32) []protocol.Diagnostic {
	if cv.symbolTable == nil {
		return nil
	}

	// Build schema map for typed validation
	schemas := cv.buildSchemaMap()

	// Collect all resource IDs (both typed and untyped)
	allResourceIDs := make([]string, 0, len(cv.symbolTable.Resources))
	for id := range cv.symbolTable.Resources {
		allResourceIDs = append(allResourceIDs, id)
	}

	// Create CEL environment with both typed schemas and untyped resource declarations
	var env *cel.Env
	var err error

	if len(schemas) > 0 {
		// Typed environment with schema information, plus untyped resource IDs
		// Resources with schemas get typed validation, others are declared as 'any'
		untypedResourceIDs := make([]string, 0)
		for _, id := range allResourceIDs {
			if _, hasSchema := schemas[id]; !hasSchema {
				untypedResourceIDs = append(untypedResourceIDs, id)
			}
		}
		untypedResourceIDs = append(untypedResourceIDs, "self") // Add 'self' as untyped
		env, err = kcel.DefaultEnvironment(
			kcel.WithTypedResources(schemas),
			kcel.WithResourceIDs(untypedResourceIDs),
		)
	} else {
		// Fallback to untyped environment (current behavior)
		allResourceIDs = append(allResourceIDs, "self")
		env, err = kcel.DefaultEnvironment(kcel.WithResourceIDs(allResourceIDs))
	}

	if err != nil {
		// Environment creation failed - return a diagnostic
		return []protocol.Diagnostic{
			{
				Range: protocol.Range{
					Start: protocol.Position{Line: position.Line, Character: exprStartChar},
					End:   protocol.Position{Line: position.Line, Character: exprStartChar + uint32(len(expr))},
				},
				Severity: severityPtr(protocol.DiagnosticSeverityError),
				Source:   stringPtr("cel"),
				Message:  fmt.Sprintf("Failed to create CEL environment: %v", err),
			},
		}
	}

	// Parse the expression
	parsedAST, issues := env.Parse(expr)
	if issues != nil && issues.Err() != nil {
		return cv.celIssuesToDiagnostics(issues, position, exprStartChar, expr)
	}

	// Type check the expression (this catches type errors!)
	_, issues = env.Check(parsedAST)
	if issues != nil && issues.Err() != nil {
		return cv.celIssuesToDiagnostics(issues, position, exprStartChar, expr)
	}

	// Expression is valid
	return nil
}

// buildSchemaMap builds a map of schemas for CEL typed validation
func (cv *CELValidator) buildSchemaMap() map[string]*spec.Schema {
	schemas := make(map[string]*spec.Schema)

	// Add instance schema if available
	if instanceSchema := cv.symbolTable.GetInstanceSchema(); instanceSchema != nil {
		schemas["schema"] = instanceSchema
	}

	// Add resource schemas if available
	if cv.symbolTable.ResourceSchemas != nil {
		for id, schema := range cv.symbolTable.ResourceSchemas {
			schemas[id] = schema
		}
	}

	return schemas
}

// celIssuesToDiagnostics converts CEL compilation issues to LSP diagnostics
func (cv *CELValidator) celIssuesToDiagnostics(issues *cel.Issues, linePos protocol.Position, exprStartChar uint32, expr string) []protocol.Diagnostic {
	var diagnostics []protocol.Diagnostic

	for _, celErr := range issues.Errors() {
		// CEL Error has Location and Message fields
		errorMsg := celErr.Message

		// Calculate actual position in the document
		// CEL Location has Line() and Column() methods
		// CEL uses 1-based line/column, LSP uses 0-based
		var startChar, endChar uint32

		if celErr.Location != nil {
			// CEL gave us location information
			celCol := celErr.Location.Column()
			if celCol > 0 {
				startChar = exprStartChar + uint32(celCol-1)
				endChar = startChar + 1 // Highlight single character
			} else {
				// No column info - highlight the whole expression
				startChar = exprStartChar
				endChar = exprStartChar + uint32(len(expr))
			}
		} else {
			// No location info - highlight the whole expression
			startChar = exprStartChar
			endChar = exprStartChar + uint32(len(expr))
		}

		diagnostic := protocol.Diagnostic{
			Range: protocol.Range{
				Start: protocol.Position{Line: linePos.Line, Character: startChar},
				End:   protocol.Position{Line: linePos.Line, Character: endChar},
			},
			Severity: severityPtr(protocol.DiagnosticSeverityError),
			Source:   stringPtr("cel"),
			Message:  errorMsg,
		}
		diagnostics = append(diagnostics, diagnostic)
	}

	return diagnostics
}

// ValidateDocument scans a document for CEL expressions and validates them all
func (cv *CELValidator) ValidateDocument(content string) []protocol.Diagnostic {
	var diagnostics []protocol.Diagnostic

	lines := strings.Split(content, "\n")
	for lineNum, line := range lines {
		// Find all ${...} expressions in the line
		diagnostics = append(diagnostics, cv.validateLineExpressions(line, uint32(lineNum))...)
	}

	return diagnostics
}

// validateLineExpressions finds and validates all CEL expressions in a line
func (cv *CELValidator) validateLineExpressions(line string, lineNum uint32) []protocol.Diagnostic {
	var diagnostics []protocol.Diagnostic

	for i := 0; i < len(line); i++ {
		// Look for ${
		if i < len(line)-1 && line[i] == '$' && line[i+1] == '{' {
			// Found start of expression
			exprStart := i + 2

			// Find matching }
			depth := 1
			j := exprStart
			for j < len(line) && depth > 0 {
				if line[j] == '{' {
					depth++
				} else if line[j] == '}' {
					depth--
				}
				j++
			}

			if depth == 0 {
				// Found complete expression
				expr := line[exprStart : j-1]

				// Validate this expression
				exprDiags := cv.ValidateExpression(expr, protocol.Position{Line: lineNum, Character: 0}, uint32(exprStart))
				diagnostics = append(diagnostics, exprDiags...)

				// Skip past this expression
				i = j - 1
			}
		}
	}

	return diagnostics
}

func severityPtr(s protocol.DiagnosticSeverity) *protocol.DiagnosticSeverity {
	return &s
}
