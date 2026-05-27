# Method Reference vs Method Call Fix

## The Issue

When typing `${app.replace.` (method reference without parens), the LSP was incorrectly suggesting string methods like `replace`, `split`, `contains`.

**Why this was wrong:**
- `app` is a string
- `app.replace` is a **method reference** (not a call)
- `app.replace` doesn't have a type yet (it's just a reference to the method)
- We shouldn't suggest `app.replace.replace` - that makes no sense!

**What should happen:**
- `app.replace` → unknown type (it's a method reference)
- `app.replace()` → returns string
- `app.replace().` → show string methods

## The Fix

Updated type inference to detect method references (method names without parentheses):

```go
// In InferCELExpressionType()
if strings.Contains(expr, ".") {
    // Check if the last segment is a known method name
    lastDot := strings.LastIndex(expr, ".")
    if lastDot != -1 && lastDot < len(expr)-1 {
        lastSegment := expr[lastDot+1:]
        // If this is a known method name WITHOUT parens, it's a reference
        if isKnownMethod(lastSegment) {
            return CELTypeUnknown  // Don't infer method return type
        }
    }
    return inferFieldAccessType(expr, symbolTable)
}
```

**New helper function:**
```go
func isKnownMethod(name string) bool {
    knownMethods := map[string]bool{
        // String methods
        "contains": true, "startsWith": true, "endsWith": true, "matches": true,
        "split": true, "replace": true, "substring": true, "trim": true,
        "lowerAscii": true, "upperAscii": true,
        // List methods
        "filter": true, "map": true, "all": true, "exists": true, "exists_one": true,
        // Universal methods
        "size": true,
        // Map methods
        "merge": true,
    }
    return knownMethods[name]
}
```

## Behavior Change

### Before Fix

```yaml
${app.replace.|
```
**Shows:** `replace`, `split`, `contains`, ... (WRONG!)
- Incorrectly inferred `app.replace` as a string
- Suggested methods on a method reference

### After Fix

```yaml
${app.replace.|
```
**Shows:** Nothing (CORRECT!)
- `app.replace` is recognized as a method reference
- Type inference returns `CELTypeUnknown`
- No methods suggested

### Method Calls Still Work

```yaml
${app.replace('x', 'y').|
```
**Shows:** `replace`, `split`, `contains`, ... (CORRECT!)
- `app.replace('x', 'y')` is a method call
- Returns string
- String methods shown

## Examples

### String Methods

**Method reference (no parens):**
```yaml
${schema.spec.name.replace.|
# Shows: nothing
```

**Method call (with parens):**
```yaml
${schema.spec.name.replace('old', 'new').|
# Shows: split, contains, replace, trim, etc.
```

### List Methods

**Method reference:**
```yaml
${items.filter.|
# Shows: nothing
```

**Method call:**
```yaml
${items.filter(x, x > 5).|
# Shows: filter, map, all, exists, size
```

### Map Methods

**Method reference:**
```yaml
${deployment.metadata.labels.merge.|
# Shows: nothing
```

**Method call:**
```yaml
${deployment.metadata.labels.merge({'extra': 'label'}).|
# Shows: merge, size
```

## Testing

### Automated Tests

Added 3 test cases to `cel_type_inference_test.go`:

```go
{
    name:     "method reference: schema.spec.name.replace (no parens)",
    expr:     "schema.spec.name.replace",
    expected: CELTypeUnknown,
},
{
    name:     "method reference: schema.spec.name.split (no parens)",
    expr:     "schema.spec.name.split",
    expected: CELTypeUnknown,
},
{
    name:     "method reference: deployment.metadata.labels.merge (no parens)",
    expr:     "deployment.metadata.labels.merge",
    expected: CELTypeUnknown,
},
```

**All tests pass!** ✅

### Manual Testing

Use `/tmp/test-method-reference-fix.yaml`:

**TEST 1:** Type `${schema.spec.name.replace.|`
- Should show: Nothing (replace is a method reference)

**TEST 2:** Type `${schema.spec.name.replace('x', 'y').|`
- Should show: String methods (split, contains, etc.)

**TEST 3:** Type `${deployment.metadata.labels.app.split.|`
- Should show: Nothing (split is a method reference)

**TEST 4:** Type `${deployment.metadata.labels.app.split('-').|`
- Should show: List methods (filter, map, all, exists)

## Impact

**Better UX:**
- No more confusing suggestions like `app.replace.replace`
- Users understand: method references don't have methods, only calls do
- Encourages correct syntax: `method()` not just `method`

**More accurate type inference:**
- Distinguishes between references and calls
- Only infers return types for actual method invocations
- Reduces noise in completion suggestions

## Files Modified

- `analysis/cel_type_inference.go` - Added method reference detection
- `analysis/cel_type_inference_test.go` - Added tests for method references

## Future Enhancements

### 1. First-Class Functions?
If CEL/Kro ever supports first-class functions, this distinction becomes important:
```yaml
transform: ${items.map}  # map is a reference, can be passed around
result: ${items.map(transform)}  # applying the function
```

### 2. Method Signature Hover
When hovering over a method reference, show its signature:
```
replace(old: string, new: string) -> string
```

### 3. Completion After Reference
Maybe suggest parentheses after a method reference:
```yaml
${app.replace|
# Suggest: replace() (with parens)
```

## Summary

✅ **Fixed:** Method references no longer show method completions
✅ **Tested:** 3 new tests, all passing
✅ **Verified:** Method calls still work correctly

The LSP now correctly distinguishes between:
- `method` (reference) → unknown type, no completions
- `method()` (call) → return type, show appropriate methods

**User confusion: eliminated!** 🎯
