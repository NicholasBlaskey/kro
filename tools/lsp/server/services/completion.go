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
	"sort"
	"strings"

	"github.com/kro-run/kro/tools/lsp/server/analysis"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// CompletionProvider provides auto-completion for RGD files
type CompletionProvider struct {
	symbolTable   *analysis.SymbolTable
	positionMap   map[string]protocol.Range
	k8sSchema     *analysis.K8sSchemaProvider
}

// NewCompletionProvider creates a new completion provider
func NewCompletionProvider(symbolTable *analysis.SymbolTable, positionMap map[string]protocol.Range) *CompletionProvider {
	return &CompletionProvider{
		symbolTable: symbolTable,
		positionMap: positionMap,
		k8sSchema:   analysis.NewK8sSchemaProvider(),
	}
}

// ProvideCompletions returns completion items for the given position
func (cp *CompletionProvider) ProvideCompletions(content string, position protocol.Position) []protocol.CompletionItem {
	if cp.symbolTable == nil {
		return nil
	}

	// Analyze what kind of completion is needed
	ctx := analysis.AnalyzeCompletionContext(content, position, cp.symbolTable)

	switch ctx.Type {
	case analysis.CompletionContextResource:
		return cp.completeResourceIDs(ctx.Prefix, position)
	case analysis.CompletionContextField:
		return cp.completeFields(ctx.ResourceID, ctx.Prefix, position)
	case analysis.CompletionContextFunction:
		return cp.completeFunctions(ctx.Prefix, position)
	case analysis.CompletionContextYAMLKey:
		return cp.completeYAMLKeys(ctx.Prefix, position)
	default:
		return nil
	}
}

// completeResourceIDs provides completions for resource IDs
// Also includes CEL functions since we're at the root level of a CEL expression
func (cp *CompletionProvider) completeResourceIDs(prefix string, position protocol.Position) []protocol.CompletionItem {
	var items []protocol.CompletionItem

	// Calculate the range to replace (the prefix that was typed)
	replaceRange := protocol.Range{
		Start: protocol.Position{
			Line:      position.Line,
			Character: position.Character - uint32(len(prefix)),
		},
		End: position,
	}

	// Add all resource IDs
	for id, symbol := range cp.symbolTable.Resources {
		if prefix == "" || strings.HasPrefix(id, prefix) {
			detail := fmt.Sprintf("%s resource", symbol.Type)
			if len(symbol.Dependencies) > 0 {
				detail += fmt.Sprintf(" (depends on: %s)", strings.Join(symbol.Dependencies, ", "))
			}

			insertTextFormat := protocol.InsertTextFormatPlainText
			item := protocol.CompletionItem{
				Label:  id,
				Kind:   completionItemKindPtr(protocol.CompletionItemKindVariable),
				Detail: stringPtr(detail),
				Documentation: &protocol.MarkupContent{
					Kind:  protocol.MarkupKindMarkdown,
					Value: cp.formatResourceDocumentation(symbol),
				},
				InsertText:       stringPtr(id),
				InsertTextFormat: &insertTextFormat,
				TextEdit: &protocol.TextEdit{
					Range:   replaceRange,
					NewText: id,
				},
				SortText:   stringPtr("1_" + id), // Prefix to prioritize
				FilterText: stringPtr(id),
				Preselect:  boolPtrHelper(strings.HasPrefix(id, prefix) && prefix != ""),
			}
			items = append(items, item)
		}
	}

	// Add special identifiers
	specialIDs := []struct {
		id     string
		detail string
		doc    string
	}{
		{"schema", "Instance schema", "Access the instance spec fields defined in the schema"},
		{"self", "Current resource", "Reference to the current resource (for readyWhen/includeWhen)"},
	}

	for _, special := range specialIDs {
		if prefix == "" || strings.HasPrefix(special.id, prefix) {
			insertTextFormat := protocol.InsertTextFormatPlainText
			items = append(items, protocol.CompletionItem{
				Label:  special.id,
				Kind:   completionItemKindPtr(protocol.CompletionItemKindKeyword),
				Detail: stringPtr(special.detail),
				Documentation: &protocol.MarkupContent{
					Kind:  protocol.MarkupKindMarkdown,
					Value: special.doc,
				},
				InsertText:       stringPtr(special.id),
				InsertTextFormat: &insertTextFormat,
				TextEdit: &protocol.TextEdit{
					Range:   replaceRange,
					NewText: special.id,
				},
				SortText:   stringPtr("0_" + special.id), // Highest priority
				FilterText: stringPtr(special.id),
				Preselect:  boolPtrHelper(strings.HasPrefix(special.id, prefix) && prefix != ""),
			})
		}
	}

	// Add CEL functions at the root level too
	// This allows completing functions like hash(), omit(), size() etc. alongside resources
	celProvider := NewCELCompletionProvider(cp.symbolTable)
	celFunctions := celProvider.ProvideCELFunctionCompletions(prefix, position)
	items = append(items, celFunctions...)

	// Sort by sortText for predictable ordering
	sort.Slice(items, func(i, j int) bool {
		si := ""
		sj := ""
		if items[i].SortText != nil {
			si = *items[i].SortText
		}
		if items[j].SortText != nil {
			sj = *items[j].SortText
		}
		return si < sj
	})

	return items
}

// completeFields provides completions for fields on a resource
func (cp *CompletionProvider) completeFields(fieldPath string, prefix string, position protocol.Position) []protocol.CompletionItem {
	var items []protocol.CompletionItem

	insertTextFormat := protocol.InsertTextFormatPlainText

	// Calculate the range to replace (the prefix that was typed)
	replaceRange := protocol.Range{
		Start: protocol.Position{
			Line:      position.Line,
			Character: position.Character - uint32(len(prefix)),
		},
		End: position,
	}

	// TYPE-AWARE COMPLETION: Infer the type of the expression before the dot
	// and offer methods appropriate for that type
	exprType := analysis.InferCELExpressionType(fieldPath, cp.symbolTable)
	if exprType != analysis.CELTypeUnknown {
		// Get methods available for this type
		methods := analysis.GetMethodsForType(exprType)
		if len(methods) > 0 {
			for _, method := range methods {
				if prefix == "" || strings.HasPrefix(method, prefix) {
					// Get full function info from catalog for better docs
					fn := analysis.GetCELFunction(method)
					if fn != nil {
						doc := fn.Description
						if fn.Example != "" {
							doc += "\n\nExample:\n```cel\n" + fn.Example + "\n```"
						}

						items = append(items, protocol.CompletionItem{
							Label:            method,
							Kind:             completionItemKindPtr(protocol.CompletionItemKindMethod),
							Detail:           stringPtr(fn.Signature),
							Documentation: &protocol.MarkupContent{
								Kind:  protocol.MarkupKindMarkdown,
								Value: doc,
							},
							InsertText:       stringPtr(method),
							InsertTextFormat: &insertTextFormat,
							TextEdit: &protocol.TextEdit{
								Range:   replaceRange,
								NewText: method,
							},
							SortText: stringPtr("0_" + method), // Prioritize type-aware methods
						})
					} else {
						// Method not in catalog, add basic completion
						items = append(items, protocol.CompletionItem{
							Label:            method,
							Kind:             completionItemKindPtr(protocol.CompletionItemKindMethod),
							Detail:           stringPtr(fmt.Sprintf("%s method", exprType)),
							InsertText:       stringPtr(method),
							InsertTextFormat: &insertTextFormat,
							TextEdit: &protocol.TextEdit{
								Range:   replaceRange,
								NewText: method,
							},
							SortText: stringPtr("0_" + method),
						})
					}
				}
			}

			// Don't return early - we might also have concrete fields to show
			// (especially for maps with concrete keys like labels.app, labels.dns)
		}
	}

	// Check for concrete fields in the template (e.g., deployment.metadata.labels.app)
	// This comes AFTER type-aware methods so both can be shown together
	concreteRootResource := fieldPath
	if dotIdx := strings.Index(fieldPath, "."); dotIdx != -1 {
		concreteRootResource = fieldPath[:dotIdx]
	}

	if resource := cp.symbolTable.LookupResource(concreteRootResource); resource != nil {
		// Extract the field path after the resource ID
		fieldPathSuffix := strings.TrimPrefix(fieldPath, concreteRootResource+".")

		// Check if we have concrete fields for this path
		if concreteKeys, ok := resource.ConcreteFields[fieldPathSuffix]; ok && len(concreteKeys) > 0 {
			for _, fieldName := range concreteKeys {
				if prefix == "" || strings.HasPrefix(fieldName, prefix) {
					items = append(items, protocol.CompletionItem{
						Label:            fieldName,
						Kind:             completionItemKindPtr(protocol.CompletionItemKindField),
						Detail:           stringPtr(fmt.Sprintf("Defined in %s template", concreteRootResource)),
						InsertText:       stringPtr(fieldName),
						InsertTextFormat: &insertTextFormat,
						TextEdit: &protocol.TextEdit{
							Range:   replaceRange,
							NewText: fieldName,
						},
						SortText: stringPtr("0_" + fieldName), // Prioritize concrete fields same as methods
					})
				}
			}
		}
	}

	// If we have items from type-aware methods or concrete fields, and type is not object/unknown, return now
	if len(items) > 0 && exprType != analysis.CELTypeUnknown && exprType != analysis.CELTypeObject {
		return items
	}

	// If this is a method call chain (contains parentheses), don't fall back to k8s field completions
	// Method call results are values (string, bool, int, etc.), not k8s objects with fields
	if strings.Contains(fieldPath, "(") {
		return items // Could be empty if the type has no methods (e.g., bool, int)
	}

	// Handle nested paths like "schema.spec"
	if fieldPath == "schema.spec" {
		// Complete with actual spec field names from the schema
		if cp.symbolTable.Schema != nil && cp.symbolTable.Schema.SpecFields != nil {
			for fieldName := range cp.symbolTable.Schema.SpecFields {
				if prefix == "" || strings.HasPrefix(fieldName, prefix) {
					items = append(items, protocol.CompletionItem{
						Label:            fieldName,
						Kind:             completionItemKindPtr(protocol.CompletionItemKindField),
						Detail:           stringPtr("Schema spec field"),
						InsertText:       stringPtr(fieldName),
						InsertTextFormat: &insertTextFormat,
						TextEdit: &protocol.TextEdit{
							Range:   replaceRange,
							NewText: fieldName,
						},
					})
				}
			}
		}
		return items
	}

	// Handle special cases
	if fieldPath == "schema" {
		// Complete with spec fields from schema
		if cp.symbolTable.Schema != nil {
			items = append(items, protocol.CompletionItem{
				Label:            "spec",
				Kind:             completionItemKindPtr(protocol.CompletionItemKindField),
				Detail:           stringPtr("Instance spec fields"),
				InsertText:       stringPtr("spec"),
				InsertTextFormat: &insertTextFormat,
				TextEdit: &protocol.TextEdit{
					Range:   replaceRange,
					NewText: "spec",
				},
			})
		}
		return items
	}

	// If the path goes deeper than schema.spec.fieldName, don't offer completions
	// (e.g., schema.spec.name is a leaf value, schema.spec.name.something doesn't make sense)
	if strings.HasPrefix(fieldPath, "schema.spec.") {
		// This is a leaf field, no completions
		return items
	}

	// Extract the root resource ID from the path
	rootResource := fieldPath
	dotCount := strings.Count(fieldPath, ".")
	if dotIdx := strings.Index(fieldPath, "."); dotIdx != -1 {
		rootResource = fieldPath[:dotIdx]
	}

	// Check if this is a known resource
	resource, exists := cp.symbolTable.Resources[rootResource]
	if !exists {
		// Unknown resource, no completions
		return items
	}

	// For nested paths (e.g., "deployment.spec.", "service.spec.ports."), use concrete fields first
	if dotCount > 0 {
		// Extract the field path after the resource ID (e.g., "spec" from "deployment.spec")
		fieldPathSuffix := strings.TrimPrefix(fieldPath, rootResource+".")

		// First, check if we have concrete fields from the template
		if concreteKeys, ok := resource.ConcreteFields[fieldPathSuffix]; ok && len(concreteKeys) > 0 {
			for _, fieldName := range concreteKeys {
				if prefix == "" || strings.HasPrefix(fieldName, prefix) {
					items = append(items, protocol.CompletionItem{
						Label:            fieldName,
						Kind:             completionItemKindPtr(protocol.CompletionItemKindField),
						Detail:           stringPtr(fmt.Sprintf("Defined in %s template", concreteRootResource)),
						InsertText:       stringPtr(fieldName),
						InsertTextFormat: &insertTextFormat,
						TextEdit: &protocol.TextEdit{
							Range:   replaceRange,
							NewText: fieldName,
						},
						SortText: stringPtr("0_" + fieldName), // Prioritize template fields
					})
				}
			}
			// Return early - concrete fields take precedence over k8s schema
			return items
		}

		// Fall back to K8s schema fields if we know the kind
		if resource.K8sKind != "" && cp.k8sSchema != nil {
			k8sFields := cp.k8sSchema.GetFields(resource.K8sKind, fieldPathSuffix)
			for _, fieldName := range k8sFields {
				if prefix == "" || strings.HasPrefix(fieldName, prefix) {
					items = append(items, protocol.CompletionItem{
						Label:            fieldName,
						Kind:             completionItemKindPtr(protocol.CompletionItemKindField),
						Detail:           stringPtr(fmt.Sprintf("%s.%s field", resource.K8sKind, fieldPathSuffix)),
						InsertText:       stringPtr(fieldName),
						InsertTextFormat: &insertTextFormat,
						TextEdit: &protocol.TextEdit{
							Range:   replaceRange,
							NewText: fieldName,
						},
					})
				}
			}
		}
		return items
	}

	// Standard Kubernetes resource fields (only for first-level resource access)
	standardFields := []struct {
		name   string
		detail string
	}{
		{"metadata", "Resource metadata (name, namespace, labels, annotations)"},
		{"spec", "Resource specification"},
		{"status", "Resource status"},
	}

	for _, field := range standardFields {
		if prefix == "" || strings.HasPrefix(field.name, prefix) {
			items = append(items, protocol.CompletionItem{
				Label:            field.name,
				Kind:             completionItemKindPtr(protocol.CompletionItemKindField),
				Detail:           stringPtr(field.detail),
				InsertText:       stringPtr(field.name),
				InsertTextFormat: &insertTextFormat,
				TextEdit: &protocol.TextEdit{
					Range:   replaceRange,
					NewText: field.name,
				},
			})
		}
	}

	return items
}

// completeFunctions provides completions for CEL functions
// Delegates to CELCompletionProvider for comprehensive CEL function support
func (cp *CompletionProvider) completeFunctions(prefix string, position protocol.Position) []protocol.CompletionItem {
	// Use CEL completion provider for better CEL function support
	celProvider := NewCELCompletionProvider(cp.symbolTable)
	return celProvider.ProvideCELFunctionCompletions(prefix, position)
}

// completeYAMLKeys provides completions for top-level YAML keys
func (cp *CompletionProvider) completeYAMLKeys(prefix string, position protocol.Position) []protocol.CompletionItem {
	keys := []struct {
		key    string
		detail string
	}{
		{"apiVersion", "API version (kro.run/v1alpha1)"},
		{"kind", "Resource kind (ResourceGraphDefinition)"},
		{"metadata", "Resource metadata"},
		{"spec", "ResourceGraphDefinition specification"},
	}

	// Calculate the range to replace (the prefix that was typed)
	replaceRange := protocol.Range{
		Start: protocol.Position{
			Line:      position.Line,
			Character: position.Character - uint32(len(prefix)),
		},
		End: position,
	}

	var items []protocol.CompletionItem
	for _, k := range keys {
		if prefix == "" || strings.HasPrefix(k.key, prefix) {
			items = append(items, protocol.CompletionItem{
				Label:      k.key,
				Kind:       completionItemKindPtr(protocol.CompletionItemKindProperty),
				Detail:     stringPtr(k.detail),
				InsertText: stringPtr(k.key),
				TextEdit: &protocol.TextEdit{
					Range:   replaceRange,
					NewText: k.key,
				},
			})
		}
	}

	return items
}

// formatResourceDocumentation formats documentation for a resource symbol
func (cp *CompletionProvider) formatResourceDocumentation(symbol *analysis.ResourceSymbol) string {
	var doc strings.Builder

	doc.WriteString(fmt.Sprintf("**Type:** %s\n\n", symbol.Type))

	if len(symbol.Dependencies) > 0 {
		doc.WriteString("**Dependencies:**\n")
		for _, dep := range symbol.Dependencies {
			doc.WriteString(fmt.Sprintf("- `%s`\n", dep))
		}
		doc.WriteString("\n")
	}

	if len(symbol.Iterators) > 0 {
		doc.WriteString("**ForEach Iterators:**\n")
		for _, iter := range symbol.Iterators {
			doc.WriteString(fmt.Sprintf("- `%s`\n", iter))
		}
		doc.WriteString("\n")
	}

	if symbol.ExternalRef != nil {
		doc.WriteString("**External Reference:**\n")
		doc.WriteString(fmt.Sprintf("- API: `%s`\n", symbol.ExternalRef.APIVersion))
		doc.WriteString(fmt.Sprintf("- Kind: `%s`\n", symbol.ExternalRef.Kind))
	}

	return doc.String()
}

// Protocol helpers
func completionItemKindPtr(kind protocol.CompletionItemKind) *protocol.CompletionItemKind {
	return &kind
}
