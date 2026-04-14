# Lifecycle Field Spec

## Overview
The `lifecycle` field gives users fine-grained control over resource deletion behavior. Instead of all-or-nothing deletion when a ResourceGroup is removed, individual resources can opt into custom lifecycle policies using expressive CEL expressions.

**Key principle:** Keep stateful resources (databases, volumes) while cleaning up everything else.

## Field Definition

```yaml
apiVersion: kro.run/v1alpha1
kind: ResourceGroup
metadata:
  name: my-app
spec:
  resources:
    - id: database
      template:
        apiVersion: v1
        kind: PersistentVolumeClaim
        metadata:
          name: my-app-data
        spec:
          accessModes: ["ReadWriteOnce"]
          resources:
            requests:
              storage: 10Gi
      lifecycle: "${policy().withRetain()}"  # <- Keeps PVC when RG is deleted
    
    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        # ... (no lifecycle field = default delete behavior)
```

## CEL Library: `policy`

The `policy()` function returns a lifecycle policy builder with a clean, chainable API.

### `policy()`

Returns an empty policy builder.

```javascript
policy()  // returns: {}
```

### `policy().withRetain()`

Marks a resource to be retained when its ResourceGroup is deleted.

**Returns:**
```json
{
  "deletePolicy": "retain"
}
```

**Use cases:**
- Persistent volumes that contain important data
- Shared infrastructure resources (load balancers, DNS records)
- Resources with external dependencies that need manual cleanup

### Chaining Design

The builder supports method chaining for composing multiple lifecycle aspects:

```javascript
policy()                            // base: {}
  .withRetain()                     // {deletePolicy: "retain"}
  .withFinalizer("cleanup-script")  // {deletePolicy: "retain", finalizer: "cleanup-script"}
```

**Safety guarantee:** Setting the same field twice is an error.

```yaml
# ❌ ERROR: deletePolicy already set to "retain"
lifecycle: "${policy().withRetain().withDelete()}"
```

This prevents ambiguity and catches configuration mistakes early. Each lifecycle aspect can only be set once per policy.

### Extensibility

The builder pattern allows future expansion without breaking existing usage:

- `policy().withFinalizer(name)` - Custom cleanup hooks
- `policy().withUpdatePolicy(strategy)` - Control update behavior  
- `policy().withDependsOn(resourceIds)` - Explicit ordering

Chaining works because each aspect targets a different field. Same-field conflicts always error.

## Examples

### Retain Production Database

```yaml
resources:
  - id: postgres-pvc
    template:
      apiVersion: v1
      kind: PersistentVolumeClaim
      metadata:
        name: "${instance.name}-postgres"
      spec:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: 50Gi
    lifecycle: "${policy().withRetain()}"
```

**Result:** When the ResourceGroup is deleted, the PVC remains in the cluster with all its data intact.

### Environment-Aware Retention

```yaml
resources:
  - id: redis-pvc
    template:
      apiVersion: v1
      kind: PersistentVolumeClaim
      # ...
    lifecycle: "${instance.spec.env == 'prod' ? policy().withRetain() : policy()}"
```

**Result:** Production instances keep their Redis data; dev/staging instances clean up fully.

### Complex Resource with Multiple Policies

```yaml
resources:
  - id: backup-storage
    template:
      apiVersion: v1
      kind: PersistentVolumeClaim
      # ...
    # Future: compose multiple lifecycle aspects
    lifecycle: "${policy().withRetain().withFinalizer('backup-to-s3')}"
```

### Direct Object Syntax (advanced)

```yaml
# This works but prefer policy().withRetain() for consistency
lifecycle: {deletePolicy: "retain"}

# Or just an empty policy (equivalent to omitting the field)
lifecycle: "${policy()}"  # same as {}
```

The object syntax is valid CEL but not the recommended style. The `policy()` builder provides better discoverability and forwards compatibility.

## Implementation Notes

This spec focuses on **UX design only**. Implementation is deferred.

### Evaluation Timing
- CEL expressions are evaluated once at resource creation
- The resulting policy object is stored and used during deletion

### Default Behavior
- Resources without a `lifecycle` field are deleted when the ResourceGroup is deleted
- Empty policy `lifecycle: "${policy()}"` or `lifecycle: {}` is equivalent to no field (default delete)

### CEL Environment
- Standard CEL context: `instance`, `status`, user-defined variables
- New function: `policy()` returns a lifecycle policy builder

## Validation Rules

### Expression Validation
```yaml
# ✅ Valid
lifecycle: "${policy().withRetain()}"
lifecycle: "${instance.isProd ? policy().withRetain() : policy()}"
lifecycle: {deletePolicy: "retain"}
lifecycle: "${policy()}"  # empty policy

# ❌ Invalid CEL syntax
lifecycle: "${policy().withRetain("  # syntax error

# ❌ Wrong type (must be object)
lifecycle: "${true}"
lifecycle: "retain"
```

### Policy Validation
```yaml
# ❌ Conflicting policies
lifecycle: "${policy().withRetain().withDelete()}"
# Error: deletePolicy cannot be set multiple times

# ❌ Unknown delete policy value
lifecycle: {deletePolicy: "maybe"}
# Error: deletePolicy must be "retain" or "delete"

# ❌ Invalid field
lifecycle: {deletePolicy: "retain", foo: "bar"}
# Error: unknown field "foo" in lifecycle policy
```

### Error Messages
Validation errors should be clear and actionable:

```
Error in resources[2].lifecycle: deletePolicy cannot be set multiple times
  Expression: ${policy().withRetain().withDelete()}
  First set: withRetain() at position 10
  Conflict: withDelete() at position 25
```

## Future Expansion

The builder pattern enables adding new lifecycle aspects without breaking changes:

- **Finalizers**: `policy().withFinalizer("custom-cleanup")` 
- **Update policies**: `policy().withUpdateStrategy("recreate")`
- **Dependency ordering**: `policy().withDependsOn(["db", "cache"])`
- **Cascade control**: `policy().withOrphanDependents()`

Each new method targets its own field, so existing code continues to work.
