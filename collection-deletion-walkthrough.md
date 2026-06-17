# Collection Deletion: Why orderedIntersection?

## Key Facts
1. `DeleteTargets()` is ONLY called during **instance deletion**, never during normal reconciliation
2. Collection shrinkage during normal reconciliation uses **applyset prune**, not `DeleteTargets()`
3. For collections, `DeleteTargets()` returns `orderedIntersection(n.observed, n.desired)`

## The Question
**Why compute the intersection during instance deletion? Why not just delete everything observed?**

## Example Scenario

### Setup
RGD with forEach collection:
```yaml
resources:
  - id: configs
    forEach:
      - region: ${schema.spec.regions}
    template:
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: ${region}
        namespace: ${schema.metadata.namespace}
```

### Instance State
```yaml
apiVersion: v1alpha1
kind: RegionalApp
metadata:
  name: my-app
  namespace: tenant-a
spec:
  regions: ["east", "west"]
```

### Cluster State (Observed)
Three ConfigMaps exist in namespace `tenant-a`:
1. `ConfigMap/east` - created by this instance
2. `ConfigMap/west` - created by this instance  
3. `ConfigMap/orphan` - created manually OR leftover from a previous bug

All three have the kro labels (e.g., `kro.run/resource-graph-definition-name: regional-configs`)

### Instance Gets Deleted
User runs: `kubectl delete regionalapp my-app`

**What should happen?**
Delete ConfigMaps "east" and "west" (the legitimate collection members)

**What about "orphan"?**
- If we delete ALL observed: delete "east", "west", "orphan"
- If we compute intersection: delete only "east", "west"

## The orderedIntersection Logic

```go
func (n *Node) DeleteTargets() ([]*unstructured.Unstructured, error) {
    // Compute what SHOULD exist based on current instance spec
    desired, err := n.GetDesiredIdentity()  // Evaluates forEach → [east, west]
    if err != nil {
        return nil, err  // <-- THIS IS THE BUG!
    }
    
    // Return only observed resources that match desired identities
    return orderedIntersection(n.observed, desired), nil
}
```

**Result:**
- `observed = [east, west, orphan]`
- `desired = [east, west]`
- `intersection = [east, west]` (in desired order)
- Only "east" and "west" are deleted
- "orphan" is left behind

## Why This Design?

The intersection provides **safety** during deletion:
- Only delete resources that are "legitimate" members of the collection
- Don't delete resources that just happen to have the same labels
- Handle edge cases where extra resources exist

However, this creates a **dependency on CEL evaluation**:
- If evaluating `desired` fails (CEL error), we cannot determine what to delete
- The instance gets stuck, even though we conceptually want to delete everything

## The Trade-off

### Option 1: Current Behavior (intersection)
```go
targets = orderedIntersection(observed, desired)
```
- ✅ Safe: only deletes legitimate collection members
- ❌ Fragile: fails if CEL expressions have runtime errors
- ❌ Leaves orphans if desired cannot be computed

### Option 2: Delete Everything (proposed for full deletion)
```go
if instance.HasDeletionTimestamp():
    targets = observed  // Delete everything with kro labels
```
- ✅ Robust: works even with CEL errors
- ✅ Completes deletion reliably
- ❌ Might delete "orphan" resources
- ❓ Is deleting orphans during instance deletion actually a problem?

## Conclusion

During full instance deletion:
- We're deleting the entire instance anyway
- All resources with kro labels were created/managed by this instance at some point
- Even if "orphan" exists, it's better to delete it than leave the instance stuck
- Users can force-remove finalizers if they really want to preserve orphans

**Recommendation:** During full instance deletion (`!deletionTimestamp.IsZero()`), skip `GetDesiredIdentity()` and just return `observed` directly. This allows deletion to proceed even with CEL errors.
