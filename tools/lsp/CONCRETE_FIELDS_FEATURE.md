# Concrete Fields Feature

## Overview

The LSP now extracts **concrete field structure** from resource templates and uses it for richer autocompletion and go-to-definition.

Previously, completions were limited to:
- Generic k8s schema (e.g., `metadata` has `name`, `namespace`, `labels`, but not the specific keys you defined)
- Schema fields from the RGD

Now, the LSP analyzes your actual template structure and offers completions based on **what you actually wrote**.

## Example

Given this template:

```yaml
resources:
  - id: deployment
    template:
      metadata:
        labels:
          app: deployment
          dns: ${schema.spec.fullDNSName}
          partials: "..."
          first_part: "..."
```

When you type `${deployment.metadata.labels.`, you now get completions for:
- ✅ `app`
- ✅ `dns`
- ✅ `partials`
- ✅ `first_part`

Instead of just knowing "labels is a map[string]string", the LSP knows the **exact keys** you defined.

## Features

### 1. **Rich Autocompletion**
- Type `${deployment.metadata.labels.` → see all keys you defined (`app`, `dns`, etc.)
- Type `${deployment.spec.selector.matchLabels.` → see keys from that nested map
- Template-based completions are **prioritized** over generic k8s schema

### 2. **Go-to-Definition for Map Keys**
- Ctrl+click on `app` in `${deployment.metadata.labels.app}`
- Jumps to `app: deployment` line in the template

### 3. **Works for Any Nesting Level**
- `metadata.labels.app` ✅
- `spec.selector.matchLabels.app` ✅
- `spec.template.spec.containers[0].env` (once we add array support) 🔜

## Implementation

### Data Structure
`ResourceSymbol` now has:
```go
ConcreteFields map[string][]string
```

Example:
```go
symbol.ConcreteFields = {
  "metadata.labels": ["app", "dns", "partials", "first_part"],
  "spec.selector.matchLabels": ["app"],
  "spec.template.metadata.labels": ["app", "version"],
}
```

### Extraction
When building the symbol table, we walk the template YAML and extract all map keys:

```go
func extractConcreteFields(obj interface{}, path string, concreteFields map[string][]string)
```

This runs once at parse time, so no performance hit during completion/definition requests.

### Completion Priority
1. **Concrete fields from template** (SortText: `0_fieldName`) ← highest priority
2. K8s schema fields (SortText: default) ← fallback
3. Generic standard fields (metadata, spec, status) ← lowest priority

### Go-to-Definition
Already works! The positionMap already tracks every YAML key path like:
```
spec.resources[0].template.metadata.labels.app#key
```

So when you Ctrl+click on `app` in a CEL expression, we build the path and look it up.

## Testing

Use `/tmp/test-concrete-fields.yaml`:

```yaml
resources:
  - id: deployment
    template:
      metadata:
        labels:
          app: deployment
          dns: ${schema.spec.fullDNSName}
  
  - id: service
    template:
      metadata:
        labels:
          # Type ${deployment.metadata.labels.| here
          app: ${deployment.metadata.labels.app}
```

**Try:**
1. Type `${deployment.metadata.labels.` → see `app`, `dns` in completions
2. Ctrl+click `app` in `${deployment.metadata.labels.app}` → jumps to `app: deployment`
3. Type `${deployment.spec.selector.matchLabels.` → see keys from that map

## Future Enhancements

### 1. **Array Element Fields**
Currently skipped. Could extract fields from array elements:
```yaml
containers:
  - name: app
    image: nginx
```
→ `ConcreteFields["spec.template.spec.containers[0]"] = ["name", "image"]`

### 2. **Hover Tooltips**
Show "Defined in deployment template at line X" when hovering concrete fields

### 3. **Type Inference from Values**
If we see `replicas: 3`, we know it's an integer. Could offer type-aware completions.

### 4. **Cross-Resource Field Tracking**
Track when resource A references resource B's fields, show in dependency graph.

## Files Changed

- `analysis/symbol_table.go` - Added `ConcreteFields`, `extractConcreteFields()`
- `services/completion.go` - Check concrete fields before k8s schema
- `services/definition.go` - Already works via positionMap

## Impact

**Before:**
- Autocompletion limited to generic k8s fields
- No way to discover what keys you actually defined in maps

**After:**
- Rich, template-aware completions
- Go-to-definition works for any field you defined
- Much faster template authoring

This is a **huge UX win** for complex templates with deeply nested structures!
