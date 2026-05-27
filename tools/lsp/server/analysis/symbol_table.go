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
	"strings"

	"github.com/kubernetes-sigs/kro/api/v1alpha1"
	"github.com/kubernetes-sigs/kro/pkg/graph"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"sigs.k8s.io/yaml"
)

// SymbolTable tracks all symbols (resources, schema fields, iterators) in an RGD.
// It provides fast lookup for completions, go-to-definition, and hover information.
type SymbolTable struct {
	Schema    *SchemaSymbol
	Resources map[string]*ResourceSymbol
	Scopes    map[string]*ResourceScope
}

// SchemaSymbol represents the instance CRD schema definition
type SchemaSymbol struct {
	Kind       string
	APIVersion string
	Group      string
	SpecFields map[string]*FieldSymbol
	Position   protocol.Range
}

// ResourceSymbol represents a resource in the RGD
type ResourceSymbol struct {
	ID             string
	Type           ResourceType
	Position       protocol.Range
	Dependencies   []string
	Fields         map[string]*FieldSymbol
	Iterators      []string
	Template       interface{}
	ExternalRef    *v1alpha1.ExternalRef
	K8sKind        string            // The Kubernetes kind from the template (e.g., "Deployment", "Service")
	K8sAPIVersion  string            // The Kubernetes API version (e.g., "apps/v1", "v1")
	ArrayIndex     int               // The index in spec.resources array (for positionMap lookups)
	ConcreteFields map[string][]string // Actual keys defined in template (e.g., "metadata.labels" -> ["app", "dns"])
}

// ResourceType indicates the kind of resource
type ResourceType string

const (
	ResourceTypeTemplate   ResourceType = "template"
	ResourceTypeExternal   ResourceType = "externalRef"
	ResourceTypeCollection ResourceType = "collection"
)

// FieldSymbol represents a field in a schema or resource
type FieldSymbol struct {
	Name     string
	Type     string
	Path     string
	Position protocol.Range
}

// ResourceScope tracks the forEach iterator scope for a resource
type ResourceScope struct {
	ResourceID string
	Iterators  map[string]*IteratorSymbol
	Parent     *ResourceScope
}

// IteratorSymbol represents a forEach iterator variable
type IteratorSymbol struct {
	Name       string
	Expression string
	Position   protocol.Range
}

// NewSymbolTable creates an empty symbol table
func NewSymbolTable() *SymbolTable {
	return &SymbolTable{
		Resources: make(map[string]*ResourceSymbol),
		Scopes:    make(map[string]*ResourceScope),
	}
}

// BuildFromRGD constructs a symbol table from an RGD and its validated graph
func BuildFromRGD(rgd *v1alpha1.ResourceGraphDefinition, g *graph.Graph, positions map[string]protocol.Range) *SymbolTable {
	st := NewSymbolTable()

	// Extract schema symbol
	if rgd.Spec.Schema != nil {
		st.Schema = &SchemaSymbol{
			Kind:       rgd.Spec.Schema.Kind,
			APIVersion: rgd.Spec.Schema.APIVersion,
			Group:      rgd.Spec.Schema.Group,
			SpecFields: make(map[string]*FieldSymbol),
			Position:   positions["spec.schema"],
		}

		// Parse spec fields from SimpleSchema
		if len(rgd.Spec.Schema.Spec.Raw) > 0 {
			instanceSpec := map[string]interface{}{}
			if err := yaml.Unmarshal(rgd.Spec.Schema.Spec.Raw, &instanceSpec); err == nil {
				// Extract field names and types from the spec
				for fieldName, fieldValue := range instanceSpec {
					fieldSymbol := &FieldSymbol{
						Name: fieldName,
						Type: "any",
						Path: "spec." + fieldName,
					}

					// Parse SimpleSchema type syntax
					if typeStr, ok := fieldValue.(string); ok {
						fieldSymbol.Type = parseSimpleSchemaType(typeStr)
					}

					st.Schema.SpecFields[fieldName] = fieldSymbol
				}
			}
		}
	}

	// Extract resource symbols
	for i, res := range rgd.Spec.Resources {
		resourcePath := fmt.Sprintf("spec.resources[%d]", i)
		idPath := resourcePath + ".id"

		symbol := &ResourceSymbol{
			ID:             res.ID,
			Position:       positions[idPath],
			Fields:         make(map[string]*FieldSymbol),
			ArrayIndex:     i,
			ConcreteFields: make(map[string][]string),
		}

		// Determine resource type
		if res.ExternalRef != nil {
			symbol.Type = ResourceTypeExternal
			symbol.ExternalRef = res.ExternalRef
		} else if len(res.ForEach) > 0 {
			symbol.Type = ResourceTypeCollection
		} else {
			symbol.Type = ResourceTypeTemplate
		}

		// Extract dependencies from graph if available
		if g != nil && g.Nodes != nil {
			if node, ok := g.Nodes[res.ID]; ok {
				symbol.Dependencies = node.Meta.Dependencies
			}
		}

		// Extract K8s kind from template (Template is a runtime.RawExtension)
		if len(res.Template.Raw) > 0 {
			var templateMap map[string]interface{}
			if err := yaml.Unmarshal(res.Template.Raw, &templateMap); err == nil {
				if kind, ok := templateMap["kind"].(string); ok {
					symbol.K8sKind = kind
				}
				if apiVersion, ok := templateMap["apiVersion"].(string); ok {
					symbol.K8sAPIVersion = apiVersion
				}

				// Extract concrete fields from template structure
				extractConcreteFields(templateMap, "", symbol.ConcreteFields)
			}
		}

		// Extract forEach iterators
		if len(res.ForEach) > 0 {
			scope := &ResourceScope{
				ResourceID: res.ID,
				Iterators:  make(map[string]*IteratorSymbol),
			}

			for dimIdx, dimension := range res.ForEach {
				for iterName, iterExpr := range dimension {
					iterPath := fmt.Sprintf("%s.forEach[%d].%s", resourcePath, dimIdx, iterName)
					scope.Iterators[iterName] = &IteratorSymbol{
						Name:       iterName,
						Expression: iterExpr,
						Position:   positions[iterPath],
					}
					symbol.Iterators = append(symbol.Iterators, iterName)
				}
			}

			st.Scopes[res.ID] = scope
		}

		st.Resources[res.ID] = symbol
	}

	return st
}

// LookupResource finds a resource symbol by ID
func (st *SymbolTable) LookupResource(id string) *ResourceSymbol {
	if st == nil || st.Resources == nil {
		return nil
	}
	return st.Resources[id]
}

// GetCompletions returns all resource IDs matching the given prefix
func (st *SymbolTable) GetCompletions(prefix string) []string {
	if st == nil || st.Resources == nil {
		return nil
	}

	var matches []string
	for id := range st.Resources {
		if prefix == "" || strings.HasPrefix(id, prefix) {
			matches = append(matches, id)
		}
	}
	return matches
}

// GetIteratorsInScope returns all forEach iterators available in a resource's scope
func (st *SymbolTable) GetIteratorsInScope(resourceID string) []string {
	if st == nil || st.Scopes == nil {
		return nil
	}

	scope := st.Scopes[resourceID]
	if scope == nil {
		return nil
	}

	var iterators []string
	for name := range scope.Iterators {
		iterators = append(iterators, name)
	}
	return iterators
}

// GetAllResourceIDs returns all resource IDs in the symbol table
func (st *SymbolTable) GetAllResourceIDs() []string {
	if st == nil || st.Resources == nil {
		return nil
	}

	ids := make([]string, 0, len(st.Resources))
	for id := range st.Resources {
		ids = append(ids, id)
	}
	return ids
}

// extractConcreteFields walks a template object and extracts concrete map keys
// For example, if the template has:
//   metadata:
//     labels:
//       app: myapp
//       dns: ${...}
// It records: concreteFields["metadata.labels"] = ["app", "dns"]
func extractConcreteFields(obj interface{}, path string, concreteFields map[string][]string) {
	switch v := obj.(type) {
	case map[string]interface{}:
		// Collect keys at this level
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}

		// Store the keys for this path (if path is not empty)
		if path != "" {
			concreteFields[path] = keys
		}

		// Recurse into nested objects
		for key, value := range v {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			extractConcreteFields(value, childPath, concreteFields)
		}

	case []interface{}:
		// For arrays, we could extract fields from each element
		// For now, skip arrays to keep it simple
		// (most CEL expressions access array elements explicitly, not their keys)
	}
}

// parseSimpleSchemaType parses Kro's SimpleSchema type syntax into a normalized type string
// Examples:
//   "string" -> "string"
//   "integer" -> "integer"
//   "boolean" -> "boolean"
//   "string | default='hello'" -> "string (default: 'hello')"
//   "integer | min=1 | max=100" -> "integer (min: 1, max: 100)"
//   "[string]" -> "array[string]"
//   "{string: integer}" -> "map[string]integer"
func parseSimpleSchemaType(typeStr string) string {
	// Simple types without modifiers
	switch typeStr {
	case "string", "integer", "boolean", "number", "float":
		return typeStr
	}

	// Check for array syntax: [type]
	if len(typeStr) > 2 && typeStr[0] == '[' && typeStr[len(typeStr)-1] == ']' {
		innerType := typeStr[1 : len(typeStr)-1]
		return "array[" + innerType + "]"
	}

	// Check for map syntax: {keyType: valueType}
	if len(typeStr) > 2 && typeStr[0] == '{' && typeStr[len(typeStr)-1] == '}' {
		// Simple parse for {string: integer} pattern
		if strings.Contains(typeStr, ":") {
			parts := strings.SplitN(typeStr[1:len(typeStr)-1], ":", 2)
			if len(parts) == 2 {
				keyType := strings.TrimSpace(parts[0])
				valueType := strings.TrimSpace(parts[1])
				return fmt.Sprintf("map[%s]%s", keyType, valueType)
			}
		}
	}

	// Check for type with modifiers: "string | default='hello' | minLength=5"
	if strings.Contains(typeStr, "|") {
		parts := strings.Split(typeStr, "|")
		baseType := strings.TrimSpace(parts[0])

		// Parse modifiers
		var modifiers []string
		for _, part := range parts[1:] {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "default=") {
				defaultVal := strings.TrimPrefix(part, "default=")
				modifiers = append(modifiers, "default: "+defaultVal)
			} else if strings.HasPrefix(part, "min=") {
				minVal := strings.TrimPrefix(part, "min=")
				modifiers = append(modifiers, "min: "+minVal)
			} else if strings.HasPrefix(part, "max=") {
				maxVal := strings.TrimPrefix(part, "max=")
				modifiers = append(modifiers, "max: "+maxVal)
			} else if strings.HasPrefix(part, "minLength=") {
				minLen := strings.TrimPrefix(part, "minLength=")
				modifiers = append(modifiers, "minLength: "+minLen)
			} else if strings.HasPrefix(part, "maxLength=") {
				maxLen := strings.TrimPrefix(part, "maxLength=")
				modifiers = append(modifiers, "maxLength: "+maxLen)
			} else if strings.HasPrefix(part, "pattern=") {
				pattern := strings.TrimPrefix(part, "pattern=")
				modifiers = append(modifiers, "pattern: "+pattern)
			} else if strings.HasPrefix(part, "enum=") {
				enumVals := strings.TrimPrefix(part, "enum=")
				modifiers = append(modifiers, "enum: "+enumVals)
			}
		}

		if len(modifiers) > 0 {
			return baseType + " (" + strings.Join(modifiers, ", ") + ")"
		}
		return baseType
	}

	// Fallback - return as-is
	return typeStr
}
