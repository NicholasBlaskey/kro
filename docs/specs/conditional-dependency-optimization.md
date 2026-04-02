# Conditional Dependency Optimization (Tree Shaking)

**Status**: Implemented
**Authors**: @nblaskey
**Created**: 2026-03-31
**Implemented**: 2026-04-01

## Problem Statement

Currently, kro's dependency graph builder conservatively includes all resource references found in CEL expressions as dependencies, even when those references appear in conditional branches that will never execute.

**Example:**
```yaml
spec:
  resources:
    - id: deployment
      template: |
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: app
        spec:
          template:
            spec:
              volumes:
                - name: data
                  ${schema.useDB ? db.spec.connection : pvc.spec.volumeName}
```

In this example:
- If `schema.useDB = true`, only `db` should be a dependency
- If `schema.useDB = false`, only `pvc` should be a dependency

**Current behavior**: Both `db` and `pvc` are marked as dependencies, requiring both resources to be defined and created, even though only one is actually used.

**Desired behavior**: Analyze conditional expressions where the condition depends only on `schema` (instance spec), evaluate the condition at build time, and only include the taken branch as a dependency.

## Goals

1. **Minimize unnecessary dependencies**: Remove dependencies from unreachable conditional branches
2. **Enable optional resources**: Allow RGD authors to conditionally include/exclude entire resources based on schema
3. **Maintain correctness**: Never remove a dependency that could actually be needed at runtime
4. **Preserve backward compatibility**: Existing RGDs continue to work unchanged

## Non-Goals

1. **Runtime optimization**: This is a build-time optimization only
2. **Complex control flow**: Only handle simple ternary conditionals initially (not nested comprehensions, etc.)
3. **Non-schema conditions**: Only optimize when condition depends purely on `schema`, not on other resources

## Design

### High-Level Approach

The optimization operates in two phases during graph building:

**Phase 1: Partial Evaluation (Build Time)**
- Detect conditional expressions in resource templates
- Identify conditions that depend only on `schema` (no resource references)
- Evaluate these conditions against the instance spec schema
- Determine which branches are reachable

**Phase 2: Dependency Pruning**
- For conditionals with schema-only conditions:
  - Extract dependencies from condition expression
  - Extract dependencies from each branch (consequent/alternative)
  - Only add dependencies from the taken branch to the DAG
- For conditionals with resource references in conditions:
  - Fall back to current behavior (include all dependencies)

### Detailed Design

#### Core Idea: Use CEL's Built-in Partial Evaluation

Instead of manually tracking conditionals, leverage CEL's `PartialEval` to simplify expressions with known schema values, then extract dependencies from the simplified result.

**Flow:**
```
Original: schema.useDB ? db.host : pvc.name
         ↓ (PartialEval with schema={useDB: true})
Simplified: db.host
         ↓ (Inspector.Inspect)
Dependencies: [db]  // pvc eliminated!
```

#### 1. Partial Evaluation Helper

Add `pkg/graph/partial.go`:

```go
// partialEvaluator simplifies CEL expressions with known schema values
type partialEvaluator struct {
    env *cel.Env          // CEL env with only 'schema' variable declared
    activation any        // Schema values to bind during evaluation
}

// newPartialEvaluator creates an evaluator with the instance spec schema
func newPartialEvaluator(schemaValues map[string]any) (*partialEvaluator, error) {
    // Create minimal CEL env with just 'schema' variable
    env, err := krocel.DefaultEnvironment(
        cel.Variable("schema", cel.DynType),
    )
    if err != nil {
        return nil, err
    }

    return &partialEvaluator{
        env: env,
        activation: map[string]any{"schema": schemaValues},
    }, nil
}

// simplify attempts to simplify the expression using partial evaluation
// Returns the simplified expression string, or the original if no simplification possible
func (pe *partialEvaluator) simplify(exprStr string) string {
    ast, issues := pe.env.Compile(exprStr)
    if issues.Err() != nil {
        // Can't compile (references resources, etc.) - return original
        return exprStr
    }

    // Attempt partial evaluation with schema bound
    residual, details := cel.PartialEval(ast, pe.activation)
    if details.Err() != nil {
        // Partial eval failed - return original
        return exprStr
    }

    // Check if expression was simplified
    // (residual AST has fewer nodes than original)
    if isSimplified(residual, ast) {
        // Convert residual AST back to string
        return astToString(residual)
    }

    return exprStr
}
```

#### 2. Modify Dependency Extraction

Update `extractDependencies` in `pkg/graph/builder.go`:

```go
// extractDependencies now accepts optional partial evaluator
func extractDependencies(
    inspector *ast.Inspector,
    expr *krocel.Expression,
    iteratorVars []string,
    partialEval *partialEvaluator,  // NEW: optional optimizer
) (
    resourceDeps []string,
    iteratorRefs []string,
    err error,
) {
    // Try to simplify expression with schema values
    exprToInspect := expr.Original
    if partialEval != nil {
        exprToInspect = partialEval.simplify(expr.Original)
    }

    // Inspect the simplified expression
    inspectionResult, err := inspector.Inspect(exprToInspect)
    if err != nil {
        return nil, nil, fmt.Errorf("failed to inspect expression: %w", err)
    }

    // Rest of the function unchanged - extract dependencies from inspection result
    // ...
}
```

#### 3. Integration Points

**In `NewResourceGraphDefinition`:**
1. After building instance spec schema, extract schema default values
2. Create `partialEvaluator` with schema defaults
3. Pass evaluator to `buildDependencyGraph`

**In `buildDependencyGraph`:**
1. Pass `partialEvaluator` to `extractTemplateDependencies`
2. Pass it to `extractForEachDependencies`
3. Pass it to `extractConditionDependencies` (for includeWhen)

**Schema Value Extraction:**
```go
// Extract default values from instance spec schema for partial evaluation
func extractSchemaDefaults(instanceSpecSchema *extv1.JSONSchemaProps) map[string]any {
    defaults := make(map[string]any)
    for propName, propSchema := range instanceSpecSchema.Properties {
        if propSchema.Default != nil {
            defaults[propName] = propSchema.Default.Object
        }
        // Could also handle required fields without defaults as errors
    }
    return defaults
}
```

### Example Transformation

**Input RGD:**
```yaml
spec:
  schema:
    spec:
      useDB: boolean
  resources:
    - id: db
      template: |
        apiVersion: v1
        kind: Service
        metadata:
          name: db
    - id: pvc
      template: |
        apiVersion: v1
        kind: PersistentVolumeClaim
        metadata:
          name: storage
    - id: app
      template: |
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: app
        spec:
          template:
            spec:
              volumes:
                - name: data
                  ${schema.useDB ? db.spec.clusterIP : pvc.spec.volumeName}
```

**Current DAG:**
```
app -> [db, pvc]
db -> []
pvc -> []
```

**After optimization (when useDB is defined in schema):**
```
# If schema default is useDB=true:
app -> [db]
db -> []
pvc -> [] (unreferenced, could be omitted)

# If schema default is useDB=false:
app -> [pvc]
db -> [] (unreferenced, could be omitted)
pvc -> []
```

## Implementation

Literally ~20 lines of code:

```go
// In extractDependencies, before inspector.Inspect():
if schemaValues != nil {
    ast, _ := schemaEnv.Compile(expr.Original)
    residual, _ := cel.PartialEval(ast, map[string]any{"schema": schemaValues})
    expr.Original = astToString(residual)  // or however we get string back
}

// Then proceed with existing inspector.Inspect(expr.Original)
```

That's it. CEL does all the heavy lifting.

## Tasks

- [ ] Add PartialEval call before inspector.Inspect in extractDependencies
- [ ] Write one test showing `schema.x ? a : b` only depends on `a` when `x=true`

## Edge Cases & Limitations

### Supported

✅ **Simple ternary conditionals**
```cel
schema.useDB ? db.host : pvc.name
```

✅ **Schema-only conditions with logical operators**
```cel
schema.useDB && schema.production ? db.replica : db.standalone
```

✅ **Nested ternaries**
```cel
schema.env == "prod" ? (schema.ha ? db.cluster : db.single) : db.dev
```

### Not Supported (Fall back to conservative)

❌ **Conditions referencing resources**
```cel
db.ready ? db.endpoint : fallback.endpoint
// Cannot evaluate at build time - both db and fallback are dependencies
```

❌ **Conditions in forEach**
```cel
forEach: [schema.useDB ? databases : caches]
// forEach expressions are evaluated before template resolution
```

❌ **Dynamic comprehensions**
```cel
[x for x in resources if x.enabled].map(r, r.name)
// Too complex for build-time analysis
```

### Validation Rules

1. **Schema must have defaults or be fully specified**: If condition references `schema.useDB`, the schema must either:
   - Have a default value for `useDB`
   - Be marked as required

2. **Type safety**: Condition must type-check to `bool` or `optional<bool>`

3. **No side effects**: Optimization assumes condition evaluation is pure

## Testing Strategy

### Unit Tests

1. **AST Inspector**: Test conditional detection across all ternary patterns
2. **Partial Evaluator**: Test schema-only evaluation with various types
3. **Dependency Extraction**: Test pruning logic for all branch combinations

### Integration Tests

```go
func TestConditionalDependencyOptimization(t *testing.T) {
    tests := []struct {
        name           string
        rgd            *v1alpha1.ResourceGraphDefinition
        schemaDefaults map[string]any
        expectedDeps   map[string][]string
    }{
        {
            name: "simple ternary with useDB=true",
            // RGD with schema.useDB ? db : pvc
            schemaDefaults: map[string]any{"useDB": true},
            expectedDeps: map[string][]string{
                "app": {"db"},  // pvc pruned
            },
        },
        {
            name: "nested ternary",
            // schema.env == "prod" ? (schema.ha ? db.cluster : db.single) : db.dev
            schemaDefaults: map[string]any{"env": "prod", "ha": true},
            expectedDeps: map[string][]string{
                "app": {"db.cluster"},  // db.single and db.dev pruned
            },
        },
        {
            name: "resource condition falls back",
            // db.ready ? db : fallback
            expectedDeps: map[string][]string{
                "app": {"db", "fallback"},  // both kept
            },
        },
    }
    // ...
}
```

### End-to-End Tests

Create example RGDs demonstrating real-world use cases:
1. Optional database vs. PVC for storage
2. Environment-specific resource selection (dev/staging/prod)
3. Feature flags controlling resource inclusion

## Metrics & Observability

Track optimization effectiveness:

```go
// Metrics
graph_conditional_expressions_total        // Total conditionals found
graph_conditional_expressions_optimized    // Successfully optimized
graph_dependencies_pruned_total            // Dependencies removed
graph_partial_evaluation_errors_total      // Fallback to conservative
```

## Documentation

### User Guide

Add section to RGD authoring guide:

```markdown
## Conditional Dependencies

You can use ternary expressions to conditionally reference resources based on your schema:

```yaml
spec:
  schema:
    spec:
      storageType: string | default="pvc"  # Must have default or be required
  resources:
    - id: database
      # ...
    - id: volume
      # ...
    - id: app
      template: |
        # ...
        spec:
          volumes:
            - name: data
              ${schema.storageType == "database" ? database.connectionString : volume.mountPath}
```

**Requirements:**
- Condition must only reference `schema` fields
- Referenced schema fields must have defaults or be required
- Ternary operator syntax: `condition ? consequent : alternative`

**Benefits:**
- Reduced resource creation (unused resources aren't dependencies)
- Clearer resource graph visualization
- Faster reconciliation (fewer watches)
```

## Security Considerations

None. This is a build-time optimization that doesn't change runtime behavior or introduce new attack surfaces.

## Alternatives Considered

### 1. Runtime Dependency Resolution

**Approach**: Keep all dependencies at build time, dynamically skip at runtime.

**Pros**:
- Simpler implementation
- More flexible (can handle resource-based conditions)

**Cons**:
- Still creates watches and tracks unnecessary resources
- Doesn't reduce resource definitions in RGD
- No visualization benefits

**Decision**: Build-time optimization is more valuable for resource reduction.

### 2. Explicit Dependency Annotations

**Approach**: Let users manually mark conditional dependencies:

```yaml
resources:
  - id: app
    dependencies:
      conditional:
        - if: schema.useDB
          then: [db]
          else: [pvc]
```

**Pros**:
- Explicit and clear
- No complex analysis needed

**Cons**:
- Verbose and error-prone
- Duplicates information already in CEL expressions
- Harder to maintain consistency

**Decision**: Automatic detection is more ergonomic.

## Future Work

1. **Constant folding**: Optimize `schema.count * 2` to avoid runtime multiplication
2. **Dead code elimination**: Remove resources with no dependents
3. **includeWhen optimization**: Apply partial evaluation to includeWhen conditions
4. **Whole-program optimization**: Cross-RGD dependency analysis

## Open Questions

1. Should we emit warnings when optimization isn't possible?
2. How to handle schema evolution (adding/removing/changing defaults)?
3. Should optimized vs. conservative dependencies be distinguishable in the DAG?

## Implementation Summary

**Date**: 2026-04-01
**Effort**: ~2 hours
**Lines of code**: ~100 lines in `pkg/graph/builder.go`

### What was implemented

Successfully implemented conditional dependency optimization using CEL's built-in partial evaluation. The implementation is simpler than originally planned because we leverage CEL's `PartialEval` and `ResidualAst` APIs.

**Core components**:
1. `partialEvaluator` struct - wraps CEL env and activation with schema values
2. `extractSchemaDefaults()` - extracts default values from instance spec schema
3. `simplifyExpression()` - runs CEL PartialEval iteratively until fixed point
4. Threaded through all dependency extraction functions

**Test coverage**:
- Ternary conditionals (both branches)
- Nested ternary conditionals
- Non-schema conditionals (fallback to conservative)
- Non-conditional expressions

### Results

**Before**:
```
schema.useDB ? db.host : pvc.name
Dependencies: [db, pvc]
```

**After**:
```
schema.useDB=true → Simplified: db.host → Dependencies: [db]
schema.useDB=false → Simplified: pvc.name → Dependencies: [pvc]
```

**Complex nested example**:
```
schema.useDB ? (schema.isReplicated ? dbPrimary : dbSingle) : pvc
With useDB=true, isReplicated=false → dbSingle
Dependencies: [dbSingle]  (eliminated: dbPrimary, pvc)
```

### Performance

Negligible impact - partial evaluation happens once at RGD build time, not during runtime reconciliation.

### Backward compatibility

Fully backward compatible:
- If schema has no defaults, no optimization occurs
- If partial eval fails, falls back to original expression
- All existing tests pass

## References

- [CEL Specification](https://github.com/google/cel-spec)
- [kro Graph Builder](../../pkg/graph/builder.go)
- [kro AST Inspector](../../pkg/cel/ast/inspector.go)
