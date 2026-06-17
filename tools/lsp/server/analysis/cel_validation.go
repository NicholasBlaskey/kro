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

package analysis

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/cel-go/cel"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// CELError represents a CEL compilation or type checking error with location info
type CELError struct {
	Expression string // The CEL expression that failed
	Message    string // Error message from CEL compiler
	Line       int    // Line in YAML where expression appears (0-indexed)
	Column     int    // Column in YAML where expression starts (0-indexed)
}

// CELValidator validates CEL expressions and extracts detailed error information
type CELValidator struct {
	env *cel.Env
}

// NewCELValidator creates a CEL validator with the given environment
func NewCELValidator(env *cel.Env) *CELValidator {
	return &CELValidator{env: env}
}

// ValidateExpression validates a CEL expression and returns any errors
func (v *CELValidator) ValidateExpression(expr string) *CELError {
	if v.env == nil {
		return nil
	}

	// Parse the expression
	parsedAST, issues := v.env.Parse(expr)
	if issues != nil && issues.Err() != nil {
		return &CELError{
			Expression: expr,
			Message:    v.formatCELError(issues.Err()),
		}
	}

	// Type check the expression
	_, issues = v.env.Check(parsedAST)
	if issues != nil && issues.Err() != nil {
		return &CELError{
			Expression: expr,
			Message:    v.formatCELError(issues.Err()),
		}
	}

	return nil
}

// ValidateExpressionWithType validates a CEL expression and checks if it returns the expected type
func (v *CELValidator) ValidateExpressionWithType(expr string, expectedType *cel.Type) *CELError {
	if v.env == nil {
		return nil
	}

	// Parse the expression
	parsedAST, issues := v.env.Parse(expr)
	if issues != nil && issues.Err() != nil {
		return &CELError{
			Expression: expr,
			Message:    v.formatCELError(issues.Err()),
		}
	}

	// Type check the expression
	checkedAST, issues := v.env.Check(parsedAST)
	if issues != nil && issues.Err() != nil {
		return &CELError{
			Expression: expr,
			Message:    v.formatCELError(issues.Err()),
		}
	}

	// Check if the output type matches expected type
	if expectedType != nil {
		outputType := checkedAST.OutputType()
		if !expectedType.IsAssignableType(outputType) {
			return &CELError{
				Expression: expr,
				Message: fmt.Sprintf("type mismatch: expression returns %s but expected %s",
					outputType.String(), expectedType.String()),
			}
		}
	}

	return nil
}

// formatCELError formats a CEL error message to be more readable
func (v *CELValidator) formatCELError(err error) string {
	msg := err.Error()

	// Extract the most relevant part of the error message
	// CEL errors often have format: "ERROR: <location>:line:col: message"
	if strings.Contains(msg, "ERROR:") {
		parts := strings.SplitN(msg, ":", 4)
		if len(parts) >= 4 {
			return strings.TrimSpace(parts[3])
		}
	}

	// Clean up common CEL error patterns
	msg = strings.TrimPrefix(msg, "ERROR: ")
	msg = strings.TrimPrefix(msg, "<input>:")

	return msg
}

// ExtractCELErrorLocation attempts to extract line/column from CEL error message
func ExtractCELErrorLocation(errMsg string) (int, int, bool) {
	// CEL errors have format like: "ERROR: <input>:1:15: undeclared reference..."
	re := regexp.MustCompile(`<input>:(\d+):(\d+)`)
	matches := re.FindStringSubmatch(errMsg)
	if len(matches) == 3 {
		var line, col int
		// CEL uses 1-based indexing, LSP uses 0-based
		fmt.Sscanf(matches[1], "%d", &line)
		fmt.Sscanf(matches[2], "%d", &col)
		return line - 1, col - 1, true
	}
	return 0, 0, false
}

// CreateCELDiagnostic creates an LSP diagnostic from a CEL error
func CreateCELDiagnostic(celError *CELError, yamlLine int, yamlCol int) protocol.Diagnostic {
	// Try to extract precise location from CEL error message
	if _, col, ok := ExtractCELErrorLocation(celError.Message); ok {
		// CEL gives us position within the expression string
		// We need to offset by the YAML position
		yamlCol += col
	}

	severity := protocol.DiagnosticSeverityError
	source := "kro-cel"

	return protocol.Diagnostic{
		Range: protocol.Range{
			Start: protocol.Position{
				Line:      uint32(yamlLine),
				Character: uint32(yamlCol),
			},
			End: protocol.Position{
				Line:      uint32(yamlLine),
				Character: uint32(yamlCol + len(celError.Expression)),
			},
		},
		Severity: &severity,
		Message:  fmt.Sprintf("CEL error: %s", celError.Message),
		Source:   &source,
	}
}

// IsBoolType checks if a CEL type is boolean or optional<bool>
func IsBoolType(t *cel.Type) bool {
	if t.String() == "bool" {
		return true
	}
	// Check for optional<bool>
	if strings.HasPrefix(t.String(), "optional_type<") && strings.Contains(t.String(), "bool") {
		return true
	}
	return false
}

// IsStringType checks if a CEL type is string
func IsStringType(t *cel.Type) bool {
	return t.String() == "string" || t.String() == "optional_type<string>"
}

// IsListType checks if a CEL type is a list
func IsListType(t *cel.Type) bool {
	return strings.HasPrefix(t.String(), "list<")
}

// IsMapType checks if a CEL type is a map
func IsMapType(t *cel.Type) bool {
	return strings.HasPrefix(t.String(), "map<")
}
