# ApplySet Label Migration

**Date:** April 2026  
**Status:** Implemented

## Summary

Migrated from using `applyset.kubernetes.io/part-of` to `kro.run/owner-{applySetID}=true` as the primary label for listing and tracking managed resources in ApplySets.

## Motivation

The standard KEP-3659 `part-of` label is designed for single-owner scenarios where only one ApplySet manages a resource. When multiple kro instances need to track the same resource (future shared ownership), the `part-of` label gets overwritten since there can only be one value.

By introducing `owner-*` labels where each ApplySet ID gets its own label key, multiple kro instances can independently track ownership via separate labels:
- Instance A: `kro.run/owner-{idA}=true`
- Instance B: `kro.run/owner-{idB}=true`

## Implementation

### Label Strategy

All applied resources now receive **both** labels:
```yaml
metadata:
  labels:
    applyset.kubernetes.io/part-of: "applyset-{hash}-v1"     # KEP-3659 compliance
    kro.run/owner-{applySetID}: "true"                        # Multi-instance tracking
```

### Listing

Changed from:
```go
labelSelector: "applyset.kubernetes.io/part-of={applySetID}"
```

To:
```go
labelSelector: "kro.run/owner-{applySetID}=true"
```

### Conflict Detection

Updated to check both labels for backwards compatibility:
1. First check `kro.run/owner-*` labels (new format)
2. Fall back to `applyset.kubernetes.io/part-of` (old format)

This ensures conflicts are detected for resources applied by both old and new kro versions during migration.

### Parent Object Labels

Parent objects (ResourceGroup instances) continue to use only `applyset.kubernetes.io/id` per KEP-3659. The owner label is only needed on child resources for listing/pruning.

## Migration Path

### Known Limitation

Resources applied with older kro versions will only have the `part-of` label. These won't be found by the new `owner-*` selector until they're re-applied.

**Impact:** Orphaned resources (resources removed from RGD but not yet cleaned up) won't be pruned in the first reconcile after upgrade.

**Mitigation:** This is acceptable for short upgrade windows (<1 hour). Only affects resources that were:
1. Removed from the RGD
2. Not already cleaned up before upgrade

**Alternative:** Could list twice (both selectors) at cost of 2x API calls. Currently not implemented.

### Upgrade Process

1. Deploy new kro version
2. First reconcile: Resources re-applied get both labels
3. Subsequent reconciles: All resources have owner label, full functionality restored

## Files Changed

- `pkg/controller/instance/applyset/const.go` - Added `OwnerLabelPrefix` constant
- `pkg/controller/instance/applyset/applyset.go` - Updated listing selector, conflict detection, label injection
- `pkg/controller/instance/applyset/applyset_test.go` - Updated test fixtures to use owner labels

## Future Work

This label migration lays the foundation for:
- Shared ownership mode (multiple instances managing same resource)
- Cooperative field management between instances
- Enhanced conflict resolution strategies

See `docs/design/proposals/ssa-conflict-resolution.md` for planned shared ownership features.
