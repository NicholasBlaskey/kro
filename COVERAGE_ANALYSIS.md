# Integration Test Coverage Analysis

**Date:** 2026-04-21  
**Coverage File:** integration-cover.out  
**Goal:** Identify untested customer-facing features that could cause bugs in production

## Executive Summary

Found **5 critical feature gaps** with 0% test coverage that customers will likely encounter:

1. **Number/Float type validation** (minimum/maximum markers)
2. **CEL library functions** (hash, json, lists, maps)  
3. **String validation markers** (minLength, maxLength, pattern)
4. **Array validation markers** (minItems, maxItems, uniqueItems)
5. **Enum validation for integers**

## Critical Findings

### 1. Number Type Validation - 0% Coverage 🚨

**Files:**
- `pkg/simpleschema/markers.go:290-306` (applyMinimumMarker, applyMaximumMarker)

**Customer Impact:** HIGH
- Users can define schemas with `minimum` and `maximum` constraints on number/integer fields
- **These constraints are NEVER tested** in integration tests
- Potential bugs:
  - Float parsing errors could cause RGD creation to fail silently
  - Min/max validation might not actually enforce constraints
  - Edge cases (negative numbers, decimals, large values) untested

**Example untested scenario:**
```yaml
variables:
  spec:
    cpu: number | minimum=0.5 maximum=64.0
    memory: integer | minimum=512 maximum=16384
```

**Risk:** Customer creates RGD with min/max, assumes validation works, but:
- May accept invalid values outside range
- May fail to parse float values correctly
- May have precision issues with decimal numbers

### 2. CEL Library Functions - 0% Coverage 🚨

**Files:**
- `pkg/cel/library/hash.go`: fnv64aHash, sha256Hash, md5Hash (0%)
- `pkg/cel/library/json.go`: unmarshalJSON, marshalJSON (0%)
- `pkg/cel/library/lists.go`: setAtIndex, insertAtIndex, removeAtIndex (0%)
- `pkg/cel/library/maps.go`: merge (0%)

**Customer Impact:** CRITICAL
- These are documented user-facing CEL functions
- **Zero test coverage** means they've never been exercised
- Potential bugs:
  - Wrong argument may cause panics instead of errors
  - Type coercion issues (string vs bytes for hash functions)
  - List/map mutation functions may corrupt data structures
  - JSON marshal/unmarshal may fail on complex nested types

**Example untested scenarios:**
```yaml
resources:
  - id: configmap
    spec:
      data:
        hash: ${hash.fnv64a(schema.spec.name).base64()}
        config: ${json.marshal(schema.spec.config)}
        merged: ${schema.spec.defaults.merge(schema.spec.overrides)}
```

**Risk:** Customer uses these functions in production, hits edge case:
- Panic on nil/invalid input → RGD reconciliation crash
- Wrong output type → downstream resource creation fails
- Data corruption in list manipulation

### 3. String Validation - 0% Coverage 🚨

**Files:**
- `pkg/simpleschema/markers.go:360-395` (minLength, maxLength, pattern)

**Customer Impact:** MEDIUM-HIGH
- Common validation patterns for names, URLs, emails, etc.
- **Never tested** in integration
- Potential bugs:
  - Pattern regex compilation might fail at runtime vs creation time
  - Invalid regex could crash the controller
  - Length validation might not actually work

**Example untested scenario:**
```yaml
variables:
  spec:
    name: string | minLength=3 maxLength=63 pattern="^[a-z0-9-]+$"
    email: string | pattern="^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"
```

**Risk:** 
- Invalid regex accepted at RGD creation, crashes controller during validation
- Min/max length not enforced → invalid K8s resource names slip through

### 4. Array Validation - 0% Coverage 🚨

**Files:**
- `pkg/simpleschema/markers.go:398-435` (minItems, maxItems, uniqueItems)

**Customer Impact:** MEDIUM
- Common for lists of IPs, ports, tags, etc.
- **Zero coverage**
- Potential bugs:
  - uniqueItems marker may not set x-kubernetes-list-type correctly
  - Min/max items validation might be ignored

**Example untested scenario:**
```yaml
variables:
  spec:
    ports: array | minItems=1 maxItems=10 uniqueItems=true
    tags: array | maxItems=50
```

**Risk:** Customer expects uniqueness validation, but duplicates are allowed

### 5. Integer Enum Validation - 0% Coverage

**Files:**
- `pkg/simpleschema/markers.go:344-349` (integer enum parsing)

**Customer Impact:** MEDIUM
- Integer enums are supported but **never tested**
- String enum works (has coverage) but integer path untested
- Potential bugs:
  - ParseInt errors might not be handled correctly
  - Could accept non-integer values in integer enum

**Example untested scenario:**
```yaml
variables:
  spec:
    priority: integer | enum=1,2,3,5,8
    replicas: integer | enum=1,3,5,10
```

## Likely Bugs to Investigate

### Bug Candidate #1: Float Precision in Min/Max Validation

**Location:** `pkg/simpleschema/markers.go:290-305`

```go
func applyMinimumMarker(schema *extv1.JSONSchemaProps, marker *Marker) error {
	val, err := strconv.ParseFloat(marker.Value, 64)  // ← Never tested
	if err != nil {
		return fmt.Errorf("failed to parse minimum enum value: %w", err)  // ← Typo: "enum value"?
	}
	schema.Minimum = &val
	return nil
}
```

**Suspected Issues:**
1. Error message says "enum value" but this is for minimum - copy-paste bug?
2. No test for float parsing means edge cases untested:
   - Scientific notation: `1.5e10`
   - Very large numbers: `9999999999.99`
   - Negative numbers: `-42.5`
   - Precision: `0.333333333333`

**Reproduction:**
```yaml
kind: ResourceGraphDefinition
spec:
  schema:
    spec:
      type: object
      properties:
        ratio: number | minimum=0.333333333333 maximum=1.5e10
```

**Expected:** Should validate correctly  
**Risk:** May fail to parse, or lose precision

### Bug Candidate #2: JSON Marshal with Sentinels/Special Types

**Location:** `pkg/cel/library/json.go:131-143`

```go
func marshalJSON(value ref.Val) ref.Val {
	native, err := conversion.GoNativeType(value)  // ← Never tested with complex types
	if err != nil {
		return types.NewErr("json.marshal failed to convert value: %s", err.Error())
	}
	
	jsonBytes, err := json.Marshal(native)
	if err != nil {
		return types.NewErr("json.marshal failed to encode value: %s", err.Error())
	}
	
	return types.String(string(jsonBytes))
}
```

**Suspected Issues:**
1. What happens if value contains `sentinels.Omit`? Should it be filtered?
2. What if value has circular references?
3. CEL optional types - are they unwrapped correctly?

**Reproduction:**
```yaml
resources:
  - id: configmap
    spec:
      data:
        # What if schema.spec.config has omit sentinels?
        config: ${json.marshal(schema.spec.config)}
```

**Risk:** Panic or wrong JSON output if special CEL types not handled

### Bug Candidate #3: List Mutation Index Bounds

**Location:** `pkg/cel/library/lists.go:139-164`

```go
func listsSetAtIndex(args ...ref.Val) ref.Val {
	// ...
	if idx < 0 || idx >= size {
		return types.NewErr("lists.setAtIndex: index %d out of bounds [0, %d)", idx, size)
	}
	// ...
}
```

**Suspected Issues:**
1. Never tested, so boundary conditions unknown
2. What if list is empty? Size = 0, valid index range is [0, 0) = nothing
3. What if index is negative in CEL expression that evaluates later?
4. Integer overflow with very large lists?

**Reproduction:**
```yaml
resources:
  - id: configmap
    spec:
      data:
        # Edge cases never tested
        empty: ${lists.setAtIndex([], 0, "x")}  # Should this error?
        negative: ${lists.setAtIndex([1,2,3], -1, 99)}  # Already caught
        boundary: ${lists.setAtIndex([1], 0, 99)}  # idx=0, size=1, valid
```

**Risk:** Off-by-one errors, unexpected panics

## Recommendations

### High Priority Tests Needed

1. **Number validation suite:**
   - Float min/max with decimals, scientific notation, edge values
   - Integer min/max with negative, zero, large values
   - Invalid values (strings, nulls)

2. **CEL library integration tests:**
   - Each hash function with various inputs (empty string, unicode, large strings)
   - JSON marshal/unmarshal roundtrip with complex nested objects
   - List mutations with empty lists, single item, boundaries
   - Map merge with overlapping keys, empty maps, nested maps

3. **Schema validation markers:**
   - String patterns with complex regex, invalid regex
   - Array uniqueness enforcement
   - Integer enums with invalid values

### Test Coverage Goals

Current integration coverage: ~85% (by line)  
**Gap:** User-facing validation features at 0%

**Target:**
- Core validation markers: 80%+ coverage
- CEL library functions: 90%+ coverage (user-facing API)
- Type conversion edge cases: 75%+ coverage

## Conclusion

The integration test suite has **excellent coverage of the core reconciliation logic** but **zero coverage of user-facing schema validation and CEL library features**. This creates high risk for customer-impacting bugs in:

1. Type validation (number min/max)
2. CEL expression evaluation (library functions)
3. Schema markers (string/array validation)

**These are features customers will use immediately** when defining ResourceGraphDefinitions, making this a critical gap.

**ROI of fixing:** High - these are common user workflows that will hit production first.
