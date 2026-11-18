// Copyright 2026 The Kubernetes Authors.
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

// Package defschema infers the OpenAPI schema of a def payload: computed data
// whose fields are literals or CEL expressions. It is shared by the RGD builder
// and the Graph compiler so both type a def the same way.
package defschema

import (
	"slices"
	"strings"

	"github.com/google/cel-go/cel"
	"k8s.io/kube-openapi/pkg/validation/spec"

	krocel "github.com/kubernetes-sigs/kro/pkg/cel"
	"github.com/kubernetes-sigs/kro/pkg/graph/parser"
	"github.com/kubernetes-sigs/kro/pkg/graph/schema"
)

// Infer walks the def payload and returns an object schema describing its
// runtime shape.
//
// A string that is a CEL expression takes the checked output type recorded in
// exprTypes under the field's path (the path format of the field descriptors
// returned by parser.ParseSchemalessResource). A string containing "${" with no
// recorded type stays dyn. Literals take the type of their value:
//
//   - bool literal   -> boolean
//   - integer        -> integer
//   - float          -> number
//   - string         -> string
//   - map            -> object with the inferred properties
//   - array          -> array of the first element's schema, or of dyn when
//     the elements disagree or the array is empty
//   - nil / other    -> dyn
//
// exprTypes and provider may be nil, in which case every expression is dyn.
func Infer(payload map[string]any, exprTypes map[string]*cel.Type, provider *krocel.DeclTypeProvider) (*spec.Schema, error) {
	return infer(payload, "", exprTypes, provider)
}

func infer(v any, path string, exprTypes map[string]*cel.Type, provider *krocel.DeclTypeProvider) (*spec.Schema, error) {
	switch val := v.(type) {
	case bool:
		return scalar("boolean"), nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, uintptr:
		return scalar("integer"), nil
	case float32, float64:
		return scalar("number"), nil
	case string:
		return inferString(val, path, exprTypes, provider)
	case map[string]any:
		props := make(map[string]spec.Schema, len(val))
		for k, child := range val {
			sch, err := infer(child, parser.JoinPathAndFieldName(path, k), exprTypes, provider)
			if err != nil {
				return nil, err
			}
			props[k] = *sch
		}
		return &spec.Schema{SchemaProps: spec.SchemaProps{
			Type:       []string{"object"},
			Properties: props,
		}}, nil
	case []any:
		return inferList(val, path, exprTypes, provider)
	default:
		return dynSchema(), nil
	}
}

func inferString(val, path string, exprTypes map[string]*cel.Type, provider *krocel.DeclTypeProvider) (*spec.Schema, error) {
	if t, ok := exprTypes[path]; ok {
		if t.IsExactType(cel.DynType) {
			return dynSchema(), nil
		}
		return schema.CELTypeToSchema(t, provider)
	}
	// A field whose value is a CEL fragment with no recorded type can resolve
	// to any type at runtime, so leave it dyn instead of holding downstream
	// expressions to the literal string type.
	if strings.Contains(val, "${") {
		return dynSchema(), nil
	}
	return scalar("string"), nil
}

// inferList types an array by its elements. Items must be set or
// SchemaDeclTypeWithMetadata drops the field.
func inferList(items []any, path string, exprTypes map[string]*cel.Type, provider *krocel.DeclTypeProvider) (*spec.Schema, error) {
	var elem *spec.Schema
	for i, item := range items {
		sch, err := infer(item, parser.JoinPathAndIndex(path, i), exprTypes, provider)
		if err != nil {
			return nil, err
		}
		switch {
		case elem == nil:
			elem = sch
		case !schemasEqual(elem, sch):
			elem = dynSchema()
		}
	}
	if elem == nil {
		elem = dynSchema()
	}
	return &spec.Schema{SchemaProps: spec.SchemaProps{
		Type:  []string{"array"},
		Items: &spec.SchemaOrArray{Schema: elem},
	}}, nil
}

func scalar(t string) *spec.Schema {
	return &spec.Schema{SchemaProps: spec.SchemaProps{Type: []string{t}}}
}

// dynSchema returns a schema that the CEL DeclType converter maps to DynType.
// SchemaDeclTypeWithMetadata returns nil for a fully empty schema (which would
// silently drop the field from its parent's properties), so the
// x-kubernetes-int-or-string marker takes its IsXIntOrString fast path, which
// returns DynType. The actual value can be any type at runtime.
func dynSchema() *spec.Schema {
	s := &spec.Schema{}
	s.AddExtension("x-kubernetes-int-or-string", true)
	return s
}

func schemasEqual(a, b *spec.Schema) bool {
	if a == nil || b == nil {
		return a == b
	}
	if !slices.Equal(a.Type, b.Type) {
		return false
	}
	if len(a.Properties) != len(b.Properties) {
		return false
	}
	for k, propA := range a.Properties {
		propB, ok := b.Properties[k]
		if !ok || !schemasEqual(&propA, &propB) {
			return false
		}
	}
	if (a.Items == nil) != (b.Items == nil) {
		return false
	}
	if a.Items != nil && b.Items != nil {
		if (a.Items.Schema == nil) != (b.Items.Schema == nil) {
			return false
		}
		if a.Items.Schema != nil && b.Items.Schema != nil && !schemasEqual(a.Items.Schema, b.Items.Schema) {
			return false
		}
	}
	return true
}
