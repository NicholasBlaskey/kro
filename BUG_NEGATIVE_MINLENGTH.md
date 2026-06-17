# Bug Report: Negative minLength/maxLength Accepted - Invalid OpenAPI Schema

**Bug ID:** KRO-MARKER-005  
**Severity:** MEDIUM  
**Component:** `pkg/simpleschema/markers.go:361-382`  
**Status:** CONFIRMED - Easily Reproducible  
**Date Discovered:** 2026-04-30  

---

## Summary

The `applyMinLengthMarker` and `applyMaxLengthMarker` functions do not validate that the length values are non-negative. This allows invalid OpenAPI schemas to be created with negative length constraints, which violates the JSON Schema specification and may cause issues with:
- OpenAPI validators
- Code generators  
- kubectl validation
- Third-party tools

---

## Root Cause

**File:** `pkg/simpleschema/markers.go:361-382`

```go
func applyMinLengthMarker(schema *extv1.JSONSchemaProps, marker *Marker) error {
	if schema.Type != schemaTypeString {
		return fmt.Errorf("minLength marker is only valid for string types, got type: %s", schema.Type)
	}
	val, err := strconv.ParseInt(marker.Value, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse minLength value: %w", err)
	}
	schema.MinLength = &val  // ← BUG: No validation that val >= 0
	return nil
}

func applyMaxLengthMarker(schema *extv1.JSONSchemaProps, marker *Marker) error {
	if schema.Type != schemaTypeString {
		return fmt.Errorf("maxLength marker is only valid for string types, got type: %s", schema.Type)
	}
	val, err := strconv.ParseInt(marker.Value, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse maxLength value: %w", err)
	}
	schema.MaxLength = &val  // ← BUG: No validation that val >= 0
	return nil
}
```

**Problem:**
- No check: `if val < 0`
- Negative lengths are semantically meaningless
- Violates JSON Schema spec (minLength/maxLength must be non-negative integers)

---

## JSON Schema Specification

From [JSON Schema Validation Spec](https://json-schema.org/draft/2020-12/json-schema-validation.html#rfc.section.6.3):

> **6.3.1. minLength**
> 
> The value of this keyword MUST be a non-negative integer.
>
> **6.3.2. maxLength**
>
> The value of this keyword MUST be a non-negative integer.

**Negative values are explicitly forbidden by the spec.**

---

## Reproduction

### Step 1: Create RGD with Negative minLength

```yaml
# test-negative-length.yaml
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test-negative-length
spec:
  schema:
    apiVersion: v1alpha1
    kind: NegativeLengthTest
    spec:
      name: "string | minLength=-5"
  resources:
    - id: cm
      template:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: test
        data:
          name: ${schema.spec.name}
```

### Step 2: Apply

```bash
kubectl apply -f test-negative-length.yaml
```

**Expected:** RGD should be rejected with error:
```
minLength must be non-negative, got: -5
```

**Actual:** RGD is ACCEPTED and becomes Active!

### Step 3: Inspect Generated CRD

```bash
kubectl get crd negativelengthtests.kro.run -o yaml | grep -B2 -A2 "minLength"
```

**Output:**
```yaml
properties:
  name:
    minLength: -5  # ← Invalid per JSON Schema spec!
    type: string
```

---

## Impact

### Severity: MEDIUM

**Why MEDIUM:**
1. **Spec violation** - Creates invalid OpenAPI schemas
2. **Tool compatibility** - May break validators, generators, linters
3. **Confusing behavior** - Negative length has no semantic meaning
4. **Future-proofing** - Kubernetes may reject these CRDs in future versions

### User-Facing Impact

**1. Validation Tool Failures**

Third-party OpenAPI validators will flag the schema as invalid:
```
Error: /spec/properties/name/minLength must be >= 0, got -5
```

**2. Code Generator Issues**

Tools that generate client code from CRDs may:
- Skip the invalid field
- Generate broken validation code
- Fail entirely

**3. Confusing Semantics**

Users see `minLength=-5` and wonder:
- Does it mean "no minimum"?
- Is it a bug in their YAML?
- Why was it accepted?

**4. kubectl Validation**

Future kubectl versions with stricter CRD validation may reject instances:
```
Error: Invalid CRD schema: minLength cannot be negative
```

---

## Examples of Invalid Usage

### Example 1: Negative minLength
```yaml
name: string | minLength=-10
```
**Current:** Accepted, creates `minLength: -10` in CRD  
**Should:** Reject with error

### Example 2: Negative maxLength
```yaml
name: string | maxLength=-1
```
**Current:** Accepted, creates `maxLength: -1` in CRD  
**Should:** Reject with error

### Example 3: Both Negative
```yaml
name: string | minLength=-5 maxLength=-1
```
**Current:** Accepted  
**Should:** Reject both

### Example 4: maxLength < minLength (also should be caught)
```yaml
name: string | minLength=10 maxLength=5
```
**Current:** Accepted (semantically impossible constraint)  
**Should:** Reject with error

---

## Fix

### Code Changes

**File:** `pkg/simpleschema/markers.go`

#### Change 1: Add Validation to applyMinLengthMarker

```diff
 func applyMinLengthMarker(schema *extv1.JSONSchemaProps, marker *Marker) error {
 	if schema.Type != schemaTypeString {
 		return fmt.Errorf("minLength marker is only valid for string types, got type: %s", schema.Type)
 	}
 	val, err := strconv.ParseInt(marker.Value, 10, 64)
 	if err != nil {
 		return fmt.Errorf("failed to parse minLength value: %w", err)
 	}
+	if val < 0 {
+		return fmt.Errorf("minLength must be non-negative, got: %d", val)
+	}
 	schema.MinLength = &val
 	return nil
 }
```

#### Change 2: Add Validation to applyMaxLengthMarker

```diff
 func applyMaxLengthMarker(schema *extv1.JSONSchemaProps, marker *Marker) error {
 	if schema.Type != schemaTypeString {
 		return fmt.Errorf("maxLength marker is only valid for string types, got type: %s", schema.Type)
 	}
 	val, err := strconv.ParseInt(marker.Value, 10, 64)
 	if err != nil {
 		return fmt.Errorf("failed to parse maxLength value: %w", err)
 	}
+	if val < 0 {
+		return fmt.Errorf("maxLength must be non-negative, got: %d", val)
+	}
 	schema.MaxLength = &val
 	return nil
 }
```

### Optional: Cross-Validation

Also check that minLength <= maxLength when both are present:

```go
func applyMaxLengthMarker(schema *extv1.JSONSchemaProps, marker *Marker) error {
	// ... existing validation ...
	if val < 0 {
		return fmt.Errorf("maxLength must be non-negative, got: %d", val)
	}
	// Cross-validate with minLength if present
	if schema.MinLength != nil && *schema.MinLength > val {
		return fmt.Errorf("maxLength (%d) must be >= minLength (%d)", val, *schema.MinLength)
	}
	schema.MaxLength = &val
	return nil
}
```

---

## Similar Issues

### minItems / maxItems

Check if the same issue exists for array length markers:

```bash
grep -A10 "applyMinItemsMarker\|applyMaxItemsMarker" pkg/simpleschema/markers.go
```

**If they also lack negative checks, fix them too.**

---

## Test Plan

### Unit Tests

**File:** `pkg/simpleschema/markers_test.go`

```go
func TestApplyMinLengthMarker_Negative(t *testing.T) {
	schema := &extv1.JSONSchemaProps{Type: "string"}
	marker := &Marker{
		MarkerType: MarkerTypeMinLength,
		Value:      "-5",
	}
	
	err := applyMinLengthMarker(schema, marker)
	
	require.Error(t, err)
	assert.Contains(t, err.Error(), "minLength must be non-negative")
	assert.Contains(t, err.Error(), "-5")
}

func TestApplyMaxLengthMarker_Negative(t *testing.T) {
	schema := &extv1.JSONSchemaProps{Type: "string"}
	marker := &Marker{
		MarkerType: MarkerTypeMaxLength,
		Value:      "-10",
	}
	
	err := applyMaxLengthMarker(schema, marker)
	
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maxLength must be non-negative")
}

func TestApplyMinLengthMarker_Zero(t *testing.T) {
	schema := &extv1.JSONSchemaProps{Type: "string"}
	marker := &Marker{
		MarkerType: MarkerTypeMinLength,
		Value:      "0",
	}
	
	err := applyMinLengthMarker(schema, marker)
	
	// Zero is valid per JSON Schema spec
	require.NoError(t, err)
	require.NotNil(t, schema.MinLength)
	assert.Equal(t, int64(0), *schema.MinLength)
}

func TestApplyMaxLengthMarker_LessThanMin(t *testing.T) {
	schema := &extv1.JSONSchemaProps{Type: "string"}
	minLen := int64(10)
	schema.MinLength = &minLen
	
	marker := &Marker{
		MarkerType: MarkerTypeMaxLength,
		Value:      "5",
	}
	
	err := applyMaxLengthMarker(schema, marker)
	
	// Optional cross-validation
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maxLength (5) must be >= minLength (10)")
}
```

### Integration Test

**File:** `test/integration/suites/core/marker_validation_test.go`

```go
var _ = Describe("Length Marker Validation", func() {
	It("should reject negative minLength", func(ctx SpecContext) {
		rgd := generator.NewResourceGraphDefinition("test-negative-minlength",
			generator.WithSchema("TestNegativeMinLength", "v1alpha1",
				map[string]interface{}{
					"name": "string | minLength=-5",
				},
				nil,
			),
			generator.WithResource("configmap", configMapTemplate, nil, nil),
		)
		
		Expect(env.Client.Create(ctx, rgd)).To(Succeed())
		
		Eventually(func(g Gomega) {
			var updated krov1alpha1.ResourceGraphDefinition
			g.Expect(env.Client.Get(ctx, client.ObjectKeyFromObject(rgd), &updated)).To(Succeed())
			
			condition := getCondition(updated.Status.Conditions, "GraphAccepted")
			g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(condition.Message).To(ContainSubstring("minLength must be non-negative"))
		}).WithTimeout(10 * time.Second).Should(Succeed())
	})
	
	It("should accept zero minLength", func(ctx SpecContext) {
		rgd := generator.NewResourceGraphDefinition("test-zero-minlength",
			generator.WithSchema("TestZeroMinLength", "v1alpha1",
				map[string]interface{}{
					"name": "string | minLength=0",
				},
				nil,
			),
			generator.WithResource("configmap", configMapTemplate, nil, nil),
		)
		
		Expect(env.Client.Create(ctx, rgd)).To(Succeed())
		
		Eventually(func(g Gomega) {
			var updated krov1alpha1.ResourceGraphDefinition
			g.Expect(env.Client.Get(ctx, client.ObjectKeyFromObject(rgd), &updated)).To(Succeed())
			
			condition := getCondition(updated.Status.Conditions, "GraphAccepted")
			g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		}).WithTimeout(10 * time.Second).Should(Succeed())
	})
})
```

---

## Verification

### Before Fix

```bash
kubectl apply -f test-negative-length.yaml
kubectl get rgd test-negative-length -o jsonpath='{.status.state}'
# Output: Active  ← WRONG

kubectl get crd negativelengthtests.kro.run -o yaml | grep minLength
# Output: minLength: -5  ← INVALID
```

### After Fix

```bash
kubectl apply -f test-negative-length.yaml
kubectl get rgd test-negative-length -o jsonpath='{.status.conditions[?(@.type=="GraphAccepted")].message}'
# Output: "minLength must be non-negative, got: -5"  ← CORRECT
```

---

## Priority Justification

**Why MEDIUM priority:**

1. **Spec compliance** - Violates JSON Schema standard
2. **Tool compatibility** - May break third-party tools
3. **Easy fix** - 3 lines per function
4. **Low user impact** - Most users won't accidentally use negative values
5. **Future risk** - Kubernetes may enforce stricter validation

**Should fix before GA** - Core validation should comply with OpenAPI spec.

---

## Related Bugs

Check for similar issues in:
- `applyMinItemsMarker` / `applyMaxItemsMarker` - array length
- `applyMinimumMarker` / `applyMaximumMarker` - numeric min/max (negative is valid here!)

**Note:** For minimum/maximum on numbers, negative values ARE valid (e.g., `temperature: integer | minimum=-273`), so don't add the same check there.

---

## References

- **JSON Schema Validation Spec:** https://json-schema.org/draft/2020-12/json-schema-validation.html#rfc.section.6.3
  - Section 6.3: Validation Keywords for Strings - minLength/maxLength must be non-negative
  
- **OpenAPI 3.0 Spec:** https://spec.openapis.org/oas/v3.0.0#schema-object
  - Lists minLength/maxLength as non-negative integers

---

## Sign-off

**Discovered by:** Edge case testing with negative marker values  
**Reproduced on:** Local development cluster  
**Severity Assessment:** P2 - Spec violation, tool compatibility risk  
**Estimated Fix Time:** 15 minutes (6 lines + tests)  
**Recommended Action:** Fix before GA release
