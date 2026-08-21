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
	"testing"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// evalTime compiles and evaluates expr with the `time` variable bound to a
// value snapshotted at now and backed by collector (which may be nil).
func evalTime(t *testing.T, now time.Time, collector *RequeueCollector, expr string) any {
	t.Helper()
	env, err := cel.NewEnv(Time())
	require.NoError(t, err)

	ast, iss := env.Compile(expr)
	require.NoError(t, iss.Err())

	prg, err := env.Program(ast)
	require.NoError(t, err)

	out, _, err := prg.Eval(map[string]any{TimeVarName: NewTimeValue(now, collector)})
	require.NoError(t, err)
	return out.Value()
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err)
	return ts
}

func TestTimeNow_ReturnsSnapshot(t *testing.T) {
	now := mustTime(t, "2026-03-15T12:34:56Z")
	got := evalTime(t, now, nil, `time.now(null)`).(time.Time)
	assert.True(t, got.Equal(now), "time.now(null) = %s, want %s", got, now)
}

func TestTimeNow_GateComparison(t *testing.T) {
	now := mustTime(t, "2026-03-15T12:00:00Z")
	// now is after the deadline -> gate true
	assert.Equal(t, true, evalTime(t, now, nil, `time.now(null) >= timestamp("2026-01-01T00:00:00Z")`))
	// now is before the deadline -> gate false
	assert.Equal(t, false, evalTime(t, now, nil, `time.now(null) >= timestamp("2027-01-01T00:00:00Z")`))
}

func TestTimeNowEvaluateAfter_FutureRecorded(t *testing.T) {
	now := mustTime(t, "2026-03-15T12:00:00Z")
	collector := &RequeueCollector{}
	got := evalTime(t, now, collector, `time.now(timestamp("2026-03-15T17:00:00Z"))`).(time.Time)
	assert.True(t, got.Equal(now), "value should still be the snapshot")

	at, ok := collector.Earliest()
	require.True(t, ok, "future evaluateAfter should be recorded")
	assert.True(t, at.Equal(mustTime(t, "2026-03-15T17:00:00Z")))
}

func TestTimeNowEvaluateAfter_PastIgnored(t *testing.T) {
	now := mustTime(t, "2026-03-15T12:00:00Z")
	collector := &RequeueCollector{}
	evalTime(t, now, collector, `time.now(timestamp("2026-03-15T09:00:00Z"))`)
	_, ok := collector.Earliest()
	assert.False(t, ok, "past evaluateAfter must be ignored (no hot loop)")
}

func TestTimeNowEvaluateAfter_KeepsEarliest(t *testing.T) {
	now := mustTime(t, "2026-03-15T12:00:00Z")
	collector := &RequeueCollector{}
	evalTime(t, now, collector, `time.now(timestamp("2026-03-15T18:00:00Z"))`)
	evalTime(t, now, collector, `time.now(timestamp("2026-03-15T15:00:00Z"))`)
	at, ok := collector.Earliest()
	require.True(t, ok)
	assert.True(t, at.Equal(mustTime(t, "2026-03-15T15:00:00Z")), "should keep the earliest, got %s", at)
}

func TestWithTime_UTC_DefaultsZero(t *testing.T) {
	now := mustTime(t, "2026-03-15T12:34:56Z")
	// only hours specified -> minutes/seconds default to 0
	got := evalTime(t, now, nil, `time.now(null).withTime({hours: 9}, "Etc/UTC")`).(time.Time)
	assert.True(t, got.Equal(mustTime(t, "2026-03-15T09:00:00Z")), "got %s", got.UTC())
}

func TestWithTime_Timezone_DSTSafe(t *testing.T) {
	// 2026-03-15 is EDT (UTC-4); 09:00 America/New_York == 13:00 UTC.
	now := mustTime(t, "2026-03-15T12:34:56Z")
	got := evalTime(t, now, nil, `time.now(null).withTime({hours: 9, minutes: 0}, "America/New_York")`).(time.Time)
	assert.Equal(t, "2026-03-15T13:00:00Z", got.UTC().Format(time.RFC3339), "9am New York should be 13:00Z in EDT")
}

func TestWithTime_UnknownKeyErrors(t *testing.T) {
	env, err := cel.NewEnv(Time())
	require.NoError(t, err)
	_, iss := env.Compile(`time.now(null).withTime({hour: 9}, "Etc/UTC")`) // "hour" not "hours"
	require.Error(t, iss.Err())
	assert.Contains(t, iss.Err().Error(), "unknown field")
}

func TestAddDays(t *testing.T) {
	now := mustTime(t, "2026-03-15T12:00:00Z")
	got := evalTime(t, now, nil, `time.now(null).addDays(1, "Etc/UTC")`).(time.Time)
	assert.Equal(t, "2026-03-16T12:00:00Z", got.UTC().Format(time.RFC3339))
}

// --- adversarial / edge-case coverage ---

func TestWithTime_InvalidTimezoneErrors(t *testing.T) {
	now := mustTime(t, "2026-03-15T12:00:00Z")
	env, err := cel.NewEnv(Time())
	require.NoError(t, err)
	ast, iss := env.Compile(`time.now(null).withTime({hours: 9}, "Not/AZone")`)
	require.NoError(t, iss.Err())
	prg, err := env.Program(ast)
	require.NoError(t, err)
	_, _, err = prg.Eval(map[string]any{TimeVarName: NewTimeValue(now, nil)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timezone")
}

func TestWithTime_OutOfRangeErrors(t *testing.T) {
	now := mustTime(t, "2026-03-15T12:00:00Z")
	for _, expr := range []string{
		`time.now(null).withTime({hours: 24}, "Etc/UTC")`,
		`time.now(null).withTime({minutes: 60}, "Etc/UTC")`,
		`time.now(null).withTime({seconds: -1}, "Etc/UTC")`,
	} {
		env, err := cel.NewEnv(Time())
		require.NoError(t, err)
		ast, iss := env.Compile(expr)
		require.NoError(t, iss.Err(), expr)
		prg, err := env.Program(ast)
		require.NoError(t, err)
		_, _, err = prg.Eval(map[string]any{TimeVarName: NewTimeValue(now, nil)})
		require.Error(t, err, "expected range error for %q", expr)
		assert.Contains(t, err.Error(), "out of range")
	}
}

func TestWithTime_EmptyMapIsMidnight(t *testing.T) {
	now := mustTime(t, "2026-03-15T12:34:56Z")
	got := evalTime(t, now, nil, `time.now(null).withTime({}, "Etc/UTC")`).(time.Time)
	assert.Equal(t, "2026-03-15T00:00:00Z", got.UTC().Format(time.RFC3339))
}

func TestAddDays_NegativeZeroAndInvalidTZ(t *testing.T) {
	now := mustTime(t, "2026-03-15T12:00:00Z")
	assert.Equal(t, "2026-03-14T12:00:00Z", evalTime(t, now, nil, `time.now(null).addDays(-1, "Etc/UTC")`).(time.Time).UTC().Format(time.RFC3339))
	assert.Equal(t, "2026-03-15T12:00:00Z", evalTime(t, now, nil, `time.now(null).addDays(0, "Etc/UTC")`).(time.Time).UTC().Format(time.RFC3339))

	env, err := cel.NewEnv(Time())
	require.NoError(t, err)
	ast, iss := env.Compile(`time.now(null).addDays(1, "Bogus/Zone")`)
	require.NoError(t, iss.Err())
	prg, err := env.Program(ast)
	require.NoError(t, err)
	_, _, err = prg.Eval(map[string]any{TimeVarName: NewTimeValue(now, nil)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timezone")
}

func TestNowEvaluateAfter_ExactNowNotRecorded(t *testing.T) {
	now := mustTime(t, "2026-03-15T12:00:00Z")
	collector := &RequeueCollector{}
	// evaluateAfter == now: must NOT be recorded (strictly-future only).
	evalTime(t, now, collector, `time.now(timestamp("2026-03-15T12:00:00Z"))`)
	_, ok := collector.Earliest()
	assert.False(t, ok, "evaluateAfter == now should not schedule a requeue")
}

func TestNowEvaluateAfter_NilCollectorSafe(t *testing.T) {
	now := mustTime(t, "2026-03-15T12:00:00Z")
	// A nil collector (e.g. readyWhen-style path) must not panic.
	got := evalTime(t, now, nil, `time.now(timestamp("2030-01-01T00:00:00Z"))`).(time.Time)
	assert.True(t, got.Equal(now))
}

func TestWithTime_DuplicateKeyRejectedAtParse(t *testing.T) {
	env, err := cel.NewEnv(Time())
	require.NoError(t, err)
	_, iss := env.Compile(`time.now(null).withTime({hours: 9, hours: 10}, "Etc/UTC")`)
	require.Error(t, iss.Err())
	assert.Contains(t, iss.Err().Error(), "duplicate field")
}

// TestTicker_RestartBucketMonotonic validates the "roll every N minutes" pattern
// (KREP-025 example 2): int(time.now(null)) / intervalSeconds is a monotonic counter
// that increments exactly once per interval — unlike modulo, which wraps.
func TestTicker_RestartBucketMonotonic(t *testing.T) {
	base := mustTime(t, "2026-03-15T12:00:00Z")
	expr := `int(time.now(null)) / 600` // 600s = 10 minutes

	b0 := evalTime(t, base, nil, expr).(int64)
	b1 := evalTime(t, base.Add(600*time.Second), nil, expr).(int64)
	b10 := evalTime(t, base.Add(6000*time.Second), nil, expr).(int64)

	assert.Equal(t, int64(1), b1-b0, "one interval later -> bucket + 1")
	assert.Equal(t, int64(10), b10-b0, "ten intervals later -> bucket + 10")
}

// TestTicker_SchedulesRequeue proves the self-ticking part: nesting time.now(null)
// schedules the next reconcile ~one interval out.
func TestTicker_SchedulesRequeue(t *testing.T) {
	base := mustTime(t, "2026-03-15T12:00:00Z")
	c := &RequeueCollector{}
	evalTime(t, base, c, `int(time.now(time.now(null) + duration("10m"))) / 600`)
	at, ok := c.Earliest()
	require.True(t, ok)
	assert.True(t, at.Equal(base.Add(10*time.Minute)), "requeue at now + 10m, got %s", at)
}
func TestBusinessHoursBoundary(t *testing.T) {
	// 12:00 UTC: inside a 9-17 window. next close (today 17:00) < next open
	// (tomorrow 09:00), so the gate is true (inside).
	now := mustTime(t, "2026-03-16T12:00:00Z")
	expr := `
		cel.bind(o, time.now(null).withTime({hours: 9}, "Etc/UTC"),
		cel.bind(c, time.now(null).withTime({hours: 17}, "Etc/UTC"),
		cel.bind(nextOpen,  time.now(null) < o ? o : o.addDays(1, "Etc/UTC"),
		cel.bind(nextClose, time.now(null) < c ? c : c.addDays(1, "Etc/UTC"),
			nextClose < nextOpen))))`
	env, err := cel.NewEnv(Time(), ext.Bindings())
	require.NoError(t, err)
	ast, iss := env.Compile(expr)
	require.NoError(t, iss.Err())
	prg, err := env.Program(ast)
	require.NoError(t, err)
	out, _, err := prg.Eval(map[string]any{TimeVarName: NewTimeValue(now, nil)})
	require.NoError(t, err)
	assert.Equal(t, true, out.Value(), "12:00 should be inside business hours")
}
