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

import "testing"

func TestInferCELExpressionType(t *testing.T) {
	// Create a symbol table with test data
	symbolTable := NewSymbolTable()
	symbolTable.Schema = &SchemaSymbol{
		SpecFields: map[string]*FieldSymbol{
			"name":        {Name: "name", Type: "string"},
			"fullDNSName": {Name: "fullDNSName", Type: "string"},
			"replicas":    {Name: "replicas", Type: "integer"},
			"enabled":     {Name: "enabled", Type: "boolean"},
			"items":       {Name: "items", Type: "array[string]"},
			"config":      {Name: "config", Type: "map[string]string"},
		},
	}

	// Add a test resource
	symbolTable.Resources = map[string]*ResourceSymbol{
		"deployment": {
			ID:      "deployment",
			K8sKind: "Deployment",
		},
	}

	tests := []struct {
		name     string
		expr     string
		expected CELType
	}{
		// Schema field access
		{
			name:     "schema.spec.name is string",
			expr:     "schema.spec.name",
			expected: CELTypeString,
		},
		{
			name:     "schema.spec.fullDNSName is string",
			expr:     "schema.spec.fullDNSName",
			expected: CELTypeString,
		},
		{
			name:     "schema.spec.replicas is int",
			expr:     "schema.spec.replicas",
			expected: CELTypeInt,
		},
		{
			name:     "schema.spec.enabled is bool",
			expr:     "schema.spec.enabled",
			expected: CELTypeBool,
		},
		{
			name:     "schema.spec.items is list",
			expr:     "schema.spec.items",
			expected: CELTypeList,
		},
		{
			name:     "schema.spec.config is map",
			expr:     "schema.spec.config",
			expected: CELTypeMap,
		},

		// String method returns
		{
			name:     "string.split returns list",
			expr:     "schema.spec.fullDNSName.split('.')",
			expected: CELTypeList,
		},
		{
			name:     "string.upperAscii returns string",
			expr:     "schema.spec.name.upperAscii()",
			expected: CELTypeString,
		},
		{
			name:     "string.lowerAscii returns string",
			expr:     "schema.spec.name.lowerAscii()",
			expected: CELTypeString,
		},
		{
			name:     "string.substring returns string",
			expr:     "schema.spec.name.substring(0, 5)",
			expected: CELTypeString,
		},
		{
			name:     "string.replace returns string",
			expr:     "schema.spec.name.replace('a', 'b')",
			expected: CELTypeString,
		},
		{
			name:     "string.trim returns string",
			expr:     "schema.spec.name.trim()",
			expected: CELTypeString,
		},
		{
			name:     "string.contains returns bool",
			expr:     "schema.spec.name.contains('test')",
			expected: CELTypeBool,
		},
		{
			name:     "string.startsWith returns bool",
			expr:     "schema.spec.name.startsWith('test')",
			expected: CELTypeBool,
		},
		{
			name:     "string.endsWith returns bool",
			expr:     "schema.spec.name.endsWith('test')",
			expected: CELTypeBool,
		},
		{
			name:     "string.matches returns bool",
			expr:     "schema.spec.name.matches('[a-z]+')",
			expected: CELTypeBool,
		},

		// List method returns
		{
			name:     "list.filter returns list",
			expr:     "schema.spec.items.filter(x, x.startsWith('a'))",
			expected: CELTypeList,
		},
		{
			name:     "list.map returns list",
			expr:     "schema.spec.items.map(x, x.upperAscii())",
			expected: CELTypeList,
		},
		{
			name:     "list.all returns bool",
			expr:     "schema.spec.items.all(x, size(x) > 0)",
			expected: CELTypeBool,
		},
		{
			name:     "list.exists returns bool",
			expr:     "schema.spec.items.exists(x, x.contains('test'))",
			expected: CELTypeBool,
		},

		// List indexing
		{
			name:     "list[index] returns element type (string)",
			expr:     "schema.spec.fullDNSName.split('.')[0]",
			expected: CELTypeString,
		},

		// Kro function returns
		{
			name:     "hash.fnv64a returns bytes",
			expr:     "hash.fnv64a('test')",
			expected: CELTypeBytes,
		},
		{
			name:     "hash.sha256 returns bytes",
			expr:     "hash.sha256('test')",
			expected: CELTypeBytes,
		},
		{
			name:     "random.seededString returns string",
			expr:     "random.seededString(10, 'seed')",
			expected: CELTypeString,
		},
		{
			name:     "random.seededInt returns int",
			expr:     "random.seededInt(0, 100, 'seed')",
			expected: CELTypeInt,
		},
		{
			name:     "json.marshal returns string",
			expr:     "json.marshal(schema.spec.config)",
			expected: CELTypeString,
		},
		{
			name:     "json.unmarshal returns object",
			expr:     "json.unmarshal('{}')",
			expected: CELTypeObject,
		},
		{
			name:     "lists.setAtIndex returns list",
			expr:     "lists.setAtIndex([1,2,3], 0, 99)",
			expected: CELTypeList,
		},
		{
			name:     "base64.encode returns string",
			expr:     "base64.encode(b'hello')",
			expected: CELTypeString,
		},
		{
			name:     "base64.decode returns bytes",
			expr:     "base64.decode('aGVsbG8=')",
			expected: CELTypeBytes,
		},

		// Chained operations
		{
			name:     "split().filter() returns list",
			expr:     "schema.spec.fullDNSName.split('.').filter(x, size(x) > 2)",
			expected: CELTypeList,
		},
		{
			name:     "split().map() returns list",
			expr:     "schema.spec.fullDNSName.split('.').map(x, x.upperAscii())",
			expected: CELTypeList,
		},
		{
			name:     "split()[0].upperAscii() returns string",
			expr:     "schema.spec.fullDNSName.split('.')[0].upperAscii()",
			expected: CELTypeString,
		},

		// K8s resource fields
		{
			name:     "deployment.metadata is object",
			expr:     "deployment.metadata",
			expected: CELTypeObject,
		},
		{
			name:     "deployment.metadata.name is string",
			expr:     "deployment.metadata.name",
			expected: CELTypeString,
		},
		{
			name:     "deployment.metadata.labels is map",
			expr:     "deployment.metadata.labels",
			expected: CELTypeMap,
		},
		{
			name:     "deployment.spec.replicas is int",
			expr:     "deployment.spec.replicas",
			expected: CELTypeInt,
		},

		// Literals
		{
			name:     "string literal",
			expr:     `"hello"`,
			expected: CELTypeString,
		},
		{
			name:     "integer literal",
			expr:     "42",
			expected: CELTypeInt,
		},
		{
			name:     "boolean literal true",
			expr:     "true",
			expected: CELTypeBool,
		},
		{
			name:     "boolean literal false",
			expr:     "false",
			expected: CELTypeBool,
		},
		{
			name:     "list literal",
			expr:     "[1, 2, 3]",
			expected: CELTypeList,
		},
		{
			name:     "map literal",
			expr:     "{'key': 'value'}",
			expected: CELTypeMap,
		},

		// Method references (without parens) should return unknown
		// This prevents suggesting methods on method references
		{
			name:     "method reference: schema.spec.name.replace (no parens)",
			expr:     "schema.spec.name.replace",
			expected: CELTypeUnknown,
		},
		{
			name:     "method reference: schema.spec.name.split (no parens)",
			expr:     "schema.spec.name.split",
			expected: CELTypeUnknown,
		},
		{
			name:     "method reference: deployment.metadata.labels.merge (no parens)",
			expr:     "deployment.metadata.labels.merge",
			expected: CELTypeUnknown,
		},

		// Debug: Full path with method call
		{
			name:     "method call: deployment.metadata.labels.app.replace() WITH parens",
			expr:     "deployment.metadata.labels.app.replace()",
			expected: CELTypeString,
		},

		// Bool-returning methods
		{
			name:     "method call: deployment.metadata.labels.app.contains('x') returns bool",
			expr:     "deployment.metadata.labels.app.contains('x')",
			expected: CELTypeBool,
		},
		{
			name:     "method call: deployment.metadata.labels.app.contains() empty args returns bool",
			expr:     "deployment.metadata.labels.app.contains()",
			expected: CELTypeBool,
		},

		// Chained invalid calls - calling contains on bool
		{
			name:     "invalid: bool.contains() should return unknown or bool",
			expr:     "deployment.metadata.labels.app.contains('x').contains()",
			expected: CELTypeBool, // Actually returns bool (even though invalid CEL)
		},

		// The actual user's expression
		{
			name:     "user expression: replace('x').contains().contains()",
			expr:     "service.metadata.labels.app.replace('x').contains().contains()",
			expected: CELTypeUnknown, // Actually unknown (invalid CEL - calling contains on bool)
		},

	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := InferCELExpressionType(tt.expr, symbolTable)
			if result != tt.expected {
				t.Errorf("InferCELExpressionType(%q) = %v, want %v", tt.expr, result, tt.expected)

				// Debug output
				t.Logf("Expected %v but got %v for: %s", tt.expected, result, tt.expr)
				methods := GetMethodsForType(result)
				t.Logf("Methods for type %v: %v", result, methods)
			}
		})
	}
}

func TestGetMethodsForType(t *testing.T) {
	tests := []struct {
		name         string
		celType      CELType
		expectMethod string // One method we expect to find
	}{
		{
			name:         "string has split method",
			celType:      CELTypeString,
			expectMethod: "split",
		},
		{
			name:         "string has contains method",
			celType:      CELTypeString,
			expectMethod: "contains",
		},
		{
			name:         "list has filter method",
			celType:      CELTypeList,
			expectMethod: "filter",
		},
		{
			name:         "list has map method",
			celType:      CELTypeList,
			expectMethod: "map",
		},
		{
			name:         "map has merge method",
			celType:      CELTypeMap,
			expectMethod: "merge",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			methods := GetMethodsForType(tt.celType)
			found := false
			for _, method := range methods {
				if method == tt.expectMethod {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("GetMethodsForType(%v) missing method %q, got: %v", tt.celType, tt.expectMethod, methods)
			}
		})
	}
}

func TestSchemaTypeToCELType(t *testing.T) {
	tests := []struct {
		schemaType string
		expected   CELType
	}{
		{"string", CELTypeString},
		{"string | default='hello'", CELTypeString},
		{"integer", CELTypeInt},
		{"integer | min=1 | max=10", CELTypeInt},
		{"boolean", CELTypeBool},
		{"array[string]", CELTypeList},
		{"[string]", CELTypeList},
		{"map[string]string", CELTypeMap},
		{"{string: string}", CELTypeMap},
	}

	for _, tt := range tests {
		t.Run(tt.schemaType, func(t *testing.T) {
			result := schemaTypeToCELType(tt.schemaType)
			if result != tt.expected {
				t.Errorf("schemaTypeToCELType(%q) = %v, want %v", tt.schemaType, result, tt.expected)
			}
		})
	}
}
