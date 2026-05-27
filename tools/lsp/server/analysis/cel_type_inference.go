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

import "strings"

// CELType represents a CEL expression type
type CELType string

const (
	CELTypeUnknown CELType = "unknown"
	CELTypeString  CELType = "string"
	CELTypeInt     CELType = "int"
	CELTypeBool    CELType = "bool"
	CELTypeBytes   CELType = "bytes"
	CELTypeList    CELType = "list"
	CELTypeMap     CELType = "map"
	CELTypeObject  CELType = "object"
)

// InferCELExpressionType attempts to infer the type of a CEL expression
// based on schema field types, function return types, and operations.
//
// Examples:
//   schema.spec.fullDNSName -> "string" (from schema)
//   schema.spec.fullDNSName.split('.') -> "list" (string method returns list)
//   schema.spec.fullDNSName.split('.')[0] -> "string" (list index returns element)
//   hash.fnv64a('hello') -> "bytes" (function return type)
//   [1,2,3].filter(x, x > 1) -> "list" (list method returns list)
func InferCELExpressionType(expr string, symbolTable *SymbolTable) CELType {
	if expr == "" {
		return CELTypeUnknown
	}

	// Parse the expression into segments
	// Handle: schema.spec.name, schema.spec.name.split('.'), func().method, etc.

	// Check for array indexing (but not list literals or brackets inside strings)
	// Handle: expr[index] or func()[index] or func()[index].method
	// But skip list literals like [1, 2, 3] and strings with brackets like '[a-z]+'
	// Only process if bracket appears AFTER a closing paren ) or an identifier
	bracketIdx := strings.Index(expr, "[")
	if bracketIdx != -1 && bracketIdx > 0 {
		// Check if this is indexing (comes after ) or identifier)
		// vs list literal (comes at start or after operators)
		prevChar := expr[bracketIdx-1]
		isIndexing := prevChar == ')' || (prevChar >= 'a' && prevChar <= 'z') || (prevChar >= 'A' && prevChar <= 'Z')

		if isIndexing {
		// Find matching closing bracket
		closeBracketIdx := strings.Index(expr[bracketIdx:], "]")
		if closeBracketIdx != -1 {
			closeBracketIdx += bracketIdx

			// Get the base expression before [
			baseExpr := expr[:bracketIdx]
			baseType := InferCELExpressionType(baseExpr, symbolTable)

			// If base is a list, indexing returns the element type
			if baseType == CELTypeList {
				// Check if there's more after the bracket
				if closeBracketIdx+1 < len(expr) {
					// There's more expression after [index]
					// e.g., split('.')[0].upperAscii()
					// The [0] returns string, then we continue with the rest
					remainingExpr := expr[closeBracketIdx+1:]
					if strings.HasPrefix(remainingExpr, ".") {
						// It's a method/field access on the indexed element
						// split() returns list(string), so [0] returns string
						// Then we need to infer the type of string.upperAscii()

						// Build a synthetic expression with string type + remaining
						// Extract the method/field from remaining (e.g., ".upperAscii()")
						methodPart := remainingExpr[1:] // Skip the dot

						// Check if it's a function call
						if strings.Contains(methodPart, "(") {
							// It's a method call - find the method name
							parenIdx := strings.Index(methodPart, "(")
							methodName := methodPart[:parenIdx]

							// Infer return type based on string methods
							switch methodName {
							case "upperAscii", "lowerAscii", "trim", "replace", "substring":
								return CELTypeString
							case "split":
								return CELTypeList
							case "contains", "startsWith", "endsWith", "matches":
								return CELTypeBool
							}
						}

						return CELTypeString // Default for field access on string
					}
				}

				// Just the indexing, no more operations
				// Common pattern: string.split() returns list(string), so [0] is string
				if strings.Contains(baseExpr, ".split(") {
					return CELTypeString
				}
				return CELTypeString // Default assumption for list indexing
			}

			return CELTypeUnknown
		}
		}
	}

	// Try to match function calls
	if strings.Contains(expr, "(") {
		return inferFunctionCallType(expr, symbolTable)
	}

	// Try to match field access: resource.field or schema.spec.field
	// BUT: if the expression ends with a method name (no parens), don't infer its return type
	// Example: "app.replace" is a method reference, not a call
	// Example: "app.replace()" is a call that returns string
	if strings.Contains(expr, ".") {
		// Check if the last segment looks like a method name
		// (it's a known method name but not followed by parentheses)
		lastDot := strings.LastIndex(expr, ".")
		if lastDot != -1 && lastDot < len(expr)-1 {
			lastSegment := expr[lastDot+1:]
			// Check if this is a known method name
			if isKnownMethod(lastSegment) {
				// This is a method reference without (), not a call
				// Return unknown so we don't suggest further methods
				return CELTypeUnknown
			}
		}
		return inferFieldAccessType(expr, symbolTable)
	}

	// Check if it's a known resource ID
	if symbolTable != nil && symbolTable.LookupResource(expr) != nil {
		return CELTypeObject
	}

	// Check for literals
	if strings.HasPrefix(expr, `"`) || strings.HasPrefix(expr, `'`) {
		return CELTypeString
	}
	if expr == "true" || expr == "false" {
		return CELTypeBool
	}
	if len(expr) > 0 && expr[0] >= '0' && expr[0] <= '9' {
		return CELTypeInt
	}
	if strings.HasPrefix(expr, "[") {
		return CELTypeList
	}
	if strings.HasPrefix(expr, "{") {
		return CELTypeMap
	}

	return CELTypeUnknown
}

// inferFunctionCallType infers the type of a function call expression
func inferFunctionCallType(expr string, symbolTable *SymbolTable) CELType {
	// For chained calls like a.b().c().d(), we need to find the LAST complete function call
	// and recursively infer the receiver type

	// Find the last closing paren
	lastCloseParen := strings.LastIndex(expr, ")")
	if lastCloseParen == -1 {
		return CELTypeUnknown
	}

	// Find the matching opening paren for this closing paren
	parenCount := 0
	matchingOpenParen := -1
	for i := lastCloseParen; i >= 0; i-- {
		if expr[i] == ')' {
			parenCount++
		} else if expr[i] == '(' {
			parenCount--
			if parenCount == 0 {
				matchingOpenParen = i
				break
			}
		}
	}

	if matchingOpenParen == -1 {
		return CELTypeUnknown
	}

	// Get the expression before the opening paren - this contains the method name and receiver
	beforeParen := expr[:matchingOpenParen]

	// Find the method name (last segment before '(')
	lastDotIdx := strings.LastIndex(beforeParen, ".")
	var functionName string
	var receiverExpr string

	if lastDotIdx != -1 {
		functionName = beforeParen[lastDotIdx+1:]
		receiverExpr = beforeParen[:lastDotIdx]
	} else {
		functionName = beforeParen
	}

	// Check if it's a known CEL function (e.g., hash.fnv64a, random.seededInt)
	fullFuncName := beforeParen
	if fn := GetCELFunction(fullFuncName); fn != nil {
		return celReturnTypeToType(fn.Returns)
	}

	// If this is a method call on a receiver, infer the receiver type first
	if receiverExpr != "" {
		receiverType := InferCELExpressionType(receiverExpr, symbolTable)

		// Return type based on receiver type + method name
		switch functionName {
		// String methods
		case "split":
			if receiverType == CELTypeString {
				return CELTypeList // split returns list(string)
			}
		case "substring", "replace", "trim", "lowerAscii", "upperAscii":
			if receiverType == CELTypeString {
				return CELTypeString
			}
		case "matches", "startsWith", "endsWith", "contains":
			if receiverType == CELTypeString || receiverType == CELTypeList {
				return CELTypeBool
			}

		// List methods
		case "filter", "map", "slice":
			if receiverType == CELTypeList {
				return CELTypeList
			}
		case "all", "exists", "exists_one":
			if receiverType == CELTypeList {
				return CELTypeBool
			}

		// Universal methods
		case "size":
			return CELTypeInt

		// Map methods
		case "merge":
			if receiverType == CELTypeMap {
				return CELTypeMap
			}
		}
	}

	// Check Kro function namespaces (for top-level calls)
	if strings.HasPrefix(fullFuncName, "hash.") {
		return CELTypeBytes
	}
	if strings.HasPrefix(fullFuncName, "random.seededString") {
		return CELTypeString
	}
	if strings.HasPrefix(fullFuncName, "random.seededInt") {
		return CELTypeInt
	}
	if strings.HasPrefix(fullFuncName, "json.unmarshal") {
		return CELTypeObject // Actually 'dyn', but treat as object
	}
	if strings.HasPrefix(fullFuncName, "json.marshal") {
		return CELTypeString
	}
	if strings.HasPrefix(fullFuncName, "lists.") {
		return CELTypeList
	}
	if strings.HasPrefix(fullFuncName, "base64.encode") {
		return CELTypeString
	}
	if strings.HasPrefix(fullFuncName, "base64.decode") {
		return CELTypeBytes
	}

	return CELTypeUnknown
}

// inferFieldAccessType infers the type of a field access expression
// Handles: schema.spec.name, deployment.metadata.labels, list[0], etc.
func inferFieldAccessType(expr string, symbolTable *SymbolTable) CELType {
	// Handle array indexing: expr[index] returns element type
	if bracketIdx := strings.Index(expr, "["); bracketIdx != -1 {
		// Get the base expression before [
		baseExpr := expr[:bracketIdx]
		baseType := InferCELExpressionType(baseExpr, symbolTable)

		// If base is a list, indexing returns the element type
		// For now, we don't track element types, so return unknown for nested inference
		// Could be enhanced to track list(string) -> string, list(int) -> int
		if baseType == CELTypeList {
			// Try to infer element type from the base expression
			// Common pattern: string.split() returns list(string), so [0] is string
			if strings.Contains(baseExpr, ".split(") {
				return CELTypeString
			}
			return CELTypeString // Default assumption for list indexing
		}

		return CELTypeUnknown
	}

	// Split into segments
	segments := strings.Split(expr, ".")

	// Start with the first segment
	currentType := CELTypeUnknown

	if segments[0] == "schema" {
		if len(segments) >= 3 && segments[1] == "spec" {
			// schema.spec.fieldName - look up field type
			fieldName := segments[2]
			if symbolTable != nil && symbolTable.Schema != nil && symbolTable.Schema.SpecFields != nil {
				if field, ok := symbolTable.Schema.SpecFields[fieldName]; ok {
					currentType = schemaTypeToCELType(field.Type)

					// If there are more segments, we've reached a terminal field
					// and further access is unknown
					if len(segments) > 3 {
						return CELTypeUnknown
					}
					return currentType
				}
			}
		}
		return CELTypeObject // schema itself is an object
	}

	// Check if first segment is a resource ID
	if symbolTable != nil {
		if resource := symbolTable.LookupResource(segments[0]); resource != nil {
			// It's a k8s resource object
			if len(segments) == 1 {
				return CELTypeObject
			}

			// Check common k8s field types
			if len(segments) >= 2 {
				fieldType := inferK8sFieldType(segments[1:])
				if fieldType != CELTypeUnknown {
					return fieldType
				}
			}
		}
	}

	return CELTypeUnknown
}

// inferK8sFieldType infers the type of a k8s field path
func inferK8sFieldType(pathSegments []string) CELType {
	if len(pathSegments) == 0 {
		return CELTypeUnknown
	}

	// Common k8s field types
	firstField := pathSegments[0]

	switch firstField {
	case "metadata":
		if len(pathSegments) == 1 {
			return CELTypeObject
		}
		// metadata.name, metadata.namespace, etc.
		secondField := pathSegments[1]
		switch secondField {
		case "name", "namespace", "uid", "resourceVersion":
			return CELTypeString
		case "labels", "annotations":
			if len(pathSegments) == 2 {
				return CELTypeMap // The map itself
			}
			return CELTypeString // labels.key or annotations.key
		case "generation":
			return CELTypeInt
		}

	case "spec":
		if len(pathSegments) == 1 {
			return CELTypeObject
		}
		// Common spec fields
		secondField := pathSegments[1]
		switch secondField {
		case "replicas", "port", "targetPort", "minReadySeconds", "revisionHistoryLimit":
			return CELTypeInt
		case "selector", "template":
			return CELTypeObject
		case "containers", "volumes", "ports":
			return CELTypeList
		case "type", "clusterIP", "loadBalancerIP", "externalName":
			return CELTypeString
		}

	case "status":
		return CELTypeObject

	case "data", "stringData":
		// ConfigMap/Secret data fields
		if len(pathSegments) == 1 {
			return CELTypeMap
		}
		return CELTypeString // data.key
	}

	return CELTypeUnknown
}

// schemaTypeToCELType converts a schema type string to CELType
func schemaTypeToCELType(schemaType string) CELType {
	// Handle simple types
	if strings.HasPrefix(schemaType, "string") {
		return CELTypeString
	}
	if strings.HasPrefix(schemaType, "integer") || strings.HasPrefix(schemaType, "int") {
		return CELTypeInt
	}
	if strings.HasPrefix(schemaType, "boolean") || strings.HasPrefix(schemaType, "bool") {
		return CELTypeBool
	}
	if strings.HasPrefix(schemaType, "array") || strings.HasPrefix(schemaType, "[") {
		return CELTypeList
	}
	if strings.HasPrefix(schemaType, "map") || strings.HasPrefix(schemaType, "{") {
		return CELTypeMap
	}

	return CELTypeUnknown
}

// celReturnTypeToType converts a function return type string to CELType
func celReturnTypeToType(returnType string) CELType {
	switch returnType {
	case "string":
		return CELTypeString
	case "int", "integer":
		return CELTypeInt
	case "bool", "boolean":
		return CELTypeBool
	case "bytes":
		return CELTypeBytes
	case "dyn", "any":
		return CELTypeObject
	default:
		if strings.HasPrefix(returnType, "list") {
			return CELTypeList
		}
		if strings.HasPrefix(returnType, "map") {
			return CELTypeMap
		}
		return CELTypeUnknown
	}
}

// GetMethodsForType returns available methods/fields for a given CEL type
func GetMethodsForType(celType CELType) []string {
	switch celType {
	case CELTypeString:
		return []string{
			"contains", "startsWith", "endsWith", "matches",
			"split", "replace", "substring", "trim",
			"lowerAscii", "upperAscii",
		}
	case CELTypeList:
		return []string{
			"filter", "map", "all", "exists", "exists_one",
			"size",
		}
	case CELTypeMap:
		return []string{
			"merge", "size",
		}
	case CELTypeInt, CELTypeBool, CELTypeBytes:
		// Primitive types have limited methods
		return []string{}
	case CELTypeObject:
		// Objects have field access, but we can't enumerate without schema
		return []string{}
	default:
		return []string{}
	}
}

// isKnownMethod checks if a string is a known CEL method name
// Used to distinguish method references (app.replace) from method calls (app.replace())
func isKnownMethod(name string) bool {
	knownMethods := map[string]bool{
		// String methods
		"contains": true, "startsWith": true, "endsWith": true, "matches": true,
		"split": true, "replace": true, "substring": true, "trim": true,
		"lowerAscii": true, "upperAscii": true,
		// List methods
		"filter": true, "map": true, "all": true, "exists": true, "exists_one": true,
		// Universal methods
		"size": true,
		// Map methods
		"merge": true,
	}
	return knownMethods[name]
}
