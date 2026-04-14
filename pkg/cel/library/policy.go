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

package library

import (
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// Policy returns a cel.EnvOption that registers the policy() function and related methods.
//
// policy() returns an empty lifecycle policy builder (empty map).
// The builder supports method chaining:
//   - withRetain() sets deletePolicy to "retain"
//   - withDelete() sets deletePolicy to "delete"
//
// Example usage:
//
//	lifecycle: "${policy()}"                    // {}
//	lifecycle: "${policy().withRetain()}"       // {deletePolicy: "retain"}
//	lifecycle: "${policy().withDelete()}"       // {deletePolicy: "delete"}
func Policy() cel.EnvOption {
	return cel.Lib(&policyLib{})
}

type policyLib struct{}

func (l *policyLib) LibraryName() string {
	return "kro.policy"
}

func (l *policyLib) CompileOptions() []cel.EnvOption {
	mapType := cel.MapType(cel.StringType, cel.DynType)
	return []cel.EnvOption{
		cel.Function("policy",
			cel.Overload("policy_void",
				[]*cel.Type{},
				mapType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					return types.NewMutableMap(types.DefaultTypeAdapter, make(map[ref.Val]ref.Val)).ToImmutableMap()
				}),
			),
		),
		cel.Function("withRetain",
			cel.MemberOverload("map_withRetain",
				[]*cel.Type{mapType},
				mapType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					m, ok := val.(traits.Mapper)
					if !ok {
						return types.NewErr("withRetain() can only be called on a map")
					}
					return withDeletePolicy(m, "retain")
				}),
			),
		),
		cel.Function("withDelete",
			cel.MemberOverload("map_withDelete",
				[]*cel.Type{mapType},
				mapType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					m, ok := val.(traits.Mapper)
					if !ok {
						return types.NewErr("withDelete() can only be called on a map")
					}
					return withDeletePolicy(m, "delete")
				}),
			),
		),
	}
}

func (l *policyLib) ProgramOptions() []cel.ProgramOption {
	return nil
}

// withDeletePolicy creates a new map with deletePolicy set.
// If deletePolicy is already set to a different value, returns an error.
func withDeletePolicy(m traits.Mapper, policy string) ref.Val {
	result := mapperToMutableMapper(m)

	// Check if deletePolicy is already set
	key := types.String("deletePolicy")
	if existing := result.Get(key); existing != types.NullValue {
		existingStr, ok := existing.(types.String)
		if ok && string(existingStr) != policy {
			return types.NewErr("deletePolicy cannot be set multiple times (already set to %q, cannot change to %q)", existingStr, policy)
		}
	}

	result.Insert(key, types.String(policy))
	return result.ToImmutableMap()
}

// mapperToMutableMapper copies a traits.Mapper into a MutableMap.
func mapperToMutableMapper(m traits.Mapper) traits.MutableMapper {
	vals := make(map[ref.Val]ref.Val, m.Size().(types.Int))
	for it := m.Iterator(); it.HasNext().(types.Bool); {
		k := it.Next()
		vals[k] = m.Get(k)
	}
	return types.NewMutableMap(types.DefaultTypeAdapter, vals)
}
