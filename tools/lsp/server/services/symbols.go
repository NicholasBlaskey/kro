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

	"github.com/kro-run/kro/tools/lsp/server/analysis"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// SymbolProvider provides document symbols for RGD files
type SymbolProvider struct {
	symbolTable *analysis.SymbolTable
	positionMap map[string]protocol.Range
}

// NewSymbolProvider creates a new symbol provider
func NewSymbolProvider(symbolTable *analysis.SymbolTable, positionMap map[string]protocol.Range) *SymbolProvider {
	return &SymbolProvider{
		symbolTable: symbolTable,
		positionMap: positionMap,
	}
}

// ProvideDocumentSymbols returns document symbols for the outline view
func (sp *SymbolProvider) ProvideDocumentSymbols() []protocol.DocumentSymbol {
	if sp.symbolTable == nil {
		return nil
	}

	var symbols []protocol.DocumentSymbol

	// Add schema as top-level symbol
	if sp.symbolTable.Schema != nil {
		schemaSymbol := sp.createSchemaSymbol()
		symbols = append(symbols, schemaSymbol)
	}

	// Add resources as top-level symbols
	for _, resource := range sp.symbolTable.Resources {
		resourceSymbol := sp.createResourceSymbol(resource)
		symbols = append(symbols, resourceSymbol)
	}

	return symbols
}

// createSchemaSymbol creates a document symbol for the schema
func (sp *SymbolProvider) createSchemaSymbol() protocol.DocumentSymbol {
	schema := sp.symbolTable.Schema
	detail := fmt.Sprintf("%s (%s)", schema.Kind, schema.APIVersion)

	// Get position from position map
	position := sp.positionMap["spec.schema"]
	if isZeroRange(position) {
		// Fallback to schema position from symbol table
		position = schema.Position
	}

	return protocol.DocumentSymbol{
		Name:           "Schema",
		Detail:         &detail,
		Kind:           protocol.SymbolKindStruct,
		Range:          position,
		SelectionRange: position,
	}
}

// createResourceSymbol creates a document symbol for a resource
func (sp *SymbolProvider) createResourceSymbol(resource *analysis.ResourceSymbol) protocol.DocumentSymbol {
	var detail string
	var kind protocol.SymbolKind

	switch resource.Type {
	case analysis.ResourceTypeTemplate:
		detail = "Template Resource"
		kind = protocol.SymbolKindObject
	case analysis.ResourceTypeExternal:
		detail = fmt.Sprintf("External: %s", resource.ExternalRef.Kind)
		kind = protocol.SymbolKindModule
	case analysis.ResourceTypeCollection:
		detail = "Collection (forEach)"
		kind = protocol.SymbolKindArray
	default:
		detail = string(resource.Type)
		kind = protocol.SymbolKindObject
	}

	// Add dependency info to detail
	if len(resource.Dependencies) > 0 {
		detail += fmt.Sprintf(" (depends on: %d)", len(resource.Dependencies))
	}

	symbol := protocol.DocumentSymbol{
		Name:           resource.ID,
		Detail:         &detail,
		Kind:           kind,
		Range:          resource.Position,
		SelectionRange: resource.Position,
	}

	// Add forEach iterators as children for collection resources
	if len(resource.Iterators) > 0 {
		if scope, ok := sp.symbolTable.Scopes[resource.ID]; ok {
			var children []protocol.DocumentSymbol
			for _, iterName := range resource.Iterators {
				if iter, ok := scope.Iterators[iterName]; ok {
					iterDetail := "forEach iterator"
					children = append(children, protocol.DocumentSymbol{
						Name:           iterName,
						Detail:         &iterDetail,
						Kind:           protocol.SymbolKindVariable,
						Range:          iter.Position,
						SelectionRange: iter.Position,
					})
				}
			}
			symbol.Children = children
		}
	}

	return symbol
}

