# Enhanced Hover Documentation

## Overview

The LSP now provides **rich hover tooltips** that show:

1. **K8s field documentation** - Inline docs for standard Kubernetes fields
2. **Schema field types** - Type info with constraints (min/max, enum, pattern, defaults)
3. **Concrete field indicators** - Shows if a field is defined in the template
4. **Available subfields** - Preview what fields are available under an object

## Features

### 1. K8s Field Documentation

Hover over standard Kubernetes fields to see their purpose and usage:

**Example:** Hover `labels` in `${deployment.metadata.labels}`
```
# labels

In: deployment (Deployment)

Description: Map of string keys and values for organizing and selecting objects. Labels can be used with selectors.

Type: map[string]string
```

**Covered fields:**
- `metadata.*` - name, namespace, labels, annotations, uid, generation, etc.
- `spec.*` - replicas, selector, template, strategy, etc. (Deployment)
- `spec.*` - type, ports, selector, clusterIP, etc. (Service)
- And 40+ more common k8s fields

### 2. Schema Field Types & Constraints

Hover over schema field references to see their type and validation:

**Example:** Schema with constraints:
```yaml
spec:
  schema:
    spec:
      replicas: integer | default=3 | min=1 | max=10
      environment: string | enum=[dev,staging,prod]
```

Hover `${schema.spec.replicas}` shows:
```
# replicas

Schema field

Type: integer (default: 3, min: 1, max: 10)
```

Hover `${schema.spec.environment}` shows:
```
# environment

Schema field

Type: string (enum: [dev,staging,prod])
```

**Supported SimpleSchema syntax:**
- `string`, `integer`, `boolean`, `number`, `float`
- `default=<value>` - Show default value
- `min=<n>`, `max=<n>` - Numeric constraints
- `minLength=<n>`, `maxLength=<n>` - String length
- `pattern=<regex>` - Validation pattern
- `enum=[val1,val2,...]` - Allowed values
- `[type]` - Arrays: `[string]` → `array[string]`
- `{keyType: valueType}` - Maps: `{string: integer}` → `map[string]integer`

### 3. Concrete Field Detection

When hovering over a field that's **actually defined in your template**, the LSP shows:

**Example:** Template has:
```yaml
metadata:
  labels:
    app: webapp
    env: prod
```

Hover `app` in `${deployment.metadata.labels.app}` shows:
```
# app

In: deployment (Deployment)

Defined in template: Yes

*Ctrl+Click to jump to definition in deployment template*

Type: string
```

This tells you:
- ✅ This field exists in your template (not just generic k8s schema)
- ✅ You can jump to its definition

### 4. Available Subfields Preview

When hovering over an **object** (not a leaf field), see what fields are available:

**Example:** Hover `metadata` in `${deployment.metadata.labels}`
```
# metadata

In: deployment (Deployment)

Description: Standard object metadata. Includes name, namespace, labels, and annotations.

Type: object

Available fields:
- name
- namespace
- labels
- annotations
- creationTimestamp
- ... and 6 more
```

This helps you discover what you can access without leaving the editor.

## Implementation

### Files

**New:**
- `analysis/k8s_docs.go` - Documentation strings for 50+ k8s fields

**Modified:**
- `services/hover.go` - Path-aware hover, k8s field docs, schema type display
- `analysis/symbol_table.go` - `parseSimpleSchemaType()` function

### Architecture

**Hover resolution order:**
1. Extract full path expression (e.g., `deployment.metadata.labels.app`)
2. Determine what segment the cursor is on (`app`)
3. Check if it's a schema field → show type + constraints
4. Check if it's a k8s field → show docs + type
5. Check if it's defined in template → add "Defined in template" indicator
6. Check if it has subfields → show available fields

### Type Parsing

The `parseSimpleSchemaType()` function handles Kro's SimpleSchema syntax:

```go
// Input: "integer | default=3 | min=1 | max=10"
// Output: "integer (default: 3, min: 1, max: 10)"

// Input: "[string]"
// Output: "array[string]"

// Input: "{string: integer}"
// Output: "map[string]integer"
```

This runs once at parse time when building the symbol table.

## Testing

Use `/tmp/test-hover-complete.yaml`:

### Schema Field Hover
```yaml
spec:
  schema:
    spec:
      replicas: integer | default=3 | min=1 | max=10
```
Hover `${schema.spec.replicas}` → see type with constraints

### K8s Field Hover
```yaml
metadata:
  labels:
    app: webapp
```
Hover `labels` in CEL expression → see k8s documentation

### Concrete Field Hover
```yaml
labels:
  app: webapp
  custom: myvalue
```
Hover `app` in `${deployment.metadata.labels.app}` → see "Defined in template"

### Subfield Discovery
Hover `metadata` in `${deployment.metadata.labels}` → see available subfields

## Future Enhancements

### 1. Type Inference from Values
```yaml
replicas: 3  # Infer: integer
name: "hello"  # Infer: string
enabled: true  # Infer: boolean
```

### 2. OpenAPI Schema Integration
Use OpenAPI/CRD schemas for accurate k8s type info instead of hardcoded docs.

### 3. Cross-Reference Tracking
Show "Used by N resources" when hovering schema fields or concrete keys.

### 4. CEL Function Signatures
Hover CEL functions to see signatures: `split(string, separator) -> []string`

### 5. Validation Preview
Show live validation: "Value '100' exceeds max: 10" in hover

## Impact

**Before:**
- No hover information for field paths
- Had to consult k8s docs separately
- No way to discover what fields are available
- No indication of whether fields exist in template

**After:**
- Rich, context-aware tooltips
- Inline k8s documentation
- Type info with validation constraints
- "Defined in template" indicators
- Subfield discovery without leaving editor

This makes authoring complex RGDs **much faster** — you don't need to context-switch to docs or re-read templates to remember what fields you defined! 🎸
