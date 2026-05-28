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

// HoverProvider provides hover information for RGD files
type HoverProvider struct {
	symbolTable *analysis.SymbolTable
	positionMap map[string]protocol.Range
	k8sSchema   *analysis.K8sSchemaProvider
}

// NewHoverProvider creates a new hover provider
func NewHoverProvider(symbolTable *analysis.SymbolTable, positionMap map[string]protocol.Range) *HoverProvider {
	return &HoverProvider{
		symbolTable: symbolTable,
		positionMap: positionMap,
		k8sSchema:   analysis.NewK8sSchemaProvider(),
	}
}

// ProvideHover returns hover information for the given position
func (hp *HoverProvider) ProvideHover(content string, position protocol.Position) *protocol.Hover {
	if hp.symbolTable == nil {
		return nil
	}

	// Analyze context to determine what we're hovering over
	ctx := analysis.AnalyzeCompletionContext(content, position, hp.symbolTable)

	if ctx.InCEL {
		// Extract the full path expression at cursor position
		pathExpr, segmentRanges := hp.extractPathExpressionAtPosition(content, position)
		if pathExpr == "" {
			return nil
		}

		// Find which segment we're hovering over
		segments := strings.Split(pathExpr, ".")
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

		segment := segments[segmentIndex]

		// Check if this is a CEL function (e.g., hash.fnv64a, random.seededString)
		// Try full path first (for namespaced functions like hash.fnv64a)
		if celFunc := analysis.GetCELFunction(pathExpr); celFunc != nil {
			return hp.createCELFunctionHover(celFunc)
		}
		// Try just the segment (for top-level functions like omit, size)
		if celFunc := analysis.GetCELFunction(segment); celFunc != nil {
			return hp.createCELFunctionHover(celFunc)
		}

		// Handle first segment (resource ID, schema, self)
		if segmentIndex == 0 {
			if segment == "schema" {
				return hp.createSchemaHover()
			}
			if segment == "self" {
				return &protocol.Hover{
					Contents: protocol.MarkupContent{
						Kind:  protocol.MarkupKindMarkdown,
						Value: "**self** - Reference to the current resource\n\nUsed in `readyWhen` and `includeWhen` conditions to reference the current resource's fields.",
					},
				}
			}
			if symbol := hp.symbolTable.LookupResource(segment); symbol != nil {
				return hp.createResourceHover(symbol)
			}
			return nil
		}

		// Handle schema.spec.fieldName paths
		if segments[0] == "schema" && segmentIndex >= 2 {
			fieldName := segment
			return hp.createSchemaFieldHover(fieldName)
		}

		// Handle k8s resource field paths (e.g., deployment.metadata.labels)
		if symbol := hp.symbolTable.LookupResource(segments[0]); symbol != nil {
			fieldPath := strings.Join(segments[1:segmentIndex+1], ".")
			return hp.createK8sFieldHover(symbol, fieldPath, segment)
		}
	}

	return nil
}

// createResourceHover creates hover content for a resource symbol
func (hp *HoverProvider) createResourceHover(symbol *analysis.ResourceSymbol) *protocol.Hover {
	var content strings.Builder

	// Title
	content.WriteString(fmt.Sprintf("# Resource: `%s`\n\n", symbol.ID))

	// Type
	content.WriteString(fmt.Sprintf("**Type:** %s\n\n", symbol.Type))

	// External reference details
	if symbol.ExternalRef != nil {
		content.WriteString("## External Reference\n\n")
		content.WriteString(fmt.Sprintf("- **API Version:** `%s`\n", symbol.ExternalRef.APIVersion))
		content.WriteString(fmt.Sprintf("- **Kind:** `%s`\n", symbol.ExternalRef.Kind))

		if symbol.ExternalRef.Metadata.Name != "" {
			content.WriteString(fmt.Sprintf("- **Name:** `%s`\n", symbol.ExternalRef.Metadata.Name))
		}

		if symbol.ExternalRef.Metadata.Name != "" {
			content.WriteString("- **Selector:** Uses label selector\n")
		}

		if symbol.ExternalRef.Metadata.Namespace != "" {
			content.WriteString(fmt.Sprintf("- **Namespace:** `%s`\n", symbol.ExternalRef.Metadata.Namespace))
		}
		content.WriteString("\n")
	}

	// Dependencies
	if len(symbol.Dependencies) > 0 {
		content.WriteString("## Dependencies\n\n")
		content.WriteString("This resource depends on:\n")
		for _, dep := range symbol.Dependencies {
			content.WriteString(fmt.Sprintf("- `%s`\n", dep))
		}
		content.WriteString("\n")
	}

	// Dependents (resources that depend on this one)
	dependents := hp.findDependents(symbol.ID)
	if len(dependents) > 0 {
		content.WriteString("## Used By\n\n")
		content.WriteString("The following resources depend on this resource:\n")
		for _, dep := range dependents {
			content.WriteString(fmt.Sprintf("- `%s`\n", dep))
		}
		content.WriteString("\n")
	}

	// ForEach iterators
	if len(symbol.Iterators) > 0 {
		content.WriteString("## ForEach Iterators\n\n")
		content.WriteString("This is a collection resource with iterators:\n")
		for _, iter := range symbol.Iterators {
			content.WriteString(fmt.Sprintf("- `%s`\n", iter))
		}
		content.WriteString("\n")
	}

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: content.String(),
		},
	}
}

// createSchemaHover creates hover content for the schema identifier
func (hp *HoverProvider) createSchemaHover() *protocol.Hover {
	var content strings.Builder

	content.WriteString("# Schema\n\n")
	content.WriteString("Access to the instance specification fields.\n\n")

	if hp.symbolTable.Schema != nil {
		content.WriteString(fmt.Sprintf("**Kind:** `%s`\n\n", hp.symbolTable.Schema.Kind))
		content.WriteString(fmt.Sprintf("**API Version:** `%s`\n\n", hp.symbolTable.Schema.APIVersion))

		if hp.symbolTable.Schema.Group != "" {
			content.WriteString(fmt.Sprintf("**Group:** `%s`\n\n", hp.symbolTable.Schema.Group))
		}

		content.WriteString("Use `schema.spec.<field>` to access instance spec fields defined in the RGD schema.\n")
	}

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: content.String(),
		},
	}
}

// createSchemaFieldHover creates hover content for schema.spec.fieldName
func (hp *HoverProvider) createSchemaFieldHover(fieldName string) *protocol.Hover {
	if hp.symbolTable.Schema == nil || hp.symbolTable.Schema.SpecFields == nil {
		return nil
	}

	field, ok := hp.symbolTable.Schema.SpecFields[fieldName]
	if !ok {
		return nil
	}

	var content strings.Builder
	content.WriteString(fmt.Sprintf("# `%s`\n\n", fieldName))
	content.WriteString("**Schema field**\n\n")

	if field.Type != "" && field.Type != "any" {
		content.WriteString(fmt.Sprintf("**Type:** `%s`\n\n", field.Type))
	}

	// TODO: Add default value, validation constraints when we parse SimpleSchema more deeply

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: content.String(),
		},
	}
}

// findDependents finds all resources that depend on the given resource ID
func (hp *HoverProvider) findDependents(resourceID string) []string {
	var dependents []string

	for id, symbol := range hp.symbolTable.Resources {
		for _, dep := range symbol.Dependencies {
			if dep == resourceID {
				dependents = append(dependents, id)
				break
			}
		}
	}

	return dependents
}

// extractPathExpressionAtPosition extracts the full path expression at cursor position
// Returns the path string and a slice of ranges for each segment
func (hp *HoverProvider) extractPathExpressionAtPosition(content string, position protocol.Position) (string, []protocol.Range) {
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
	start := col
	for start > 0 {
		c := line[start-1]
		if !isIdentifierChar(c) && c != '.' {
			break
		}
		start--
	}

	// Find the end of the path expression
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

		currentPos = segmentEnd + 1 // +1 for the dot
	}

	return pathExpr, ranges
}

// createK8sFieldHover creates hover content for k8s resource field paths
// e.g., deployment.metadata.labels, service.spec.ports
func (hp *HoverProvider) createK8sFieldHover(symbol *analysis.ResourceSymbol, fieldPath string, fieldName string) *protocol.Hover {
	var content strings.Builder

	content.WriteString(fmt.Sprintf("# `%s`\n\n", fieldName))

	// Show the resource context
	content.WriteString(fmt.Sprintf("**In:** `%s` (", symbol.ID))
	if symbol.K8sKind != "" {
		content.WriteString(fmt.Sprintf("%s", symbol.K8sKind))
	} else {
		content.WriteString("resource")
	}
	content.WriteString(")\n\n")

	// Check if we have documentation for this field path
	if doc := analysis.GetFieldDocs(fieldPath); doc != "" {
		content.WriteString(fmt.Sprintf("**Description:** %s\n\n", doc))
	}

	// Check if this field is defined in the template (concrete field)
	parentPath := ""
	if lastDot := strings.LastIndex(fieldPath, "."); lastDot != -1 {
		parentPath = fieldPath[:lastDot]
	}

	if parentPath != "" {
		if concreteKeys, ok := symbol.ConcreteFields[parentPath]; ok {
			hasField := false
			for _, key := range concreteKeys {
				if key == fieldName {
					hasField = true
					break
				}
			}
			if hasField {
				content.WriteString("**Defined in template:** Yes\n\n")
				content.WriteString(fmt.Sprintf("*Ctrl+Click to jump to definition in `%s` template*\n\n", symbol.ID))
			}
		}
	}

	// Show type info from k8s schema if available
	if symbol.K8sKind != "" && hp.k8sSchema != nil {
		fields := hp.k8sSchema.GetFields(symbol.K8sKind, fieldPath)
		if len(fields) > 0 {
			content.WriteString("**Type:** object\n\n")
			content.WriteString("**Available fields:**\n")
			for i, f := range fields {
				if i < 5 { // Show first 5 fields
					content.WriteString(fmt.Sprintf("- `%s`\n", f))
				}
			}
			if len(fields) > 5 {
				content.WriteString(fmt.Sprintf("- ... and %d more\n", len(fields)-5))
			}
		} else {
			// Leaf field (no subfields)
			// Try to infer type from field name
			fieldType := inferFieldType(fieldName, fieldPath)
			if fieldType != "" {
				content.WriteString(fmt.Sprintf("**Type:** %s\n\n", fieldType))
			}
		}
	}

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: content.String(),
		},
	}
}

// inferFieldType tries to guess the type of a field from its name and path
func inferFieldType(fieldName, fieldPath string) string {
	// Common patterns
	switch fieldName {
	case "name", "namespace", "image", "type", "kind", "apiVersion":
		return "string"
	case "replicas", "port", "targetPort", "generation":
		return "integer"
	case "labels", "annotations", "matchLabels", "selector", "data":
		return "map[string]string"
	case "containers", "volumes", "ports", "matchExpressions":
		return "array"
	case "paused", "publishNotReadyAddresses":
		return "boolean"
	}

	// Pattern-based inference
	if strings.HasSuffix(fieldName, "Timestamp") {
		return "time.Time"
	}
	if strings.HasSuffix(fieldName, "s") && !strings.HasSuffix(fieldName, "Status") {
		// Plural often means array
		return "array"
	}

	return ""
}

// createCELFunctionHover creates hover content for CEL functions
func (hp *HoverProvider) createCELFunctionHover(fn *analysis.CELFunction) *protocol.Hover {
	var content strings.Builder

	content.WriteString(fmt.Sprintf("# `%s`\n\n", fn.Name))
	content.WriteString("**CEL Function**\n\n")

	// Signature
	content.WriteString(fmt.Sprintf("```cel\n%s\n```\n\n", fn.Signature))

	// Description
	if fn.Description != "" {
		content.WriteString(fmt.Sprintf("%s\n\n", fn.Description))
	}

	// Return type
	if fn.Returns != "" {
		content.WriteString(fmt.Sprintf("**Returns:** `%s`\n\n", fn.Returns))
	}

	// Example
	if fn.Example != "" {
		content.WriteString("**Example:**\n```cel\n")
		content.WriteString(fn.Example)
		content.WriteString("\n```\n\n")
	}

	// Category badge
	categoryLabel := strings.TrimPrefix(fn.Category, "kro-")
	if categoryLabel != fn.Category {
		content.WriteString(fmt.Sprintf("*Kro custom function (%s)*", categoryLabel))
	}

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: content.String(),
		},
	}
}

