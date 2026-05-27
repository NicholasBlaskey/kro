# CEL Type Validation Plan for LSP

## Current State

The LSP already has **basic CEL validation** in `services/cel_validation.go`:
- ✅ Compiles CEL expressions to check syntax
- ✅ Uses untyped environment (resources declared as `any`)
- ✅ Converts CEL errors to LSP diagnostics with position info
- ✅ Validates on document open/change

**Limitation:** No type checking against schemas, so expressions like `schema.spec.replicas.split()` (calling string method on int) won't be caught.

## What Kro Does (Can We Reuse This?)

### 1. Typed CEL Environment (`pkg/cel/environment.go`)

Kro creates **typed CEL environments** with full schema information:

```go
// From pkg/cel/environment.go:172
func TypedEnvironment(schemas map[string]*spec.Schema) (*cel.Env, error) {
    return DefaultEnvironment(WithTypedResources(schemas))
}
```

**How it works:**
- Converts OpenAPI schemas → CEL `DeclType` objects
- Registers them as CEL type providers
- Variables (resources) are declared with their actual types
- Enables **compile-time field type checking**

**Example:**
```go
schemas := map[string]*spec.Schema{
    "deployment": deploymentSchema,  // Full k8s Deployment schema
    "schema": instanceSpecSchema,     // User's instance spec
}
env, _ := krocel.TypedEnvironment(schemas)
```

### 2. Compilation & Type Checking (`pkg/graph/builder.go:1149`)

```go
func parseCheckAndCompile(env *cel.Env, expr *krocel.Expression) (*cel.Ast, error) {
    // Parse
    parsedAST, issues := env.Parse(expr.Original)
    if issues != nil && issues.Err() != nil {
        return nil, issues.Err()
    }

    // Type check
    checkedAST, issues := env.Check(parsedAST)
    if issues != nil && issues.Err() != nil {
        return nil, issues.Err()  // Type errors caught here!
    }

    // Compile
    program, err := env.Program(checkedAST)
    if err != nil {
        return nil, fmt.Errorf("compile: %w", err)
    }
    
    return checkedAST, nil
}
```

**Key insight:** `env.Check()` does type checking if the environment has schemas!

### 3. Return Type Validation

For specific contexts (like `includeWhen`, `readyWhen`), kro validates return types:

```go
// From pkg/graph/builder.go:1172
func validateConditionExpression(env *cel.Env, expr *krocel.Expression, conditionType, resourceID string) error {
    checkedAST, err := parseCheckAndCompile(env, expr)
    if err != nil {
        return fmt.Errorf("failed to type-check %s expression %q in resource %q: %w", conditionType, expr.Original, resourceID, err)
    }

    // Verify the expression returns bool or optional_type(bool)
    outputType := checkedAST.OutputType()
    if !krocel.IsBoolOrOptionalBool(outputType) {
        return fmt.Errorf(
            "%s expression %q must return bool, but returns %q",
            conditionType, expr.Original, outputType.String(),
        )
    }

    return nil
}
```

## Proposed Enhancement: Typed Validation for LSP

### Goal

Catch type errors in real-time as users type:
- ❌ `${schema.spec.replicas.split('-')}` — can't call string method on int
- ❌ `${deployment.metadata.labels + 5}` — can't add number to map
- ❌ `${schema.spec.name.contains(42)}` — wrong argument type
- ✅ `${schema.spec.name.contains('test')}` — correct!

### Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    LSP Server                           │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  1. Parse RGD YAML                                      │
│     ↓                                                   │
│  2. Extract Schemas                                     │
│     • Instance spec (from spec.schema)                  │
│     • Resource schemas (from API server / local cache) │
│     ↓                                                   │
│  3. Build Typed CEL Environment                         │
│     schemas := map[string]*spec.Schema{                 │
│         "schema": instanceSpecSchema,                   │
│         "deployment": deploymentSchema,                 │
│         "service": serviceSchema,                       │
│     }                                                   │
│     env, _ := krocel.TypedEnvironment(schemas)          │
│     ↓                                                   │
│  4. Validate CEL Expressions                            │
│     For each ${...} expression:                         │
│     • env.Parse() → AST                                 │
│     • env.Check() → Type errors caught here!            │
│     • Convert to LSP diagnostics                        │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

### Implementation Steps

#### Phase 1: Basic Typed Validation

**File:** `tools/lsp/server/services/cel_validation.go`

**Changes:**

1. **Add schema extraction to SymbolTable**
   ```go
   // In analysis/symbol_table.go
   type SymbolTable struct {
       Resources      map[string]*ResourceInfo
       Schema         *SchemaInfo
       ResourceSchemas map[string]*spec.Schema  // NEW: OpenAPI schemas
   }
   ```

2. **Enhance CELValidator to use typed environment**
   ```go
   // ValidateExpression with typed environment
   func (cv *CELValidator) ValidateExpression(expr string, ...) []protocol.Diagnostic {
       // OLD: env, _ := kcel.DefaultEnvironment(kcel.WithResourceIDs(resourceIDs))
       
       // NEW: Build typed environment
       schemas := cv.buildSchemaMap()
       env, err := kcel.TypedEnvironment(schemas)
       if err != nil {
           return []protocol.Diagnostic{...}
       }
       
       // Parse + Check (with types!)
       parsedAST, issues := env.Parse(expr)
       if issues != nil && issues.Err() != nil {
           return cv.celIssuesToDiagnostics(issues, ...)
       }
       
       checkedAST, issues := env.Check(parsedAST)  // Type checking!
       if issues != nil && issues.Err() != nil {
           return cv.celIssuesToDiagnostics(issues, ...)  // Type errors
       }
       
       return nil
   }
   ```

3. **Schema resolution**
   ```go
   func (cv *CELValidator) buildSchemaMap() map[string]*spec.Schema {
       schemas := make(map[string]*spec.Schema)
       
       // Add instance schema
       if cv.symbolTable.Schema != nil {
           schemas["schema"] = cv.convertToOpenAPISchema(cv.symbolTable.Schema)
       }
       
       // Add resource schemas (if available)
       for id, resourceSchema := range cv.symbolTable.ResourceSchemas {
           schemas[id] = resourceSchema
       }
       
       return schemas
   }
   ```

#### Phase 2: Schema Fetching

**Challenge:** LSP needs OpenAPI schemas for k8s resources (Deployment, Service, etc.)

**Options:**

1. **Embedded schemas** (simplest)
   - Bundle common k8s schemas in LSP binary
   - Fast, no network needed
   - Limited to pre-defined types
   
2. **Discovery API** (kro's approach)
   - Fetch from cluster API server
   - Requires credentials
   - May not work in offline mode
   
3. **Hybrid** (recommended)
   - Embedded for core types (Pod, Deployment, Service)
   - Discovery for custom resources
   - Fallback to `any` if unavailable

**Implementation:**

```go
// In analysis/k8s_schema.go (NEW)
type SchemaResolver interface {
    GetSchema(apiVersion, kind string) (*spec.Schema, error)
}

type EmbeddedSchemaResolver struct {
    schemas map[string]*spec.Schema
}

func NewEmbeddedSchemaResolver() *EmbeddedSchemaResolver {
    return &EmbeddedSchemaResolver{
        schemas: map[string]*spec.Schema{
            "v1/Pod": loadEmbeddedPodSchema(),
            "apps/v1/Deployment": loadEmbeddedDeploymentSchema(),
            "v1/Service": loadEmbeddedServiceSchema(),
            // ... more core types
        },
    }
}
```

#### Phase 3: Context-Aware Validation

Validate based on where the expression appears:

```go
type ExpressionContext int

const (
    ContextAny ExpressionContext = iota
    ContextCondition  // includeWhen, readyWhen → must return bool
    ContextTemplate   // template values → any type
    ContextStatus     // status expressions → specific type per field
)

func (cv *CELValidator) ValidateExpressionWithContext(
    expr string,
    ctx ExpressionContext,
    expectedType *cel.Type,
) []protocol.Diagnostic {
    // ... compile ...
    
    if ctx == ContextCondition {
        outputType := checkedAST.OutputType()
        if !kcel.IsBoolOrOptionalBool(outputType) {
            return []protocol.Diagnostic{{
                Message: fmt.Sprintf(
                    "Condition must return bool, got %s",
                    outputType.String(),
                ),
            }}
        }
    }
    
    if expectedType != nil && !typesCompatible(checkedAST.OutputType(), expectedType) {
        return []protocol.Diagnostic{{
            Message: fmt.Sprintf(
                "Expected type %s, got %s",
                expectedType.String(),
                checkedAST.OutputType().String(),
            ),
        }}
    }
    
    return nil
}
```

### Testing

```go
// tools/lsp/server/test/cel_validation_typed_test.go

func TestTypedCELValidation(t *testing.T) {
    tests := []struct {
        name        string
        rgd         string
        expr        string
        wantError   string
    }{
        {
            name: "string method on int field",
            rgd: `
spec:
  schema:
    spec:
      replicas: int
`,
            expr: "${schema.spec.replicas.split('-')}",
            wantError: "type 'int' does not support field selection",
        },
        {
            name: "valid string method",
            rgd: `
spec:
  schema:
    spec:
      name: string
`,
            expr: "${schema.spec.name.split('-')}",
            wantError: "",  // Valid
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ... test implementation
        })
    }
}
```

## Benefits

1. **Better Developer Experience**
   - Catch type errors immediately (red squiggles)
   - No need to apply RGD to see type errors
   - Faster feedback loop

2. **Fewer Runtime Errors**
   - Type mismatches caught at edit time
   - Reduces failed RGD applications
   - Less debugging time

3. **Better IDE Integration**
   - More accurate completions (already have this!)
   - Type-aware hover tooltips
   - Signature help for methods

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Schema resolution fails | Fallback to untyped validation (current behavior) |
| Performance with large schemas | Cache compiled environments, lazy load schemas |
| API server not available | Use embedded schemas for core types |
| Breaking existing behavior | Make typed validation opt-in initially |

## Rollout Plan

1. **Week 1:** Implement Phase 1 (basic typed validation with instance schema only)
2. **Week 2:** Add embedded core k8s schemas (Pod, Deployment, Service)
3. **Week 3:** Context-aware validation (bool for conditions)
4. **Week 4:** Polish UX, add tests, performance tuning

## Questions to Answer

1. **Where do we get k8s schemas?**
   - Option A: Bundle in binary (10-20 MB for core types)
   - Option B: Connect to API server (requires credentials)
   - Option C: User provides schema cache directory
   - **Recommendation:** Start with A, add B later

2. **Should validation be synchronous or async?**
   - Current: Synchronous on document change
   - With schemas: May be slower
   - **Recommendation:** Keep sync for now, optimize if slow

3. **How do we handle custom resources?**
   - Can't embed schemas for CRDs
   - **Recommendation:** Fall back to untyped for unknown types

## Code Reuse from Kro

✅ **Can reuse:**
- `pkg/cel/environment.go` - TypedEnvironment, WithTypedResources
- `pkg/cel/schemas.go` - SchemaDeclTypeWithMetadata, DeclTypeProvider
- `pkg/cel/types.go` - IsBoolOrOptionalBool, type utilities
- `pkg/graph/schema/conversion_cel_type.go` - Schema → CEL type conversion

❌ **Can't directly reuse:**
- `pkg/graph/builder.go` - Too coupled to RGD building
- Schema fetching logic - Need simpler approach for LSP

## Next Steps

1. Implement Phase 1 (typed validation with instance schema)
2. Add embedded k8s schemas for core types
3. Write comprehensive tests
4. Document for users

---

**Ready to implement?** The foundation is already in place - we just need to:
1. Extract schemas from RGD YAML
2. Use `krocel.TypedEnvironment()` instead of `DefaultEnvironment()`
3. The rest is already working!
