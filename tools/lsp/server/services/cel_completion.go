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

	"github.com/kro-run/kro/tools/lsp/server/analysis"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// CELCompletionProvider provides completions for CEL expressions
type CELCompletionProvider struct {
	symbolTable *analysis.SymbolTable
}

// NewCELCompletionProvider creates a new CEL completion provider
func NewCELCompletionProvider(symbolTable *analysis.SymbolTable) *CELCompletionProvider {
	return &CELCompletionProvider{
		symbolTable: symbolTable,
	}
}

// ProvideCELFunctionCompletions returns completions for CEL functions
// Called when we're inside a CEL expression and need to suggest functions
func (ccp *CELCompletionProvider) ProvideCELFunctionCompletions(prefix string, position protocol.Position) []protocol.CompletionItem {
	insertTextFormat := protocol.InsertTextFormatPlainText

	// Calculate the range to replace
	replaceRange := protocol.Range{
		Start: protocol.Position{
			Line:      position.Line,
			Character: position.Character - uint32(len(prefix)),
		},
		End: position,
	}

	var items []protocol.CompletionItem

	// Get all CEL functions from the catalog
	for _, fn := range analysis.CELFunctions {
		if prefix == "" || strings.HasPrefix(fn.Name, prefix) {
			// Determine sort priority based on category
			sortPrefix := "1_" // Default: standard functions
			if strings.HasPrefix(fn.Category, "kro-") {
				sortPrefix = "0_" // Kro custom functions first
			}

			// Build documentation with description and example
			doc := fn.Description
			if fn.Example != "" {
				doc += "\n\nExample:\n```cel\n" + fn.Example + "\n```"
			}

			items = append(items, protocol.CompletionItem{
				Label:            fn.Name,
				Kind:             completionItemKindPtr(protocol.CompletionItemKindFunction),
				Detail:           stringPtr(fn.Signature),
				Documentation: &protocol.MarkupContent{
					Kind:  protocol.MarkupKindMarkdown,
					Value: doc,
				},
				InsertText:       stringPtr(fn.Name),
				InsertTextFormat: &insertTextFormat,
				TextEdit: &protocol.TextEdit{
					Range:   replaceRange,
					NewText: fn.Name,
				},
				SortText: stringPtr(sortPrefix + fn.Name),
			})
		}
	}

	return items
}

// ProvideCELOperatorCompletions returns completions for CEL operators
func (ccp *CELCompletionProvider) ProvideCELOperatorCompletions(prefix string, position protocol.Position) []protocol.CompletionItem {
	insertTextFormat := protocol.InsertTextFormatPlainText

	replaceRange := protocol.Range{
		Start: protocol.Position{
			Line:      position.Line,
			Character: position.Character - uint32(len(prefix)),
		},
		End: position,
	}

	operators := []struct {
		name   string
		detail string
	}{
		{"&&", "Logical AND"},
		{"||", "Logical OR"},
		{"!", "Logical NOT"},
		{"==", "Equality"},
		{"!=", "Inequality"},
		{"<", "Less than"},
		{"<=", "Less than or equal"},
		{">", "Greater than"},
		{">=", "Greater than or equal"},
		{"+", "Addition / String concatenation"},
		{"-", "Subtraction"},
		{"*", "Multiplication"},
		{"/", "Division"},
		{"%", "Modulo"},
		{"in", "Membership test (element in list/map)"},
		{"?", "Conditional operator (condition ? true_val : false_val)"},
	}

	var items []protocol.CompletionItem

	for _, op := range operators {
		if prefix == "" || strings.HasPrefix(op.name, prefix) {
			items = append(items, protocol.CompletionItem{
				Label:            op.name,
				Kind:             completionItemKindPtr(protocol.CompletionItemKindOperator),
				Detail:           stringPtr(op.detail),
				InsertText:       stringPtr(op.name),
				InsertTextFormat: &insertTextFormat,
				TextEdit: &protocol.TextEdit{
					Range:   replaceRange,
					NewText: op.name,
				},
			})
		}
	}

	return items
}

// ProvideCELMacroCompletions returns completions for CEL macros (list operations)
func (ccp *CELCompletionProvider) ProvideCELMacroCompletions(prefix string, position protocol.Position) []protocol.CompletionItem {
	insertTextFormat := protocol.InsertTextFormatPlainText

	replaceRange := protocol.Range{
		Start: protocol.Position{
			Line:      position.Line,
			Character: position.Character - uint32(len(prefix)),
		},
		End: position,
	}

	macros := []struct {
		name      string
		signature string
		detail    string
	}{
		{"all", "all(var, condition)", "Returns true if all elements satisfy the condition"},
		{"exists", "exists(var, condition)", "Returns true if any element satisfies the condition"},
		{"exists_one", "exists_one(var, condition)", "Returns true if exactly one element satisfies the condition"},
		{"filter", "filter(var, condition)", "Returns a new list with elements that satisfy the condition"},
		{"map", "map(var, transform)", "Returns a new list with transformed elements"},
	}

	var items []protocol.CompletionItem

	for _, macro := range macros {
		if prefix == "" || strings.HasPrefix(macro.name, prefix) {
			items = append(items, protocol.CompletionItem{
				Label:            macro.name,
				Kind:             completionItemKindPtr(protocol.CompletionItemKindMethod),
				Detail:           stringPtr(macro.signature),
				Documentation:    stringPtr(macro.detail),
				InsertText:       stringPtr(macro.name),
				InsertTextFormat: &insertTextFormat,
				TextEdit: &protocol.TextEdit{
					Range:   replaceRange,
					NewText: macro.name,
				},
			})
		}
	}

	return items
}
