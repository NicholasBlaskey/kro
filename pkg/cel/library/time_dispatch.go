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

// time_dispatch.go makes KREP-025 time solving independent of operand order.
//
// CEL dispatches standard operators through trait interfaces on the LEFT
// operand, so `time.now() >= x` reaches KroTimestamp.Compare (which solves),
// but `x <= time.now()` reaches the plain timestamp's Compare, which rejects
// the unfamiliar right operand. The KREP's "Overriding" section anticipated
// fixing this by rewriting the source AST; intercepting the PLANNED program
// occupies the same design slot with strictly wider coverage: a now()-derived
// value that reaches an operator through cel.bind, a comprehension, or any
// other indirection is caught here by inspecting the actual runtime values,
// which no static rewrite can see.
//
// The decorator wraps the six affected operators (<, <=, >, >=, +, −). When
// neither evaluated operand is a Kro time value it replicates the standard
// library's trait dispatch exactly (including NaN semantics), so ordinary
// expressions are unchanged. When either operand is Kro time, both sides are
// lifted to affine form and the shared solve/arithmetic logic runs, with the
// reconcile clock taken from whichever side carries it.
package library

import (
	"math"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/operators"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/interpreter"
)

// TimeOperatorDecorator returns the program option that installs order-
// independent dispatch for comparisons and arithmetic involving Kro time
// values. It must be present in the program options of every environment
// that evaluates user expressions (kro wires it through ProgramOptions).
func TimeOperatorDecorator() cel.ProgramOption {
	return cel.CustomDecoratorV2(decorateTimeOperators)
}

// timeOperators are the operators whose evaluation participates in requeue
// solving. Equality is excluded: the KREP defines solving for the four
// comparisons only, and CEL equality does not dispatch through Comparer.
var timeOperators = map[string]bool{
	operators.Less:          true,
	operators.LessEquals:    true,
	operators.Greater:       true,
	operators.GreaterEquals: true,
	operators.Add:           true,
	operators.Subtract:      true,
}

func decorateTimeOperators(i interpreter.InterpretableV2) (interpreter.InterpretableV2, error) {
	call, ok := i.(interpreter.InterpretableCall)
	if !ok || !timeOperators[call.Function()] || len(call.Args()) != 2 {
		return i, nil
	}
	return &timeOpCall{InterpretableCall: call}, nil
}

// timeOpCall wraps a planned binary operator call. Embedding preserves the
// InterpretableCall surface (ID, Function, OverloadID, Args) for any other
// planner machinery that inspects it.
type timeOpCall struct {
	interpreter.InterpretableCall
}

// Exec implements InterpretableV2.
func (c *timeOpCall) Exec(frame *interpreter.ExecutionFrame) ref.Val {
	args := c.Args()
	lhs := args[0].Exec(frame)
	if types.IsUnknownOrError(lhs) {
		return lhs
	}
	rhs := args[1].Exec(frame)
	if types.IsUnknownOrError(rhs) {
		return rhs
	}
	return evalTimeOp(c.Function(), lhs, rhs)
}

// Eval implements Interpretable.
func (c *timeOpCall) Eval(vars interpreter.Activation) ref.Val {
	return c.Exec(interpreter.AsFrame(vars))
}

// kroClockOf returns the reconcile clock if v is a Kro time value.
func kroClockOf(v ref.Val) (*Clock, bool) {
	switch t := v.(type) {
	case *KroTimestamp:
		return t.clock, true
	case *KroDuration:
		return t.clock, true
	}
	return nil, false
}

// evalTimeOp dispatches a binary operator over evaluated operands. Plain
// operands take the standard-library path; a Kro time value on either side
// takes the affine solve/arithmetic path.
func evalTimeOp(fn string, lhs, rhs ref.Val) ref.Val {
	clock, lk := kroClockOf(lhs)
	if !lk {
		var rk bool
		clock, rk = kroClockOf(rhs)
		if !rk {
			return evalStandardOp(fn, lhs, rhs)
		}
	}

	switch fn {
	case operators.Add:
		return kroAdd(clock, lhs, rhs)
	case operators.Subtract:
		return kroSubtract(clock, lhs, rhs)
	default:
		return kroCompare(clock, fn, lhs, rhs)
	}
}

// evalStandardOp replicates the CEL standard library's singleton bindings for
// the six operators (trait dispatch on the left operand, NaN compares false).
func evalStandardOp(fn string, lhs, rhs ref.Val) ref.Val {
	switch fn {
	case operators.Add:
		adder, ok := lhs.(traits.Adder)
		if !ok {
			return types.MaybeNoSuchOverloadErr(lhs)
		}
		return adder.Add(rhs)
	case operators.Subtract:
		sub, ok := lhs.(traits.Subtractor)
		if !ok {
			return types.MaybeNoSuchOverloadErr(lhs)
		}
		return sub.Subtract(rhs)
	}
	if isNaNVal(lhs) || isNaNVal(rhs) {
		return types.False
	}
	cmp, ok := lhs.(traits.Comparer)
	if !ok {
		return types.MaybeNoSuchOverloadErr(lhs)
	}
	return compareResult(fn, cmp.Compare(rhs))
}

func isNaNVal(v ref.Val) bool {
	d, ok := v.(types.Double)
	return ok && math.IsNaN(float64(d))
}

// compareResult maps a three-way Compare result onto the comparison operator.
func compareResult(fn string, cmp ref.Val) ref.Val {
	c, ok := cmp.(types.Int)
	if !ok {
		return cmp // error from Compare
	}
	switch fn {
	case operators.Less:
		return types.Bool(c < 0)
	case operators.LessEquals:
		return types.Bool(c <= 0)
	case operators.Greater:
		return types.Bool(c > 0)
	case operators.GreaterEquals:
		return types.Bool(c >= 0)
	}
	return types.NewErr("unexpected comparison operator %q", fn)
}

// kroCompare lifts both operands (both timestamps, or both durations),
// answers the comparison at the fixed now, and records the future flip.
func kroCompare(clock *Clock, fn string, lhs, rhs ref.Val) ref.Val {
	if l, ok := liftTimestamp(lhs, clock); ok {
		r, ok := liftTimestamp(rhs, clock)
		if !ok {
			return types.MaybeNoSuchOverloadErr(rhs)
		}
		return compareResult(fn, compareAndSolve(l, r))
	}
	if l, ok := liftDuration(lhs, clock); ok {
		r, ok := liftDuration(rhs, clock)
		if !ok {
			return types.MaybeNoSuchOverloadErr(rhs)
		}
		return compareResult(fn, compareAndSolve(l, r))
	}
	return types.MaybeNoSuchOverloadErr(lhs)
}

// kroAdd implements ts+dur → ts, dur+ts → ts, dur+dur → dur; ts+ts errors.
func kroAdd(clock *Clock, lhs, rhs ref.Val) ref.Val {
	lt, ltOK := liftTimestamp(lhs, clock)
	ld, ldOK := liftDuration(lhs, clock)
	rt, rtOK := liftTimestamp(rhs, clock)
	rd, rdOK := liftDuration(rhs, clock)
	switch {
	case ltOK && rdOK:
		return &KroTimestamp{affine{clock: clock, nowCount: lt.nowCount + rd.nowCount, offset: lt.offset + rd.offset}}
	case ldOK && rtOK:
		return &KroTimestamp{affine{clock: clock, nowCount: ld.nowCount + rt.nowCount, offset: ld.offset + rt.offset}}
	case ldOK && rdOK:
		return &KroDuration{affine{clock: clock, nowCount: ld.nowCount + rd.nowCount, offset: ld.offset + rd.offset}}
	case ltOK && rtOK:
		return types.NewErr("adding two timestamps is not supported")
	}
	return types.MaybeNoSuchOverloadErr(rhs)
}

// kroSubtract implements ts−ts → dur, ts−dur → ts, dur−dur → dur; dur−ts errors.
func kroSubtract(clock *Clock, lhs, rhs ref.Val) ref.Val {
	lt, ltOK := liftTimestamp(lhs, clock)
	ld, ldOK := liftDuration(lhs, clock)
	rt, rtOK := liftTimestamp(rhs, clock)
	rd, rdOK := liftDuration(rhs, clock)
	switch {
	case ltOK && rtOK:
		return &KroDuration{affine{clock: clock, nowCount: lt.nowCount - rt.nowCount, offset: lt.offset - rt.offset}}
	case ltOK && rdOK:
		return &KroTimestamp{affine{clock: clock, nowCount: lt.nowCount - rd.nowCount, offset: lt.offset - rd.offset}}
	case ldOK && rdOK:
		return &KroDuration{affine{clock: clock, nowCount: ld.nowCount - rd.nowCount, offset: ld.offset - rd.offset}}
	case ldOK && rtOK:
		return types.NewErr("subtracting a timestamp from a duration is not supported")
	}
	return types.MaybeNoSuchOverloadErr(rhs)
}
