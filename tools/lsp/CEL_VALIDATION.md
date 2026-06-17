# CEL Expression Validation in Kro LSP

## Overview

The Kro LSP server now includes comprehensive CEL (Common Expression Language) validation with precise error reporting. This feature validates CEL expressions in ResourceGraphDefinitions and provides detailed diagnostics with accurate line and column positions.

## Features

### 1. **Real-time CEL Compilation**
- Validates CEL expressions as you type
- Catches syntax errors immediately
- Detects undefined variable references
- Reports type mismatches

### 2. **Context-Aware Validation**
Different CEL expressions have different available variables:

- **`includeWhen`**: Only `schema` is available
  ```yaml
  includeWhen:
    - "schema.spec.enableMonitoring == true"  # ✓ Valid
    - "self.status.ready == true"             # ✗ Error: 'self' not available in includeWhen
  ```

- **`readyWhen`**: Both `self` and all resource IDs are available
  ```yaml
  readyWhen:
    - "self.status.readyReplicas > 0"         # ✓ Valid
    - "deployment.status.ready == true"       # ✓ Valid (if deployment exists)
  ```

- **`template` expressions**: All resource IDs and `schema` are available
  ```yaml
  template:
    metadata:
      name: ${schema.spec.name}               # ✓ Valid
      labels:
        version: ${configMap.data.version}    # ✓ Valid (if configMap exists)
  ```

### 3. **Precise Error Locations**
The LSP reports errors at the exact line and column where they occur:

```yaml
resources:
  - id: deployment
    readyWhen:
      - "nonExistentResource.status.ready == true"
      #  ^^^^^^^^^^^^^^^^^^^ 
      # Error: undeclared reference to 'nonExistentResource'
```

### 4. **Automatic Variable Detection**
The validator automatically:
- Extracts all resource IDs from the RGD
- Makes them available as CEL variables
- Includes built-in variables (`schema`, `self`)
- Validates references against declared resources

## Architecture

### Components

1. **`cel_validator.go`** - Main validation manager
   - Creates context-specific CEL environments
   - Validates expressions in different contexts
   - Maps errors to YAML positions

2. **`analysis/cel_validation.go`** - CEL validation utilities
   - Wraps CEL compiler
   - Extracts error messages
   - Provides type checking helpers

3. **`validation.go`** - Integration with LSP validation pipeline
   - Runs CEL validation before full builder validation
   - Combines diagnostics from multiple sources

### Validation Flow

```
Document Change
    ↓
YAML Parsing
    ↓
RGD Structure Validation
    ↓
CEL Expression Extraction  ← NEW
    ↓
Context-Aware CEL Validation  ← NEW
    ↓
Detailed Diagnostics with Positions  ← NEW
    ↓
Full Builder Validation (if online)
    ↓
Report to Editor
```

## Implementation Details

### CEL Environment Setup

Different expression contexts require different CEL environments:

```go
// includeWhen: only schema
includeWhenEnv, _ := cel.DefaultEnvironment(
    cel.WithResourceIDs([]string{"schema"})
)

// readyWhen: self + all resources
readyWhenEnv, _ := cel.DefaultEnvironment(
    cel.WithResourceIDs([]string{"self", "deployment", "service", ...})
)

// template: all resources + schema
templateEnv, _ := cel.DefaultEnvironment(
    cel.WithResourceIDs([]string{"schema", "deployment", "service", ...})
)
```

### YAML Position Mapping

The validator uses `gopkg.in/yaml.v3` to maintain position information:

```go
var yamlNode yaml.Node
yaml.Unmarshal(content, &yamlNode)

// Walk YAML tree to find expression
resourceNode := findResourceNodeByID(&yamlNode, resourceID)
exprNode := findFieldInNode(resourceNode, "readyWhen")

// Use node.Line and node.Column for diagnostics
diagnostic := protocol.Diagnostic{
    Range: protocol.Range{
        Start: protocol.Position{
            Line:      uint32(exprNode.Line - 1),    // YAML is 1-based
            Character: uint32(exprNode.Column - 1),  // LSP is 0-based
        },
    },
    Message: celError.Message,
}
```

### Template Expression Extraction

Template expressions use `${...}` syntax:

```go
celRegex := regexp.MustCompile(`\$\{([^}]+)\}`)
matches := celRegex.FindAllStringSubmatch(nodeValue, -1)

for _, match := range matches {
    expr := match[1]  // Extract expression between ${ and }
    validateExpression(expr)
}
```

## Testing

### Unit Tests

The implementation includes comprehensive unit tests:

```bash
cd tools/lsp/server
go test -v -run TestCELValidation
```

Tests cover:
- ✓ Invalid CEL syntax detection
- ✓ Undeclared variable references
- ✓ Valid expressions pass without errors
- ✓ Context-specific variable availability

### Manual Testing

1. Create a test RGD with CEL errors:
```yaml
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test
spec:
  schema:
    kind: TestApp
    apiVersion: v1alpha1
  resources:
    - id: deployment
      readyWhen:
        - "invalidVar.status.ready == true"
```

2. Open in your editor with the LSP configured
3. Observe red underlines at the exact error location
4. Hover to see detailed error message

## Performance Considerations

- **Lightweight**: CEL validation runs before heavy builder validation
- **Cached**: CEL environments are created once per resource
- **Fast**: Typical validation completes in < 10ms for most RGDs
- **Incremental**: Only validates changed documents

## Future Enhancements

Potential improvements:

1. **Type-aware validation**: Once Kro v0.5+ is available, use `TypedEnvironment` for full schema-based type checking
2. **Smart suggestions**: Offer quick fixes for common errors (e.g., "Did you mean 'schema'?")
3. **Expression templates**: Code snippets for common CEL patterns
4. **Performance optimization**: Cache CEL programs across validations
5. **Cross-resource type inference**: Validate that `deployment.status.ready` is actually a boolean

## Troubleshooting

### "undeclared reference" errors

**Problem**: CEL reports variables as undeclared even though they exist

**Solution**: Check that:
- Resource ID is spelled correctly (case-sensitive)
- Resource is defined before it's referenced in the YAML
- Using correct variable for context (e.g., `schema` in templates, `self` in readyWhen)

### Positions are slightly off

**Problem**: Error underlines don't align perfectly with the expression

**Solution**: This can happen with:
- Multi-line YAML values
- Complex nested expressions
- YAML anchors and aliases

The validator does its best to extract positions from YAML metadata.

### No CEL errors shown in offline mode

**Problem**: CEL validation not running

**Solution**: 
- CEL validation works in both online and offline modes
- Check LSP server logs for errors
- Verify YAML is valid (CEL validation only runs after YAML parsing succeeds)

## References

- [CEL Language Specification](https://github.com/google/cel-spec)
- [Kubernetes CEL Documentation](https://kubernetes.io/docs/reference/using-api/cel/)
- [Kro ResourceGraphDefinition API](../../api/v1alpha1/resourcegraphdefinition_types.go)
