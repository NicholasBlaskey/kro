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
	"math"

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
func Policy(options ...PolicyOption) cel.EnvOption {
	lib := &policyLib{version: math.MaxUint32}
	for _, o := range options {
		lib = o(lib)
	}
	return cel.Lib(lib)
}

type policyLib struct {
	version uint32
}

// PolicyOption is a functional option for configuring the policy library.
type PolicyOption func(*policyLib) *policyLib

// PolicyVersion configures the version of the policy library.
func PolicyVersion(version uint32) PolicyOption {
	return func(lib *policyLib) *policyLib {
		lib.version = version
		return lib
	}
}

func (l *policyLib) LibraryName() string {
	return "kro.policy"
}

func (l *policyLib) CompileOptions() []cel.EnvOption {
	policyType := cel.ObjectType("kro.Policy")
	return []cel.EnvOption{
		cel.Function("policy",
			cel.Overload("policy_void",
				[]*cel.Type{},
				policyType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					return newPolicyValue("")
				}),
			),
		),
		cel.Function("withRetain",
			cel.MemberOverload("policy_withRetain",
				[]*cel.Type{policyType},
				policyType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					p, ok := val.(*policyValue)
					if !ok {
						return types.NewErr("withRetain() can only be called on a Policy")
					}
					return p.withDeletePolicy("retain")
				}),
			),
		),
		cel.Function("withDelete",
			cel.MemberOverload("policy_withDelete",
				[]*cel.Type{policyType},
				policyType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					p, ok := val.(*policyValue)
					if !ok {
						return types.NewErr("withDelete() can only be called on a Policy")
					}
					return p.withDeletePolicy("delete")
				}),
			),
		),
	}
}

func (l *policyLib) ProgramOptions() []cel.ProgramOption {
	return nil
}

type policyValue struct {
	ref.Val
	deletePolicy string
	policyType   ref.Type
}

func newPolicyValue(policy string) *policyValue {
	m := types.NewMutableMap(types.DefaultTypeAdapter, make(map[ref.Val]ref.Val))
	if policy != "" {
		m.Insert(types.String("deletePolicy"), types.String(policy))
	}
	return &policyValue{
		Val:          m.ToImmutableMap(),
		deletePolicy: policy,
		policyType:   types.NewObjectTypeValue("kro.Policy"),
	}
}

func (p *policyValue) Type() ref.Type {
	return p.policyType
}

func (p *policyValue) Get(key ref.Val) ref.Val {
	return p.Val.(traits.Mapper).Get(key)
}

func (p *policyValue) Contains(value ref.Val) ref.Val {
	return p.Val.(traits.Container).Contains(value)
}

func (p *policyValue) Iterator() traits.Iterator {
	return p.Val.(traits.Iterable).Iterator()
}

func (p *policyValue) Size() ref.Val {
	return p.Val.(traits.Sizer).Size()
}

func (p *policyValue) Find(key ref.Val) (ref.Val, bool) {
	return p.Val.(traits.Mapper).Find(key)
}

func (p *policyValue) withDeletePolicy(policy string) ref.Val {
	if p.deletePolicy != "" && p.deletePolicy != policy {
		return types.NewErr("deletePolicy cannot be set multiple times (already set to %q, cannot change to %q)", p.deletePolicy, policy)
	}
	return newPolicyValue(policy)
}
