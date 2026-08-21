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

package library

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/parser"
)

// Time returns a CEL library providing the `time` variable used to author
// time-dependent behavior on RGD instances. See KREP-025.
//
// The impure part is a single function on the injected `time` variable. It
// always takes one argument:
//
//	time.now(null)              -> timestamp   current time, no requeue
//	time.now(evaluateAfter: ts) -> timestamp   current time; requeue the instance at evaluateAfter
//
// The pure part is date math on timestamps:
//
//	ts.withTime({hours: 9, minutes: 0, ...}, tz) -> timestamp   set time-of-day in tz (DST-safe)
//	ts.addDays(n, tz)                            -> timestamp   calendar-day arithmetic (DST-safe)
//
// Gates are ordinary timestamp comparison, e.g. time.now() >= t.
func Time() cel.EnvOption {
	return cel.Lib(&timeLibrary{})
}

// TimeTypeName is the CEL type name for the injected time variable.
const TimeTypeName = "kro.run.Time"

// TimeVarName is the identifier kro injects into the CEL evaluation context
// for the time variable.
const TimeVarName = "time"

// timeType carries no traits; only method dispatch (now) is exposed.
var timeType = cel.ObjectType(TimeTypeName)

// withTime map keys. Any key not present defaults to 0.
const (
	timeKeyHours   = "hours"
	timeKeyMinutes = "minutes"
	timeKeySeconds = "seconds"
	timeKeyNanos   = "nanos"
)

var timeFieldKeys = map[string]struct{}{
	timeKeyHours:   {},
	timeKeyMinutes: {},
	timeKeySeconds: {},
	timeKeyNanos:   {},
}

type timeLibrary struct{}

func (l *timeLibrary) LibraryName() string {
	return "kro.time"
}

func (l *timeLibrary) CompileOptions() []cel.EnvOption {
	fieldsMap := cel.MapType(cel.StringType, cel.IntType)

	return []cel.EnvOption{
		cel.Types(timeType),
		cel.Variable(TimeVarName, timeType),

		// Impure: current time. Always takes one argument:
		//   time.now(null)          -> current time, no requeue
		//   time.now(evaluateAfter) -> current time, requeue at evaluateAfter
		cel.Function("now",
			cel.MemberOverload("time_now_null",
				[]*cel.Type{timeType, cel.NullType},
				cel.TimestampType,
				cel.BinaryBinding(func(receiver, _ ref.Val) ref.Val { return nowImpl(receiver) }),
			),
			cel.MemberOverload("time_now_timestamp",
				[]*cel.Type{timeType, cel.TimestampType},
				cel.TimestampType,
				cel.BinaryBinding(nowEvaluateAfterImpl),
			),
		),

		// Pure: date math on timestamps.
		cel.Function("withTime",
			cel.MemberOverload("timestamp_withTime_map_string",
				[]*cel.Type{cel.TimestampType, fieldsMap, cel.StringType},
				cel.TimestampType,
				cel.FunctionBinding(withTimeImpl),
			),
		),
		cel.Function("addDays",
			cel.MemberOverload("timestamp_addDays_int_string",
				[]*cel.Type{cel.TimestampType, cel.IntType, cel.StringType},
				cel.TimestampType,
				cel.FunctionBinding(addDaysImpl),
			),
		),

		// Authors write withTime's fields as a struct-style map literal with
		// bare identifier keys ({hours: 9}). This parse-time macro rewrites the
		// keys into string literals and validates them, so the declared
		// signature stays map(string, int).
		cel.Macros(parser.NewReceiverMacro("withTime", 2, withTimeMacro)),
	}
}

// withTimeMacro rewrites withTime's identifier-keyed map literal into a
// string-keyed one at parse time, rejecting unknown or duplicate keys with
// source-position errors.
func withTimeMacro(eh parser.ExprHelper, target ast.Expr, args []ast.Expr) (ast.Expr, *common.Error) {
	mapArg := args[0]
	// Only rewrite struct-style literals; a computed map(string,int) is left
	// untouched and type-checks on its own.
	if mapArg.Kind() != ast.MapKind {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(timeFieldKeys))
	newEntries := make([]ast.EntryExpr, 0, len(mapArg.AsMap().Entries()))
	for _, entry := range mapArg.AsMap().Entries() {
		me := entry.AsMapEntry()
		key := me.Key()
		if key.Kind() != ast.IdentKind {
			return nil, eh.NewError(key.ID(),
				"withTime: keys must be bare identifiers (hours, minutes, seconds, nanos); quoted or computed keys are not allowed")
		}
		name := key.AsIdent()
		if _, allowed := timeFieldKeys[name]; !allowed {
			return nil, eh.NewError(key.ID(),
				fmt.Sprintf("withTime: unknown field %q (allowed: hours, minutes, seconds, nanos)", name))
		}
		if _, dup := seen[name]; dup {
			return nil, eh.NewError(key.ID(), fmt.Sprintf("withTime: duplicate field %q", name))
		}
		seen[name] = struct{}{}
		newEntries = append(newEntries, eh.NewMapEntry(eh.NewLiteral(types.String(name)), me.Value(), false))
	}

	newMap := eh.NewMap(newEntries...)
	return eh.NewMemberCall("withTime", target, newMap, args[1]), nil
}

func (l *timeLibrary) ProgramOptions() []cel.ProgramOption {
	return nil
}

// RequeueCollector accumulates the earliest future instant requested through
// time.now(evaluateAfter) during a single reconcile. It is safe for concurrent
// use, though evaluation within a reconcile is sequential.
type RequeueCollector struct {
	mu  sync.Mutex
	set bool
	at  time.Time
}

// Observe records t as a candidate requeue instant, keeping the earliest.
func (c *RequeueCollector) Observe(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.set || t.Before(c.at) {
		c.set = true
		c.at = t
	}
}

// Earliest returns the earliest observed requeue instant, if any.
func (c *RequeueCollector) Earliest() (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at, c.set
}

// timeValue is the per-reconcile value backing the `time` CEL variable. It
// snapshots the current time once (so repeated time.now() calls in a reconcile
// agree) and records evaluateAfter requests into the shared collector.
type timeValue struct {
	now       time.Time
	collector *RequeueCollector
}

// NewTimeValue builds the value injected as the `time` variable for one
// reconcile. now is the snapshot returned by time.now(); collector receives
// evaluateAfter requests and may be nil (requests are then ignored).
func NewTimeValue(now time.Time, collector *RequeueCollector) ref.Val {
	return &timeValue{now: now, collector: collector}
}

func (t *timeValue) ConvertToNative(typeDesc reflect.Type) (any, error) {
	return nil, fmt.Errorf("type conversion not supported for %s", TimeTypeName)
}

func (t *timeValue) ConvertToType(typeVal ref.Type) ref.Val {
	if typeVal == timeType {
		return t
	}
	if typeVal == types.TypeType {
		return timeType
	}
	return types.NewErr("type conversion error from %s to %s", TimeTypeName, typeVal.TypeName())
}

func (t *timeValue) Equal(other ref.Val) ref.Val {
	o, ok := other.(*timeValue)
	if !ok {
		return types.MaybeNoSuchOverloadErr(other)
	}
	return types.Bool(t.now.Equal(o.now))
}

func (t *timeValue) Type() ref.Type { return timeType }

func (t *timeValue) Value() any { return t }

// nowImpl is the binding for time.now(): the per-reconcile snapshot.
func nowImpl(receiver ref.Val) ref.Val {
	tv, ok := receiver.(*timeValue)
	if !ok {
		return types.NewErr("time.now: receiver must be the time variable, got %v", receiver.Type().TypeName())
	}
	return types.Timestamp{Time: tv.now}
}

// nowEvaluateAfterImpl is the binding for time.now(evaluateAfter): returns the
// same snapshot and records evaluateAfter as a requeue point when it is in the
// future relative to the snapshot (past instants are ignored, so no hot loop).
func nowEvaluateAfterImpl(receiver, evaluateAfter ref.Val) ref.Val {
	tv, ok := receiver.(*timeValue)
	if !ok {
		return types.NewErr("time.now: receiver must be the time variable, got %v", receiver.Type().TypeName())
	}
	ts, ok := evaluateAfter.(types.Timestamp)
	if !ok {
		return types.NewErr("time.now: evaluateAfter must be a timestamp, got %v", evaluateAfter.Type().TypeName())
	}
	if tv.collector != nil && ts.Time.After(tv.now) {
		tv.collector.Observe(ts.Time)
	}
	return types.Timestamp{Time: tv.now}
}

// withTimeImpl sets the wall-clock time-of-day of the receiver in the given
// IANA timezone, keeping the date as seen in that zone. Missing fields default
// to 0. It is DST-safe because it reconstructs the instant in the zone.
func withTimeImpl(args ...ref.Val) ref.Val {
	if len(args) != 3 {
		return types.NewErr("withTime: expected (fields, tz), got %d args", len(args)-1)
	}
	base, ok := args[0].(types.Timestamp)
	if !ok {
		return types.NewErr("withTime: receiver must be a timestamp, got %v", args[0].Type().TypeName())
	}
	mapper, ok := args[1].(traits.Mapper)
	if !ok {
		return types.NewErr("withTime: fields must be a map, got %v", args[1].Type().TypeName())
	}
	tzStr, ok := args[2].(types.String)
	if !ok {
		return types.NewErr("withTime: tz must be a string, got %v", args[2].Type().TypeName())
	}

	loc, err := time.LoadLocation(string(tzStr))
	if err != nil {
		return types.NewErr("withTime: invalid timezone %q: %v", string(tzStr), err)
	}

	field := func(key string, max int64) (int, ref.Val) {
		v, found := mapper.Find(types.String(key))
		if !found {
			return 0, nil
		}
		iv, ok := v.(types.Int)
		if !ok {
			return 0, types.NewErr("withTime: %s must be an integer", key)
		}
		n := int64(iv)
		if n < 0 || n > max {
			return 0, types.NewErr("withTime: %s out of range [0, %d]: %d", key, max, n)
		}
		return int(n), nil
	}

	// Reject unknown keys so typos fail loudly.
	for it := mapper.Iterator(); it.HasNext() == types.True; {
		k := it.Next()
		ks, ok := k.(types.String)
		if !ok {
			return types.NewErr("withTime: field keys must be strings")
		}
		if _, allowed := timeFieldKeys[string(ks)]; !allowed {
			return types.NewErr("withTime: unknown field %q (allowed: hours, minutes, seconds, nanos)", string(ks))
		}
	}

	hours, errVal := field(timeKeyHours, 23)
	if errVal != nil {
		return errVal
	}
	minutes, errVal := field(timeKeyMinutes, 59)
	if errVal != nil {
		return errVal
	}
	seconds, errVal := field(timeKeySeconds, 59)
	if errVal != nil {
		return errVal
	}
	nanos, errVal := field(timeKeyNanos, 999999999)
	if errVal != nil {
		return errVal
	}

	local := base.Time.In(loc)
	result := time.Date(local.Year(), local.Month(), local.Day(), hours, minutes, seconds, nanos, loc)
	return types.Timestamp{Time: result}
}

// addDaysImpl adds n calendar days to the receiver in the given timezone. It is
// DST-safe (calendar arithmetic, not a fixed 24h duration).
func addDaysImpl(args ...ref.Val) ref.Val {
	if len(args) != 3 {
		return types.NewErr("addDays: expected (n, tz), got %d args", len(args)-1)
	}
	base, ok := args[0].(types.Timestamp)
	if !ok {
		return types.NewErr("addDays: receiver must be a timestamp, got %v", args[0].Type().TypeName())
	}
	n, ok := args[1].(types.Int)
	if !ok {
		return types.NewErr("addDays: n must be an integer, got %v", args[1].Type().TypeName())
	}
	tzStr, ok := args[2].(types.String)
	if !ok {
		return types.NewErr("addDays: tz must be a string, got %v", args[2].Type().TypeName())
	}

	loc, err := time.LoadLocation(string(tzStr))
	if err != nil {
		return types.NewErr("addDays: invalid timezone %q: %v", string(tzStr), err)
	}

	local := base.Time.In(loc)
	return types.Timestamp{Time: local.AddDate(0, 0, int(n))}
}
