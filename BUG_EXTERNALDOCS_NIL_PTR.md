# Bug: Nil Pointer Dereference in ConvertJSONSchemaPropsToSpecSchema

## Location
`pkg/graph/schema/conversion_schema.go` lines 37-44

## Description
When converting CRD schemas that have ExternalDocs, there's a nil pointer dereference bug:

```go
var externalDocs *spec.ExternalDocumentation = nil
if props.ExternalDocs != nil {
    if props.ExternalDocs.URL != "" {
        externalDocs.URL = props.ExternalDocs.URL  // ← PANIC! externalDocs is nil!
    }
    if props.ExternalDocs.Description != "" {
        externalDocs.Description = props.ExternalDocs.Description  // ← PANIC!
    }
}
```

## Impact
- Affects offline validation when loading CRDs with ExternalDocs field
- Will panic if any CRD has external documentation links

## Fix
Should initialize the struct before setting fields:

```go
var externalDocs *spec.ExternalDocumentation
if props.ExternalDocs != nil {
    externalDocs = &spec.ExternalDocumentation{}
    if props.ExternalDocs.URL != "" {
        externalDocs.URL = props.ExternalDocs.URL
    }
    if props.ExternalDocs.Description != "" {
        externalDocs.Description = props.ExternalDocs.Description
    }
}
```

## Discovered By
Code review during offline validation implementation (PR #TBD)
