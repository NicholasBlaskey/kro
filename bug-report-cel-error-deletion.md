# Bug Report: Instance Stuck During Deletion with CEL Error in Identity Path

## Summary
When an instance has a CEL expression error in an identity path (like `metadata.name`), the instance becomes stuck during deletion and cannot be removed.

## Reproduction Steps

1. Create an RGD with a CEL expression in an identity path that will fail at runtime:
   ```yaml
   # test-cel-error-identity.yaml
   apiVersion: kro.run/v1alpha1
   kind: ResourceGraphDefinition
   metadata:
     name: cel-error-identity-test
   spec:
     schema:
       apiVersion: v1alpha1
       kind: CelErrorTest
       spec:
         count: integer
   
     resources:
       - id: configmap
         template:
           apiVersion: v1
           kind: ConfigMap
           metadata:
             # CEL error in identity path - divide by zero at runtime
             name: ${"test-" + string(schema.spec.count / 0)}
           data:
             test: "value"
   ```

2. Apply the RGD:
   ```bash
   kubectl apply -f test-cel-error-identity.yaml
   ```

3. Create an instance:
   ```yaml
   # test-cel-error-instance.yaml
   apiVersion: kro.run/v1alpha1
   kind: CelErrorTest
   metadata:
     name: test-instance
     namespace: default
   spec:
     count: 5
   ```

4. Apply the instance:
   ```bash
   kubectl apply -f test-cel-error-instance.yaml
   ```

5. Observe the instance fails to reconcile with CEL error:
   ```
   ERROR: node "configmap": eval "\"test-\" + string(schema.spec.count / 0)": division by zero
   ```

6. Delete the instance:
   ```bash
   kubectl delete -f test-cel-error-instance.yaml
   ```

## Expected Behavior
The instance should be deleted successfully, even if it has CEL errors in identity paths.

## Actual Behavior
The instance gets stuck in deletion:
- `deletionTimestamp` is set
- `state` shows `DELETING`
- Finalizer `kro.run/finalizer` remains on the resource
- Controller continuously retries and hits the same CEL error
- Instance never gets removed from the cluster

### Controller Logs
```
DEBUG dynamic-controller Syncing object {"gvr": "kro.run/v1alpha1/celerrortests", "key": {"name":"test-instance","namespace":"default"}}
DEBUG celerrortests-controller reporting reconcile error metric {"controller": "celerrortests", "error": "node \"configmap\": eval \"\\\"test-\\\" + string(schema.spec.count / 0)\": division by zero"}
ERROR dynamic-controller Error syncing item, requeuing with rate limit {"item": {"name":"test-instance","namespace":"default"}, "error": "node \"configmap\": eval \"\\\"test-\\\" + string(schema.spec.count / 0)\": division by zero"}
```

### Instance Status
```yaml
metadata:
  deletionGracePeriodSeconds: 0
  deletionTimestamp: "2026-04-23T17:23:22Z"
  finalizers:
  - kro.run/finalizer
status:
  conditions:
  - lastTransitionTime: "2026-04-23T17:23:22Z"
    message: deleting resources
    observedGeneration: 2
    reason: UnderDeletion
    status: Unknown
    type: ResourcesReady
  - lastTransitionTime: "2026-04-23T17:23:22Z"
    message: deleting resources
    observedGeneration: 2
    reason: UnderDeletion
    status: Unknown
    type: Ready
  state: DELETING
```

## Root Cause

**For single resources, the `GetDesiredIdentity()` call in DeleteTargets() is unnecessary!**

Looking at `DeleteTargets()` (node.go:292-304):
```go
func (n *Node) DeleteTargets() ([]*unstructured.Unstructured, error) {
    switch n.Spec.Meta.Type {
    case graph.NodeTypeCollection, graph.NodeTypeResource:
        desired, err := n.GetDesiredIdentity()
        if err != nil {
            return nil, err
        }
        if n.Spec.Meta.Type == graph.NodeTypeCollection {
            return orderedIntersection(n.observed, desired), nil
        }
        return n.observed, nil  // <-- doesn't use 'desired'!
```

For collections, `desired` is used to compute the intersection (for collection shrinkage). But **for single resources, `desired` is computed and then completely ignored**. The function just returns `n.observed`.

During deletion, the controller follows this code path:

1. `DeleteTargets()` (pkg/runtime/node.go:284) is called to determine what resources to delete
2. For resource/collection nodes, it calls `GetDesiredIdentity()` (node.go:200)  
3. `GetDesiredIdentity()` calls `resolve(resolveIdentity)` (node.go:201)
4. In `resolve()`, when `mode == resolveIdentity`, it selects only identity path vars (node.go:235-236):
   ```go
   if mode == resolveIdentity {
       vars = n.templateVarsForPaths(identityPathsForNodeType(n.Spec.Meta.Type))
   }
   ```
5. For resource nodes, it calls `hardResolveSingleResource(vars)` (node.go:249)
6. This calls `evaluateExprsFiltered(baseExprs, false)` (node_resolve.go:33)
7. Expression evaluation fails with the CEL error at line 212: `val, err := expr.EvalCached(ctx)`
8. Since it's NOT a pending error (line 214 check fails), it hits line 223:
   ```go
   return nil, false, err
   ```
9. Error propagates back up to `hardResolveSingleResource` which wraps it (line 38):
   ```go
   return nil, fmt.Errorf("node %q: %w", n.Spec.Meta.ID, err)
   ```
10. Deletion fails, and controller retries indefinitely

The issue is that even during deletion, when we only need to know *what* to delete (n.observed), we still evaluate the CEL expressions to compute identities that are never used. If those expressions have runtime errors (not caught during static validation), deletion cannot proceed.

**For single resources, the fix is simple: skip the `GetDesiredIdentity()` call entirely and just return `n.observed` directly.**

## Testing Command
```bash
go run ./cmd/controller/main.go ./cmd/controller/pprof.go
```

## Files
- `test-cel-error-identity.yaml` - RGD with CEL error in identity path
- `test-cel-error-instance.yaml` - Instance that triggers the error
