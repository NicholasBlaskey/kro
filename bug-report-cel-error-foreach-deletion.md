# Bug Report: Instance with forEach Collection Stuck During Deletion with CEL Error in Identity Path

## Summary
When an instance has a CEL expression error in a forEach collection's identity path (like `metadata.name`), the instance becomes stuck during deletion and cannot be removed. This is the same root cause as the single resource case, but occurring within collection iteration.

## Reproduction Steps

1. Create an RGD with a forEach collection that has a CEL error in the identity path:
   ```yaml
   # test-cel-error-foreach.yaml
   apiVersion: kro.run/v1alpha1
   kind: ResourceGraphDefinition
   metadata:
     name: cel-error-foreach-test
   spec:
     schema:
       apiVersion: v1alpha1
       kind: CelErrorForEachTest
       spec:
         counts: '[]integer'
   
     resources:
       - id: configmaps
         forEach:
           - idx: ${lists.range(size(schema.spec.counts))}
         template:
           apiVersion: v1
           kind: ConfigMap
           metadata:
             # CEL error in identity path within forEach - divide by zero at runtime
             name: ${"test-" + string(schema.spec.counts[idx] / 0)}
           data:
             index: ${string(idx)}
   ```

2. Apply the RGD:
   ```bash
   kubectl apply -f test-cel-error-foreach.yaml
   ```

3. Create an instance:
   ```yaml
   # test-cel-error-foreach-instance.yaml
   apiVersion: kro.run/v1alpha1
   kind: CelErrorForEachTest
   metadata:
     name: test-foreach-instance
     namespace: default
   spec:
     counts: [5, 10, 15]
   ```

4. Apply the instance:
   ```bash
   kubectl apply -f test-cel-error-foreach-instance.yaml
   ```

5. Observe the instance fails to reconcile with CEL error:
   ```
   ERROR: collection iteration eval "\"test-\" + string(schema.spec.counts[idx] / 0)": eval "\"test-\" + string(schema.spec.counts[idx] / 0)": division by zero
   ```

6. Delete the instance:
   ```bash
   kubectl delete -f test-cel-error-foreach-instance.yaml
   ```

## Expected Behavior
The instance should be deleted successfully, even if its forEach collection has CEL errors in identity paths.

## Actual Behavior
The instance gets stuck in deletion:
- `deletionTimestamp` is set
- `state` shows `DELETING`
- Finalizer `kro.run/finalizer` remains on the resource
- Controller continuously retries and hits the same CEL error
- Instance never gets removed from the cluster

### Controller Logs with Debug Tracing
```
[DEBUG] GetDesiredIdentity: entering nodeID=configmaps nodeType=Collection
[DEBUG] resolve: using identity mode, selecting identity paths only nodeID=configmaps nodeType=Collection
[DEBUG] resolve: calling hardResolveCollection nodeID=configmaps mode=1
[DEBUG] hardResolveCollection: entering nodeID=configmaps varsCount=1
[DEBUG] hardResolveCollection: evaluating base expressions nodeID=configmaps baseExprsCount=0 iterExprsCount=1
[DEBUG] hardResolveCollection: calling evaluateForEach nodeID=configmaps
[DEBUG] hardResolveCollection: evaluating iteration expr nodeID=configmaps idx=0 expr="test-" + string(schema.spec.counts[idx] / 0)
[DEBUG] hardResolveCollection: iteration expr failed nodeID=configmaps expr="test-" + string(schema.spec.counts[idx] / 0) err=eval "\"test-\" + string(schema.spec.counts[idx] / 0)": division by zero
[DEBUG] GetDesiredIdentity: resolve failed nodeID=configmaps err=collection iteration eval "\"test-\" + string(schema.spec.counts[idx] / 0)": eval "\"test-\" + string(schema.spec.counts[idx] / 0)": division by zero

DEBUG celerrorforeachtests-controller reporting reconcile error metric {"controller": "celerrorforeachtests", "error": "collection iteration eval \"\\\"test-\" + string(schema.spec.counts[idx] / 0)\": eval \"\\\"test-\" + string(schema.spec.counts[idx] / 0)\": division by zero"}
ERROR dynamic-controller Error syncing item, requeuing with rate limit {"item": {"name":"test-foreach-instance","namespace":"default"}, "error": "collection iteration eval \"\\\"test-\" + string(schema.spec.counts[idx] / 0)\": eval \"\\\"test-\" + string(schema.spec.counts[idx] / 0)\": division by zero"}
```

### Instance Status
```yaml
metadata:
  deletionGracePeriodSeconds: 0
  deletionTimestamp: "2026-04-23T17:38:50Z"
  finalizers:
  - kro.run/finalizer
status:
  conditions:
  - lastTransitionTime: "2026-04-23T17:38:50Z"
    message: deleting resources
    observedGeneration: 2
    reason: UnderDeletion
    status: Unknown
    type: ResourcesReady
  - lastTransitionTime: "2026-04-23T17:38:50Z"
    message: deleting resources
    observedGeneration: 2
    reason: UnderDeletion
    status: Unknown
    type: Ready
  state: DELETING
```

## Root Cause
The code path during deletion for forEach collections:

1. `DeleteTargets()` (pkg/runtime/node.go:284) is called for the collection node
2. It calls `GetDesiredIdentity()` (node.go:200)
3. `GetDesiredIdentity()` calls `resolve(resolveIdentity)` (node.go:201)
4. In `resolve()`, when `mode == resolveIdentity`, it selects only identity path vars (node.go:235-236)
5. For collection nodes, it calls `hardResolveCollection(vars, setIndexLabel)` (node.go:247)
6. `hardResolveCollection()` evaluates base expressions successfully (node_resolve.go:53)
7. It calls `evaluateForEach()` to get the collection items (node_resolve.go:61)
8. Then it iterates through each item and evaluates iteration expressions (node_resolve.go:104-114):
   ```go
   for exprStr := range iterExprs {
       exprState := iterExprStates[exprStr]
       val, err := exprState.Expression.Eval(ctx)
       if err != nil {
           if isCELDataPending(err) {
               return nil, ErrDataPending
           }
           return nil, fmt.Errorf("collection iteration eval %q: %w", exprStr, err)
       }
       values[exprStr] = val
   }
   ```
9. At line 106, the expression evaluation fails with "division by zero"
10. Since it's NOT a pending error (line 108 check fails), it hits line 111:
    ```go
    return nil, fmt.Errorf("collection iteration eval %q: %w", exprStr, err)
    ```
11. Error propagates back up, deletion fails, and controller retries indefinitely

## Analysis
The issue is the same as the single resource case: even during deletion (resolveIdentity mode), the controller evaluates CEL expressions in identity paths. 

**Why does `DeleteTargets()` call `GetDesiredIdentity()` at all?**

For collections, `DeleteTargets()` computes `orderedIntersection(n.observed, desired)` to handle **collection shrinkage**. Example:
- Initially: `spec.counts = [5, 10, 15]` → creates 3 ConfigMaps
- Updated: `spec.counts = [5, 10]` → desired is now 2 ConfigMaps
- Deletion: the third ConfigMap (from idx=2) should be deleted

To compute this intersection, the controller needs to know what the "desired" identities are, which requires evaluating the forEach.

**Why does identity resolution call hardResolveCollection()?**

For forEach collections, the identity of each collection member depends on the iteration variables. In our example:
```yaml
metadata:
  name: ${"test-" + string(schema.spec.counts[idx] / 0)}
```

The `metadata.name` contains `idx` (the iterator variable), making it an **iteration expression**. From debug logs:
```
[DEBUG] hardResolveCollection: evaluating base expressions nodeID=configmaps baseExprsCount=0 iterExprsCount=1
```

To determine what resources exist (their identities), the controller MUST:
1. Evaluate the forEach expression: `lists.range(size(schema.spec.counts))` → `[0, 1, 2]`
2. For each iteration (idx=0, idx=1, idx=2), evaluate the identity expression
3. This produces the identities the controller needs to locate/delete

**Without evaluating the iteration expressions, there's no way to know what ConfigMaps exist** - their names fundamentally depend on the iterator variables.

For forEach collections, this is particularly problematic because:
- The forEach dimension creates multiple resources (one per iteration)
- Each iteration evaluates the identity path expression with different context (idx, iterator values)
- A runtime error in any iteration blocks deletion of the entire collection
- There's no way to identify individual collection members without evaluating their identity expressions
- Even though the error occurs in identity resolution (which *should* be minimal), the expressions are genuinely needed to determine what to delete

**However, during full instance deletion (not collection shrinkage), we could potentially skip identity resolution:**
- If the instance itself is being deleted (has deletionTimestamp), we want to delete ALL observed collection members
- We don't need to compute the intersection with "desired" because nothing is desired anymore
- We could just return `n.observed` directly, like single resources do
- This would allow deletion to proceed even with CEL errors in identity paths

But the current implementation doesn't distinguish between:
1. Collection shrinkage (instance still alive, collection got smaller) - needs intersection
2. Full deletion (instance being deleted) - could skip intersection and delete everything observed

## Proposed Fix

During full instance deletion, we don't need to compute desired identities - we want to delete ALL observed resources regardless of CEL errors.

**Option 1: Check at call site** (pkg/controller/instance/deletion.go:172)
```go
func (c *Controller) deleteTarget(
    rcx *ReconcileContext,
    node *runtime.Node,
    state *NodeState,
) error {
    // Check if instance is being deleted
    isFullDeletion := !rcx.Instance.GetDeletionTimestamp().IsZero()
    
    var targets []*unstructured.Unstructured
    var err error
    
    if isFullDeletion {
        // Full deletion: skip identity resolution, delete everything observed
        targets = node.GetObserved()
    } else {
        // Normal reconciliation: compute intersection for collection shrinkage
        targets, err = node.DeleteTargets()
        if err != nil {
            state.SetError(err)
            return err
        }
    }
    // ... rest of deletion logic
}
```

**Option 2: Add method to Node** (pkg/runtime/node.go)
```go
// DeleteAllTargets returns all observed objects for full deletion.
// Used during instance deletion when we don't need identity resolution.
func (n *Node) DeleteAllTargets() []*unstructured.Unstructured {
    return n.observed
}
```

Then in deletion.go:
```go
func (c *Controller) deleteTarget(...) error {
    var targets []*unstructured.Unstructured
    if !rcx.Instance.GetDeletionTimestamp().IsZero() {
        targets = node.DeleteAllTargets()
    } else {
        targets, err = node.DeleteTargets()
        if err != nil {
            state.SetError(err)
            return err
        }
    }
    // ... rest
}
```

Both approaches:
1. Skip CEL expression evaluation during full instance deletion
2. Allow deletion to proceed even with runtime CEL errors in identity paths
3. Preserve collection shrinkage behavior during normal reconciliation

## Files
- `test-cel-error-foreach.yaml` - RGD with forEach collection and CEL error in identity path
- `test-cel-error-foreach-instance.yaml` - Instance that triggers the error
