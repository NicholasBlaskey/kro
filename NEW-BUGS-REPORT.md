# New High-Impact Bugs in KRO

This report documents 3 NEW high-impact bugs found in the KRO codebase that are NOT already documented in /home/nblaskey/bugFix/*.md.

## Bug 1: Integer Overflow in Cartesian Product (DoS/Crash)

**Severity**: HIGH - Denial of Service, Potential Controller Crash

**Location**: `/local/home/nblaskey/kro/pkg/runtime/collection.go:87-100`

**Description**: 
The `cartesianProduct` function has an integer overflow vulnerability. When computing the total size of a cartesian product, it multiplies dimension sizes together without checking for overflow. On 64-bit systems, multiplying large numbers can wrap around to negative or small positive values, bypassing the `maxCollectionSize` check.

**Vulnerable Code**:
```go
func cartesianProduct(dimensions []evaluatedDimension, maxCollectionSize int) ([]map[string]any, error) {
    // ...
    total := 1
    for _, dim := range dimensions {
        if len(dim.values) == 0 {
            return nil, nil
        }
        total *= len(dim.values)  // INTEGER OVERFLOW - no check before multiplication
        if total > maxCollectionSize {
            return nil, fmt.Errorf("collection size of %d is over the maximum collection size of %d", total, maxCollectionSize)
        }
    }
    // ...
}
```

**Attack Scenario**:
1. Create an RGD with 3 dimensions of 20 items each: 20 * 20 * 20 = 8,000 items
2. Or 4 dimensions of 50 items: 50^4 = 6,250,000 items
3. With careful crafting, cause integer overflow: e.g., dimensions that multiply to > 2^63

**Impact**:
- Bypass max collection size limits
- Allocate massive amounts of memory
- Controller panic/crash when attempting to allocate
- DoS other reconciliations
- Memory exhaustion on the node

**Reproduction**:
```bash
kubectl apply -f bug-cartesian-overflow.yaml
# Controller will attempt to create 20^3 = 8,000 ConfigMaps
# With 4 dimensions of 20, that would be 160,000 items
```

**Fix Required**:
Check for overflow before each multiplication:
```go
if total > maxCollectionSize/len(dim.values) {
    return nil, fmt.Errorf("collection size would exceed maximum")
}
total *= len(dim.values)
```

---

## Bug 2: Nil Pointer Dereference in Namespace Normalization (Crash)

**Severity**: HIGH - Controller Panic/Crash

**Location**: `/local/home/nblaskey/kro/pkg/runtime/node_resolve.go:308-328`

**Description**:
The `normalizeNamespaces` function dereferences `n.deps[graph.InstanceNodeID].observed[0]` without checking if the observed slice is non-empty. For cluster-scoped instances or during initial reconciliation, the observed array may be nil or empty, causing a panic.

**Vulnerable Code**:
```go
func (n *Node) normalizeNamespaces(objs []*unstructured.Unstructured) error {
    if !n.Spec.Meta.Namespaced {
        return nil
    }
    ns := n.deps[graph.InstanceNodeID].observed[0].GetNamespace()  // PANIC - no bounds check
    // ...
}
```

**Attack Scenario**:
1. Create a cluster-scoped RGD with namespaced child resources
2. Apply an instance before the instance itself is observed
3. Controller attempts to normalize namespaces on first reconciliation
4. `observed` is nil or empty -> index out of bounds -> panic

**Impact**:
- Controller crash
- Reconciliation loop breaks
- All instances using that RGD fail
- Requires controller restart

**Reproduction**:
```bash
kubectl apply -f bug-nil-deref-namespace.yaml
# Create a cluster-scoped instance
kubectl create -f - <<EOF
apiVersion: v1alpha1
kind: NilNamespaceTest
metadata:
  name: test-instance
spec:
  name: test
EOF
# Controller may panic during first reconciliation
```

**Fix Required**:
Add nil/empty check before dereferencing:
```go
func (n *Node) normalizeNamespaces(objs []*unstructured.Unstructured) error {
    if !n.Spec.Meta.Namespaced {
        return nil
    }
    instanceNode := n.deps[graph.InstanceNodeID]
    if instanceNode.observed == nil || len(instanceNode.observed) == 0 {
        return fmt.Errorf("instance not yet observed")
    }
    ns := instanceNode.observed[0].GetNamespace()
    // ...
}
```

---

## Bug 3: Negative Value Validation Missing in Length Markers (Schema Violation)

**Severity**: HIGH - Invalid CRD Generation, Kubernetes API Server Rejection

**Location**: `/local/home/nblaskey/kro/pkg/simpleschema/markers.go:361-436`

**Description**:
The marker validation functions `applyMinLengthMarker`, `applyMaxLengthMarker`, `applyMinItemsMarker`, and `applyMaxItemsMarker` parse integer values but don't validate they are non-negative. OpenAPI schema spec requires these fields to be >= 0. Negative values will cause CRD creation to fail or be rejected by the Kubernetes API server.

**Vulnerable Code**:
```go
func applyMinLengthMarker(schema *extv1.JSONSchemaProps, marker *Marker) error {
    if schema.Type != schemaTypeString {
        return fmt.Errorf("minLength marker is only valid for string types, got type: %s", schema.Type)
    }
    val, err := strconv.ParseInt(marker.Value, 10, 64)
    if err != nil {
        return fmt.Errorf("failed to parse minLength value: %w", err)
    }
    // NO CHECK: val < 0
    schema.MinLength = &val  // INVALID if negative
    return nil
}
```

Same issue exists in:
- `applyMaxLengthMarker` (line 373-382)
- `applyMinItemsMarker` (line 415-424)
- `applyMaxItemsMarker` (line 427-436)

**Attack Scenario**:
1. Create an RGD with `minLength=-1` or `minItems=-5`
2. Controller attempts to generate CRD
3. CRD is rejected by API server validation
4. RGD is marked as failed but error is cryptic
5. All instances fail to create

**Impact**:
- RGD fails to build
- CRD creation fails
- Users cannot create instances
- Confusing error messages (API server rejection)
- No early validation feedback

**Reproduction**:
```bash
kubectl apply -f bug-negative-length.yaml
# Check RGD status - should show CRD creation failed
kubectl get rgd negative-length-test -o yaml | grep -A 10 status
```

**Fix Required**:
Add non-negative validation:
```go
func applyMinLengthMarker(schema *extv1.JSONSchemaProps, marker *Marker) error {
    if schema.Type != schemaTypeString {
        return fmt.Errorf("minLength marker is only valid for string types, got type: %s", schema.Type)
    }
    val, err := strconv.ParseInt(marker.Value, 10, 64)
    if err != nil {
        return fmt.Errorf("failed to parse minLength value: %w", err)
    }
    if val < 0 {
        return fmt.Errorf("minLength must be non-negative, got: %d", val)
    }
    schema.MinLength = &val
    return nil
}
```

Apply same fix to maxLength, minItems, maxItems.

---

## Additional Context

### Bug Discovery Method
- Static code analysis focusing on:
  - Missing bounds checks (nil, empty, index)
  - Integer overflow in arithmetic operations
  - Missing validation before type conversions
  - Panic-inducing patterns

### Why These Are High Impact
1. **Cartesian Overflow**: Direct DoS vector, no authentication needed, affects all users
2. **Nil Dereference**: Controller crash affects entire cluster, all RGDs fail
3. **Negative Length**: Breaks core functionality (CRD generation), poor UX

### Testing Priority
1. Bug #2 (Nil Deref) - Highest priority, easiest to reproduce, causes crash
2. Bug #1 (Overflow) - DoS vector, resource exhaustion
3. Bug #3 (Negative) - UX issue, easier to work around

### Related Issues
These bugs are distinct from already-documented issues:
- NOT the default value validation bypass
- NOT the JSON escaping bug
- NOT the min/max type validation issue (this is about negative values)
- NOT the CEL error deletion stuck bug
- NOT the copy-paste error messages

All three bugs can be reproduced by running the controller and applying the test YAML files provided.
