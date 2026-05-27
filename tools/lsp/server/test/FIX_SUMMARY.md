# Go-to-Definition Fix - Final Summary

## Problem
When using go-to-definition on `${schema.spec.fullDNSName}`, the LSP was showing a dropdown with 3 options instead of making each segment independently clickable. Additionally, the links weren't redirecting to the correct definitions.

## Solution
Modified the definition provider to return only ONE location - the definition of whichever segment the cursor is actually on.

### How It Works Now

When you have an expression like `${schema.spec.fullDNSName}`:

1. **Click on "schema"** → Jumps to the schema definition (where `schema:` is declared in the RGD)
2. **Click on "spec"** → No definition (it's a built-in K8s field)
3. **Click on "fullDNSName"** → No definition (it's a schema spec field defined inline)

Each segment is independently clickable with Ctrl+Click (or Cmd+Click on Mac).

### Example

```yaml
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: testapp
spec:
  schema:              ← Clicking "schema" in the expression below jumps here
    kind: TestApp
    apiVersion: v1alpha1
    spec:
      name: string
      replicas: integer
      fullDNSName: string
  resources:
    - id: deployment    ← Clicking "deployment" in the expression below jumps here
      template:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: ${schema.spec.fullDNSName}
                  ↑      ↑    ↑
                  |      |    └─ Click here: no jump (schema field)
                  |      └────── Click here: no jump (K8s field)
                  └───────────── Click here: jumps to "schema:" above
          
          labels:
            app: ${deployment.metadata.name}
                    ↑
                    └─────────── Click here: jumps to "id: deployment" above
```

## Key Changes

### 1. Modified `ProvideDefinition` (`services/definition.go`)

**Before**: Returned all segments as separate locations in a list
```go
return []protocol.Location{
    {Range: schemaRange},    // All three returned
    {Range: specRange},      // Shown as dropdown
    {Range: fullDNSNameRange},
}
```

**After**: Returns only the definition of the clicked segment
```go
// Find which segment cursor is on
if on "schema" → return schema definition location
if on "spec" → return nil (K8s field, no definition)
if on "fullDNSName" → return nil (schema field, no definition)
```

### 2. Segment Detection Logic

```go
// Extract full path and segment ranges
pathExpr, segmentRanges := dp.extractPathExpressionAtPosition(content, position)

// Find which segment the cursor is on
for i, segRange := range segmentRanges {
    if position.Character >= segRange.Start.Character && 
       position.Character <= segRange.End.Character {
        segmentIndex = i
        break
    }
}

// Return definition only for the clicked segment
if segmentIndex == 0 {
    // First segment - resource/schema/iterator
    return resourceDefinitionLocation
} else {
    // Field segment - try to find field definition
    // Returns nil for K8s fields (expected)
}
```

### 3. Position Calculation

The character positions are calculated relative to the line content:

```
          name: ${schema.spec.fullDNSName}
          012345678901234567890123456789012345678
                    1111111111222222222233333333
          
          Char 20: 's' in "schema"    ← Returns schema definition
          Char 27: 's' in "spec"      ← Returns nil (K8s field)
          Char 32: 'f' in "fullDNSName" ← Returns nil (schema field)
```

## Test Coverage

Added comprehensive tests in `definition_multi_segment_test.go`:

1. **TestDefinition_MultipleSegmentClicks** - Verifies clicking different segments of same path
2. **TestDefinition_DifferentResources** - Verifies clicking different resource names
3. **TestDefinition_OperatorExpressions** - Verifies definition in complex CEL expressions

All tests passing:
```
✅ click_on_schema → jumps to line 6 (schema definition)
✅ click_on_deployment → jumps to line 13 (deployment resource)
✅ click_on_service → jumps to line 19 (service resource)
✅ first_resource_in_addition → jumps to correct resource
✅ second_resource_in_addition → jumps to correct resource
```

## Edge Cases Handled

1. **K8s Fields**: Fields like `spec`, `status`, `metadata` return nil (no definition in RGD)
2. **Schema Fields**: Fields like `fullDNSName` defined in schema spec return nil (inline definition)
3. **Operator Expressions**: Works in `${a+b.field}`, `${a?b:c}`, `${a&&b}`, etc.
4. **Nested Paths**: Works for deep paths like `pod.spec.containers.env.valueFrom.secretKeyRef`

## User Experience

### Before:
```
Click on ${schema.spec.fullDNSName}
   ↓
Dropdown menu with 3 options:
  [1] schema.spec.fullDNSName (line 18, char 18)
  [2] schema.spec.fullDNSName (line 18, char 25)
  [3] schema.spec.fullDNSName (line 18, char 32)
   ↓
All options point back to the same line (the expression itself)
```

### After:
```
Click on "schema" in ${schema.spec.fullDNSName}
   ↓
Immediately jumps to schema definition (line 6)

Click on "spec" in ${schema.spec.fullDNSName}
   ↓
No jump (K8s field, no definition)

Click on "fullDNSName" in ${schema.spec.fullDNSName}
   ↓
No jump (schema field, defined inline)
```

## Compatibility

This change is **backwards compatible** and improves the standard LSP behavior:
- Single resource references like `${deployment}` work exactly as before
- Multi-segment paths now work correctly (each segment clickable)
- No changes to completion, hover, or other features

## Performance

- **O(n) where n is path length** - scans segments once to find clicked segment
- **O(1) symbol lookup** - map lookup for resource/schema definition
- No performance impact vs previous implementation

## Future Enhancements (Optional)

1. **Field definitions in schema**: Could jump to where schema spec fields are defined
2. **K8s documentation links**: Could link `spec`, `status` to K8s API docs
3. **DocumentHighlight**: Could highlight all three segments when hovering over any part
4. **Peek definition**: Show inline preview without jumping

## How to Test

1. **Start LSP server**:
   ```bash
   kro lsp server --offline
   ```

2. **Open an RGD file** with expressions like `${schema.spec.name}`

3. **Try clicking on each part**:
   - Ctrl+Click (or Cmd+Click) on "schema" → should jump
   - Ctrl+Click on "spec" → no jump (expected)
   - Ctrl+Click on "name" → no jump (expected)

4. **Try different resources**:
   - `${deployment.spec.replicas}` - click "deployment" → jumps to resource
   - `${service.metadata.name}` - click "service" → jumps to resource

5. **Try in operators**:
   - `${schema.spec.name+deployment.spec.replicas}`
   - Click "schema" → jumps
   - Click "deployment" → jumps

## Related Files

- `services/definition.go` - Main implementation
- `definition_segments_test.go` - Basic segment tests
- `definition_multi_segment_test.go` - Comprehensive multi-segment tests
- `analysis/completion_context.go` - Path parsing utilities
