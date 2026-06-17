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
	"strings"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

// CompletionContextType indicates what kind of completion is needed
type CompletionContextType string

const (
	CompletionContextUnknown       CompletionContextType = "unknown"
	CompletionContextResource      CompletionContextType = "resource"       // Inside ${...}, suggest resource IDs
	CompletionContextField         CompletionContextType = "field"          // After ${resource., suggest fields
	CompletionContextFunction      CompletionContextType = "function"       // CEL function names
	CompletionContextYAMLKey       CompletionContextType = "yaml_key"       // Top-level YAML keys
	CompletionContextResourceField CompletionContextType = "resource_field" // In resource template
)

// CompletionContext provides context about what to complete at a given position
type CompletionContext struct {
	Type         CompletionContextType
	Prefix       string   // The partial text to filter completions
	ResourceID   string   // For field completions, which resource
	InForEach    bool     // Whether we're inside a forEach resource
	ForEachScope string   // The resource ID whose forEach scope we're in
	LineContent  string   // Full line content for context
	InCEL        bool     // Whether we're inside a CEL expression ${...}
}

// AnalyzeCompletionContext determines what kind of completion is needed at the given position
func AnalyzeCompletionContext(content string, position protocol.Position, symbolTable *SymbolTable) *CompletionContext {
	lines := strings.Split(content, "\n")
	if int(position.Line) >= len(lines) {
		return &CompletionContext{Type: CompletionContextUnknown}
	}

	line := lines[position.Line]
	col := int(position.Character)

	// Ensure column is within line bounds
	if col > len(line) {
		col = len(line)
	}

	ctx := &CompletionContext{
		LineContent: line,
		Type:        CompletionContextUnknown,
	}

	// Check if we're inside a CEL expression
	celStart, celEnd := findCELBoundaries(line, col)

	if celStart != -1 && celEnd != -1 && col >= celStart && col <= celEnd {
		ctx.InCEL = true
		ctx.Type, ctx.Prefix, ctx.ResourceID = analyzeCELContext(line[celStart:celEnd], col-celStart)
		return ctx
	}

	// Check for incomplete CEL expression (e.g., after "${" or "${schema.")
	// celStart is the position AFTER ${, but col might be right after the { (before celStart)
	// So check if we're at or after the ${ opening
	if celStart != -1 && celEnd == -1 && col >= celStart-2 {
		ctx.InCEL = true
		// Parse the incomplete expression to determine context
		// Start from celStart (after ${) or col, whichever is later
		exprStart := celStart
		if col > celStart {
			exprStart = celStart
		} else {
			// col is at or before celStart (right after ${)
			// Extract from celStart to get the expression content
			exprStart = celStart
		}

		// End at col, but include trigger characters like '.' if present
		exprEnd := col
		if col < len(line) && line[col] == '.' {
			exprEnd = col + 1
		}

		// Ensure exprEnd doesn't go before exprStart
		if exprEnd < exprStart {
			exprEnd = exprStart
		}

		incompleteExpr := strings.TrimSpace(line[exprStart:exprEnd])
		ctx.Type, ctx.Prefix, ctx.ResourceID = analyzeCELContext(incompleteExpr, len(incompleteExpr))
		return ctx
	}

	// Not in CEL - analyze YAML context
	// Check if we're inside a K8s resource template
	if k8sContext := analyzeK8sYAMLContext(content, position, symbolTable); k8sContext != nil {
		return k8sContext
	}

	// Default to top-level YAML key completion
	ctx.Type = CompletionContextYAMLKey
	ctx.Prefix = extractPrefix(line, col)

	return ctx
}

// findCELBoundaries finds the start and end positions of a CEL expression containing the given column
// Returns (-1, -1) if not in a CEL expression
func findCELBoundaries(line string, col int) (int, int) {
	// Find the nearest ${ before or at the cursor
	start := -1
	for i := col; i >= 0; i-- {
		if i < len(line)-1 && line[i] == '$' && line[i+1] == '{' {
			start = i + 2 // Position after ${
			break
		}
	}

	if start == -1 {
		return -1, -1
	}

	// Find the matching } after the cursor
	end := -1
	for i := start; i < len(line); i++ {
		if line[i] == '}' {
			end = i
			break
		}
	}

	return start, end
}

// analyzeCELContext analyzes the CEL expression to determine completion type
// Returns (type, prefix, resourceID)
func analyzeCELContext(celExpr string, cursorPos int) (CompletionContextType, string, string) {
	// Extract the part before cursor
	beforeCursor := celExpr[:cursorPos]

	// Check if we're after a dot (field access)
	if lastDot := strings.LastIndex(beforeCursor, "."); lastDot != -1 {
		// Field completion: ${resource.fie| or ${schema.spec.fie|
		// Need to handle operators like: ${a+b.field|
		// We want just "b" not "a+b"

		// Extract the resource path by working backwards from the dot
		// and stopping at operators or whitespace
		resourcePart := extractPathBeforeDot(beforeCursor, lastDot)
		fieldPrefix := beforeCursor[lastDot+1:]

		// Pass the full path (e.g., "schema.spec") not just the resource ID
		// This allows completeFields to handle nested paths
		return CompletionContextField, fieldPrefix, resourcePart
	}

	// Check if we're calling a function
	if strings.Contains(beforeCursor, "(") {
		// Inside function arguments - could suggest various things
		// For now, treat as resource completion
		prefix := extractLastIdentifier(beforeCursor)
		return CompletionContextFunction, prefix, ""
	}

	// Default: resource ID completion
	// But need to extract just the last identifier if there are operators
	prefix := extractLastIdentifier(beforeCursor)
	return CompletionContextResource, prefix, ""
}

// extractPathBeforeDot extracts the resource path before a dot,
// stopping at CEL operators to handle cases like: a+b.field
// For "schema.spec.fullDNSName+schema.spec.", returns "schema.spec"
func extractPathBeforeDot(text string, dotPos int) string {
	if dotPos <= 0 {
		return ""
	}

	// Work backwards from the dot to find where the identifier/path starts
	// Stop at: operators (+, -, *, /, %, ==, !=, <, >, &&, ||, !, ?, :)
	// Stop at: brackets, whitespace
	// But ALLOW: parentheses (for method calls), quotes (for arguments), commas (for multiple args)
	start := dotPos - 1

	for start >= 0 {
		ch := text[start]

		// Allowed: alphanumeric, underscore, dot, parentheses, quotes, commas, spaces (for method arguments)
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
		   (ch >= '0' && ch <= '9') || ch == '_' || ch == '.' ||
		   ch == '(' || ch == ')' || ch == '\'' || ch == '"' || ch == ',' || ch == ' ' {
			start--
			continue
		}

		// Found an operator or separator - this is where the path starts
		start++
		break
	}

	// If we went all the way to the beginning
	if start < 0 {
		start = 0
	}

	result := strings.TrimSpace(text[start:dotPos])
	// Also trim trailing dots (e.g., "schema.spec." -> "schema.spec")
	result = strings.TrimSuffix(result, ".")
	return result
}

// extractResourceID extracts the resource identifier from an expression
// Handles simple cases like "deployment" or "schema.spec"
func extractResourceID(expr string) string {
	// Trim whitespace and special characters
	expr = strings.TrimSpace(expr)

	// Take the first identifier (before any operators or dots)
	parts := strings.FieldsFunc(expr, func(r rune) bool {
		return r == ' ' || r == '(' || r == ')' || r == '[' || r == ']' || r == ',' || r == '+'
	})

	if len(parts) == 0 {
		return ""
	}

	// Return the first part (the resource ID)
	first := parts[0]

	// If it contains a dot, take only up to the first dot
	if dotIdx := strings.Index(first, "."); dotIdx != -1 {
		return first[:dotIdx]
	}

	return first
}

// extractLastIdentifier extracts the last identifier being typed
func extractLastIdentifier(text string) string {
	// Work backwards from the end to find the start of the identifier
	end := len(text)
	start := end

	for start > 0 {
		c := text[start-1]
		if !isIdentifierChar(c) {
			break
		}
		start--
	}

	return text[start:end]
}

// isIdentifierChar checks if a character can be part of an identifier
func isIdentifierChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

// extractPrefix extracts the word prefix before the cursor position
func extractPrefix(line string, col int) string {
	if col <= 0 || col > len(line) {
		return ""
	}

	start := col
	for start > 0 && isIdentifierChar(line[start-1]) {
		start--
	}

	return line[start:col]
}

// GetResourceIDFromPosition attempts to determine which resource contains the given position
func GetResourceIDFromPosition(content string, position protocol.Position, positionMap map[string]protocol.Range) string {
	// Find which resource range contains this position
	for path, r := range positionMap {
		if !strings.Contains(path, "spec.resources[") {
			continue
		}

		// Check if position is within this range
		if positionInRange(position, r) {
			// Extract resource index from path like "spec.resources[0].id"
			// This is a simplified approach - could be enhanced
			return extractResourceIndex(path)
		}
	}

	return ""
}

// positionInRange checks if a position is within a range
func positionInRange(pos protocol.Position, r protocol.Range) bool {
	if pos.Line < r.Start.Line || pos.Line > r.End.Line {
		return false
	}
	if pos.Line == r.Start.Line && pos.Character < r.Start.Character {
		return false
	}
	if pos.Line == r.End.Line && pos.Character > r.End.Character {
		return false
	}
	return true
}

// extractResourceIndex extracts resource index from a path
func extractResourceIndex(path string) string {
	// This is a placeholder - in reality we'd need to map back to resource ID
	// For now, just return empty
	return ""
}

// analyzeK8sYAMLContext determines if cursor is inside a K8s resource template
// and returns context for K8s field completion
func analyzeK8sYAMLContext(content string, position protocol.Position, symbolTable *SymbolTable) *CompletionContext {
	if symbolTable == nil || symbolTable.Resources == nil {
		return nil
	}

	lines := strings.Split(content, "\n")
	if int(position.Line) >= len(lines) {
		return nil
	}

	// Walk backwards from cursor to find:
	// 1. Which resource we're in (look for "- id: <resourceId>")
	// 2. Where we are in the template hierarchy

	var resourceID string
	var k8sKind string
	var indent int // Indentation level of current line
	currentLine := lines[position.Line]
	indent = countLeadingSpaces(currentLine)

	// Find the resource by looking backwards for "- id: xxx"
	for i := int(position.Line); i >= 0; i-- {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Check for resource ID marker
		if strings.HasPrefix(trimmed, "- id:") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 3 {
				resourceID = parts[2]
				break
			}
		}

		// If we hit "resources:" we've gone too far back
		if trimmed == "resources:" {
			break
		}
	}

	if resourceID == "" {
		return nil
	}

	resource := symbolTable.LookupResource(resourceID)
	if resource == nil || resource.K8sKind == "" {
		return nil
	}

	k8sKind = resource.K8sKind
	yamlPath := buildYAMLPath(lines, int(position.Line), indent)

	// Encode resourceID, k8sKind, and yamlPath into ResourceID field
	// Format: "resourceID:kind:path"
	encodedResourceID := resourceID + ":" + k8sKind + ":" + yamlPath

	return &CompletionContext{
		Type:        CompletionContextResourceField,
		ResourceID:  encodedResourceID,
		Prefix:      extractPrefix(currentLine, int(position.Character)),
		InCEL:       false,
		LineContent: currentLine,
	}
}

// countLeadingSpaces counts leading whitespace characters
func countLeadingSpaces(line string) int {
	count := 0
	for _, ch := range line {
		if ch == ' ' || ch == '\t' {
			count++
		} else {
			break
		}
	}
	return count
}

// buildYAMLPath constructs the YAML path from template root to the current line
// For example: "spec.template.spec.containers"
func buildYAMLPath(lines []string, currentLine int, currentIndent int) string {
	path := []string{}

	// Walk backwards collecting YAML keys
	for i := currentLine - 1; i >= 0; i-- {
		line := lines[i]
		indent := countLeadingSpaces(line)
		trimmed := strings.TrimSpace(line)

		// Stop if we hit the resource id line (we've gone past the template marker)
		if strings.Contains(trimmed, "- id:") {
			break
		}

		// Check if this is the RGD resource template marker
		// It should be at a specific indentation level (after "- id: xxx")
		// and followed by K8s resource fields (apiVersion, kind, etc)
		if trimmed == "template:" && i+1 < len(lines) {
			nextLine := strings.TrimSpace(lines[i+1])
			// If next line is apiVersion/kind/metadata/spec, this is the resource template marker
			if strings.HasPrefix(nextLine, "apiVersion:") ||
			   strings.HasPrefix(nextLine, "kind:") ||
			   strings.HasPrefix(nextLine, "metadata:") ||
			   strings.HasPrefix(nextLine, "spec:") {
				// Found the resource template root - stop here
				break
			}
		}

		// If this line is less indented than current, it's a parent key
		if indent < currentIndent && strings.HasSuffix(trimmed, ":") {
			// Extract key name (remove trailing colon)
			key := strings.TrimSuffix(trimmed, ":")
			key = strings.Fields(key)[0] // Handle "key: value" cases

			// Skip list markers and numeric indices
			if key != "-" && !strings.HasPrefix(key, "[") {
				path = append([]string{key}, path...)
			}

			currentIndent = indent
		}
	}

	return strings.Join(path, ".")
}
