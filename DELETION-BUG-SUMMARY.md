# Deletion Bug Summary: CEL Errors in Identity Paths Block Instance Deletion

## Problem
Instances with CEL runtime errors in identity paths (like `metadata.name`) cannot be deleted. The instance gets stuck with:
- `deletionTimestamp` set
- `state: DELETING`
- Finalizer never removed
- Controller continuously retrying the same CEL error

## Root Cause
During full instance deletion, `DeleteTargets()` calls `GetDesiredIdentity()` to compute what resources should exist, then evaluates CEL expressions in identity paths. If these expressions have runtime errors (division by zero, etc.), the deletion fails.

**The key insight:** During full instance deletion, we don't actually need to know the "desired" identities - we want to delete ALL observed resources regardless of CEL errors.

## Test Cases

### Case 1: Single Resource
- File: `test-cel-error-identity.yaml`
- CEL error in `metadata.name`: `${"test-" + string(schema.spec.count / 0)}`
- Result: Instance stuck, cannot delete

**Finding:** For single resources, `DeleteTargets()` computes `desired` identities but **completely ignores them** and just returns `n.observed`. The `GetDesiredIdentity()` call is entirely wasteful.

### Case 2: forEach Collection
- File: `test-cel-error-foreach.yaml`
- CEL error in forEach collection `metadata.name`: `${"test-" + string(schema.spec.counts[idx] / 0)}`
- Result: Instance stuck, cannot delete

**Finding:** For collections, `DeleteTargets()` computes `orderedIntersection(n.observed, desired)`. 

**IMPORTANT:** `DeleteTargets()` is ONLY called during instance deletion, NOT during normal reconciliation. Collection shrinkage during normal reconciliation is handled by the **applyset prune mechanism** (see `pruneOrphans()` in resources.go:214), which works at the applyset level and doesn't involve `DeleteTargets()`.

So why does `DeleteTargets()` compute the intersection during deletion? The intersection ensures we only delete resources that are "legitimate" collection members (match the desired identities), not random resources that happen to have the same labels. However, if the desired identities cannot be computed (CEL error), we cannot determine what to delete, and deletion gets stuck.

The question is: **during full instance deletion, do we care about filtering to "legitimate" members, or should we just delete everything with the kro labels?**

## Code Path Analysis

Full trace during deletion:
1. `reconcileDeletion()` → `planNodesForDeletion()` → `deleteTarget()` 
2. `deleteTarget()` calls `node.DeleteTargets()` (pkg/controller/instance/deletion.go:172)
3. `DeleteTargets()` calls `GetDesiredIdentity()` (pkg/runtime/node.go:296)
4. For collections: `GetDesiredIdentity()` → `resolve(resolveIdentity)` → `hardResolveCollection()`
5. `hardResolveCollection()` iterates through forEach items, evaluating identity expressions (node_resolve.go:106)
6. Expression evaluation fails with "division by zero" (NOT a pending error)
7. Error returns at line 111: `return nil, fmt.Errorf("collection iteration eval %q: %w", exprStr, err)`
8. Deletion fails, controller retries indefinitely

## Dependency Complication

**`GetDesiredIdentity()` is also used for dependencies during deletion!**

In `planNodesForDeletion()` (deletion.go:84), `GetDesiredIdentity()` is called for ALL nodes:
- **External nodes**: To locate and observe them, providing data for dependent managed nodes (line 98-106)
- **Managed nodes**: To determine what to observe/delete

Example:
```yaml
resources:
  - id: external-db
    type: external
    template:
      metadata:
        name: ${schema.spec.dbName}  # CEL expression in identity
  
  - id: migration-job
    template:
      metadata:
        name: ${external-db.metadata.name}-migration  # depends on external-db
```

During deletion:
1. Resolve `external-db` identity → observe it → downstream nodes can use its data
2. Resolve `migration-job` identity (uses external-db data from step 1)

**If `external-db` has a CEL error, the entire deletion chain breaks!**

This means we cannot simply skip `GetDesiredIdentity()` for all nodes - external nodes need it for dependency resolution.

## Proposed Fix

Check if the instance is being deleted at the call site and skip identity resolution **only for managed nodes**:

**Wait, this won't work!** By the time we reach `deleteTarget()`, we've already called `GetDesiredIdentity()` in `planNodesForDeletion()` (line 84) and failed with the CEL error.

The fix needs to be earlier, in `planNodesForDeletion()`:

```go
// pkg/controller/instance/deletion.go:80-94
isExternal := nodeMeta.Type == graph.NodeTypeExternal || nodeMeta.Type == graph.NodeTypeExternalCollection

// Resolve identity without readiness gating. External nodes use this to
// locate the resource for observation; managed nodes use it as the deletion target.
var desired []*unstructured.Unstructured
var err error

if isExternal {
    // External nodes: must resolve identity for dependency observation
    desired, err = node.GetDesiredIdentity()
    if err != nil {
        if runtime.IsDataPending(err) {
            state.SetDeleted()
            continue
        }
        state.SetError(err)
        return nil, err
    }
    // Observe external node for downstream dependencies
    if len(desired) > 0 {
        if err := c.observeExternal(rcx, node, desired[0]); err != nil {
            state.SetError(err)
            return nil, err
        }
    }
    state.SetSkipped()
    continue
} else {
    // Managed nodes during full deletion: skip identity resolution, use labels to observe
    // Just proceed to observe step below (GET/LIST)
}

// Continue with observation logic...
```

For managed nodes, skip `GetDesiredIdentity()` and rely on:
- Single resources: Still need identity to know what name to GET... **problem!**
- Collections: LIST by labels, then `DeleteTargets()` just returns all observed

**Actually, this is harder than I thought.** For single resources, we need the identity to know what to GET. But if the identity has a CEL error, we can't GET it.

## The Real Discovery: Dead Code for Collections!

For collections in `planNodesForDeletion()`:
```go
desired, err := node.GetDesiredIdentity()  // Line 84 - can fail with CEL error!
if len(desired) == 0 {                      // Line 109 - only usage
    state.SetDeleted()
    continue
}
items, err := c.listCollectionItems(...)    // Line 127 - LISTs by LABELS, doesn't use desired!
node.SetObserved(items)
```

**`GetDesiredIdentity()` result is barely used!** Only for the `len == 0` check.

The `listCollectionItems()` function (resources_collection.go:119) LISTs by labels:
```go
selector := fmt.Sprintf("%s=%s,%s=%s",
    metadata.InstanceIDLabel, instanceUID,  // kro.run/instance-id
    metadata.NodeIDLabel, nodeID)           // kro.run/node-id
```

It doesn't use `desired` at all - just finds resources by labels!

**Clean Fix for Collections:**

During full instance deletion, if `GetDesiredIdentity()` fails with a non-pending error:
1. Assume the collection is non-empty (skip the `len == 0` check)
2. Proceed to LIST by labels (which already works without identity resolution)
3. In `DeleteTargets()`, return all observed instead of computing intersection

```go
// pkg/controller/instance/deletion.go:84-111
isFullDeletion := !rcx.Instance.GetDeletionTimestamp().IsZero()

desired, err := node.GetDesiredIdentity()
if err != nil {
    if !isExternal && runtime.IsDataPending(err) {
        state.SetDeleted()
        continue
    }
    
    // During full deletion, CEL errors are tolerable for managed nodes
    // We'll LIST by labels and delete everything found
    if isFullDeletion && !isExternal {
        // Skip identity resolution, assume non-empty, proceed to observation
        desired = nil  // Signal to skip len check
    } else {
        state.SetError(err)
        return nil, err
    }
}

if desired != nil && len(desired) == 0 {
    state.SetDeleted()
    continue
}
```

Then in `DeleteTargets()`:
```go
func (c *Controller) deleteTarget(...) error {
    // Check if this is full deletion before calling DeleteTargets
    if !rcx.Instance.GetDeletionTimestamp().IsZero() {
        // Full deletion: just delete everything observed, no intersection needed
        targets = node.GetObserved()
    } else {
        // Not full deletion (this shouldn't happen since DeleteTargets is only called during deletion)
        targets, err = node.DeleteTargets()
        if err != nil {
            state.SetError(err)
            return err
        }
    }
    // ... rest of deletion
}
```

This handles:
- ✅ Collections with CEL errors can be deleted (LIST by labels still works)
- ✅ Single resources still need identity for GET (separate fix needed)
- ✅ External nodes still resolve identity for dependencies
- ✅ No intersection computation during full deletion (avoids second CEL evaluation)

## Alternative: Add Node Method

```go
// pkg/runtime/node.go
func (n *Node) GetObserved() []*unstructured.Unstructured {
    return n.observed
}
```

Then expose this and call it from deletion.go. This is cleaner API-wise but requires exposing observed state.

## Testing
- `test-cel-error-identity.yaml` + `test-cel-error-instance.yaml` - single resource case
- `test-cel-error-foreach.yaml` + `test-cel-error-foreach-instance.yaml` - collection case
- Both instances currently stuck in DELETING state with the bug
- After fix, both should delete successfully despite CEL errors

## Controller Command
```bash
go run ./cmd/controller/main.go ./cmd/controller/pprof.go
```
