# Shared Resource Ownership Model

**Status**: Draft
**Authors**: @nblaskey
**Created**: 2026-04-02
**Updated**: 2026-04-02

## Problem Statement

Currently, kro uses Server-Side Apply (SSA) with force=true on all managed resources. This means a single kro ResourceGraph has exclusive ownership of the entire resource - any other manager (including other kro instances) gets forcibly overwritten.

This prevents multiple ResourceGraphs from collaborating on the same resource. For example:
- One RGD manages a Deployment's replica count and scaling behavior
- Another RGD manages the same Deployment's security labels and annotations
- A third RGD manages the container image and environment variables

With the current exclusive ownership model, only one RGD can successfully manage the Deployment.

## Goals

1. **Enable multi-RGD resource composition**: Allow multiple ResourceGraphs to manage different fields of the same resource
2. **First-writer-wins**: Ensure the first kro instance to claim a field retains ownership (prevents flip-flopping)
3. **Evict non-kro drift**: Automatically correct drift from kubectl/helm/other tools
4. **Field evolution**: Allow an RGD to evolve the values of fields it owns over time
5. **Clear conflict reporting**: Surface conflicts with actionable error messages
6. **Leverage Kubernetes primitives**: Use SSA managedFields and conflict detection

## Non-Goals

1. **Automatic conflict resolution**: We will not attempt to merge conflicting values from multiple kro instances
2. **Cross-cluster coordination**: This is single-cluster only
3. **Strong consistency**: Brief windows of eventual consistency are acceptable when evicting non-kro drift

## Core Ownership Rules

The Shared ownership model enforces three rules:

### Rule 1: Non-kro managers are evicted
Any field owned by a non-kro manager (kubectl, helm, etc.) is forcibly overwritten.

**Rationale**: We want kro to own the declared state. External drift should be corrected.

### Rule 2: First-writer-wins for kro instances
If another kro instance owns a field we want, we cannot take it. The first kro instance to claim a field retains permanent ownership.

**Rationale**: Prevents flip-flopping between kro instances. Stable, predictable ownership.

### Rule 3: Field evolution by owners
If we already own a field, we can modify its value freely, even if another kro instance tries to claim it.

**Rationale**: Allows RGDs to evolve over time. The owner can upgrade/change field values.

## Design Exploration

We explored multiple approaches to implement these three rules within SSA's constraints.

### Constraint: SSA's Force Flag is All-Or-Nothing

From [Kubernetes SSA documentation](https://kubernetes.io/docs/reference/using-api/server-side-apply/):

> "This forces the operation to succeed, changes the value of the field, and **removes the field from all other managers' entries in managedFields**."

The `force=true` flag applies to the **entire patch**, not individual fields. You cannot say "force this field but not that field" in a single apply operation.

This creates a fundamental tension:
- **Rule 1** requires `force=true` (to evict non-kro)
- **Rule 2** requires `force=false` (to respect kro first-writer-wins)
- **Rule 3** requires selective forcing (force our fields, not others)

### Approaches Considered

#### Approach A: Pure Shared (force=false always)
Use `force=false` for all applies, allow SSA conflicts to enforce first-writer-wins.

**Pros**: Clean SSA semantics, no stomping other kro instances
**Cons**: Cannot evict non-kro drift (violates Rule 1)
**Decision**: ❌ Rejected - evicting drift is a core requirement

#### Approach B: Eventual Consistency (force=true always)
Use `force=true` for all applies, accept temporary stomping of other kro instances, rely on reconciliation to restore consistency.

**Pros**: Simple, evicts non-kro drift
**Cons**: Kro instances stomp each other, no first-writer-wins guarantee (violates Rule 2)
**Decision**: ❌ Rejected - first-writer-wins is critical

#### Approach C: Manual managedFields Editing
Directly PATCH managedFields to remove non-kro managers, then apply with `force=false`.

From K8s docs:
> "It is however possible to change `.metadata.managedFields` through an update... **Doing so is highly discouraged**"

**Pros**: Surgical eviction of non-kro only, preserves kro ownership
**Cons**: Explicitly discouraged by K8s, brittle, potential edge cases
**Decision**: ❌ Rejected - too hacky, violates K8s guidance

#### Approach D: force=false with selective force=true (CHOSEN)
Try `force=false` first. On conflict, determine if conflicting owner is overrideable. If so, retry with `force=true`.

**Pros**: 
- Leverages SSA conflict detection
- Only force when necessary
- Standard K8s operations

**Cons**:
- Risk of field theft in race conditions
- Requires careful patch splitting

**Decision**: ✅ **Selected** - Best balance of correctness and simplicity

### Critical Race Condition: Field Theft

Initial version of Approach D had a critical flaw:

**Scenario:**
```
t0: Resource: {a: 1 (kubectl), b: unowned}
t1: We want: {a: 1, b: 2}
t2: We: force=false {a,b} → conflict on a
t3: We: GET → kubectl owns a → decide to force=true
t4: Instance B: force=false {b: 3} → SUCCESS, B owns b
t5: We: force=true {a: 1, b: 2} → SUCCESS
t6: Result: We own BOTH a and b (STOLE b from B!)
```

**Problem**: `force=true` is all-or-nothing. When forcing to evict kubectl from `a`, we also force `b` even though B was first writer.

**Solution**: Split the patch into two applies:
1. `force=true` on fields needing eviction (non-kro owners)
2. `force=false` on remaining fields (unowned or we own)

The second apply will **conflict** if another kro claimed the field between our GET and PATCH, preserving first-writer-wins.

## Final Algorithm: Split Patch with Selective Force

```go
func ApplySharedResource(ctx context.Context, desired *unstructured.Unstructured) error {
    ourManager := "kro-" + instanceUID
    
    // Step 1: Try force=false on everything first (cooperative mode)
    err := client.Patch(ctx, desired, client.Apply,
        client.FieldOwner(ourManager))
    
    if err == nil {
        return nil // Success - no conflicts
    }
    
    if !isConflict(err) {
        return err // Some other error
    }
    
    // Step 2: Conflict occurred - GET current state and classify fields
    current := &unstructured.Unstructured{}
    if err := client.Get(ctx, ObjectKeyFor(desired), current); err != nil {
        return err
    }
    
    ownership := parseManagedFields(current.GetManagedFields())
    desiredFields := extractFieldPaths(desired)
    
    fieldsNeedingForce := []string{}   // Non-kro owners - must evict (Rule 1)
    fieldsNoForce := []string{}        // Unowned or we own - cooperative
    fieldsBlocked := []string{}        // Other kro owns - error (Rule 2)
    
    for _, field := range desiredFields {
        owner := ownership[field]
        switch {
        case owner == ourManager:
            fieldsNoForce = append(fieldsNoForce, field)  // Rule 3: we can evolve
        case owner == "":
            fieldsNoForce = append(fieldsNoForce, field)  // Unowned: claim cooperatively
        case !strings.HasPrefix(owner, "kro-"):
            fieldsNeedingForce = append(fieldsNeedingForce, field)  // Rule 1: evict non-kro
        default: // other kro instance
            fieldsBlocked = append(fieldsBlocked, field)  // Rule 2: first-writer-wins
        }
    }
    
    // Step 3: Error if trying to take from another kro (Rule 2)
    if len(fieldsBlocked) > 0 {
        return fmt.Errorf("field ownership conflict with another kro instance:\n"+
            "Fields: %v\n"+
            "Owner: %s\n"+
            "Cannot override another kro instance (first writer wins).\n"+
            "Remove these fields from your ResourceGraph template to resolve.",
            fieldsBlocked, ownership[fieldsBlocked[0]])
    }
    
    // Step 4: Apply fields needing force (evict non-kro, Rule 1)
    if len(fieldsNeedingForce) > 0 {
        forcePatch := filterFields(desired, fieldsNeedingForce)
        err := client.Patch(ctx, forcePatch, client.Apply,
            client.FieldOwner(ourManager),
            client.ForceOwnership)  // force=true
        if err != nil {
            return fmt.Errorf("failed to evict non-kro managers: %w", err)
        }
    }
    
    // Step 5: Apply remaining fields cooperatively (catches ownership races)
    if len(fieldsNoForce) > 0 {
        noForcePatch := filterFields(desired, fieldsNoForce)
        err := client.Patch(ctx, noForcePatch, client.Apply,
            client.FieldOwner(ourManager))  // force=false
        
        if err != nil && isConflict(err) {
            // Someone claimed a field between our GET (step 2) and now
            conflictOwner := extractConflictOwner(err)
            if strings.HasPrefix(conflictOwner, "kro-") {
                // Another kro claimed it first - respect first-writer-wins!
                return fmt.Errorf("field ownership race: another kro instance claimed field first: %s\n"+
                    "This field is now owned by a different ResourceGraph.",
                    conflictOwner)
            }
            // Non-kro conflict shouldn't happen but return it
            return err
        }
        
        return err
    }
    
    return nil
}
```

### Why Split Patches Work

**Key insight**: By splitting into two patches, we prevent field theft:

1. **Force patch** contains only fields with non-kro owners
   - Uses `force=true` to evict kubectl/helm/etc
   - Only touches fields that need eviction
   - Cannot steal fields from other kro instances (they're not in this patch)

2. **Cooperative patch** contains unowned fields or fields we already own
   - Uses `force=false` to respect SSA conflict detection
   - If another kro claimed a field between step 2 and step 5, SSA conflict fires
   - First-writer-wins is preserved by rejecting the conflict

**Example prevention of field theft:**

```
t0: Resource: {a: 1 (kubectl), b: unowned}
t1: We want: {a: 1, b: 2}
t2: We: force=false {a,b} → conflict on a
t3: We: GET → fieldsNeedingForce=[a], fieldsNoForce=[b]
t4: Instance B: force=false {b: 3} → SUCCESS, B owns b
t5: We: force=true patch={a: 1} → SUCCESS (evicts kubectl, doesn't touch b)
t6: We: force=false patch={b: 2} → CONFLICT with kro-B!
t7: We: ERROR - B was first writer ✓
```

Without split (field theft):
```
t5: We: force=true patch={a: 1, b: 2} → SUCCESS (steals b from B) ❌
```

### Resource Schema

Add `ownership` field to resource spec:

```yaml
spec:
  resources:
    - id: my-deployment
      ownership: Shared  # or Exclusive (default)
      template: |
        apiVersion: apps/v1
        kind: Deployment
        # ...
```

### Manager Name Format

Kro uses SSA manager names of the format:
```
kro-<instance-uid>
```

**Detection**: A managedFields entry is considered "kro-owned" if the manager name starts with `kro-`.

## Trade-offs

### Benefits

✅ **Multi-RGD composition**: Multiple ResourceGraphs can manage different fields of the same resource
✅ **First-writer-wins guarantee**: Stable field ownership via SSA conflict detection
✅ **Non-kro drift correction**: Automatic eviction of kubectl/helm modifications
✅ **Field evolution**: Owners can modify field values freely
✅ **Clear error messages**: Conflicts point to specific fields and owners
✅ **Standard K8s primitives**: No custom CRDs, uses SSA semantics
✅ **Scales to N instances**: No limit on number of collaborating RGDs

### Costs

⚠️ **2 API calls when drift exists**: Force patch + cooperative patch
⚠️ **Eventual consistency**: Brief windows where instances may stomp each other during non-kro eviction
⚠️ **More complex implementation**: Field classification, patch filtering, two-phase apply
⚠️ **Parsing managedFields**: Requires walking nested JSON structure to extract field paths
⚠️ **User coordination required**: Overlapping field declarations cause permanent errors

### Acceptable Limitations

**Eventual consistency during drift**

When non-kro managers (kubectl) modify fields, multiple kro instances may briefly stomp each other while evicting the drift. System converges to stable state once drift is eliminated.

**Timeframe**: Typically 1-2 reconciliation cycles (seconds to minutes)
**Mitigation**: Fast reconciliation loops minimize convergence time

**Permanent errors on misconfiguration**

If two RGDs declare overlapping fields, both will error. User must manually fix templates.

**Rationale**: This is correct behavior - overlapping ownership is ambiguous and should be rejected.
**Mitigation**: Clear error messages guide resolution

**Race on new field claims**

When multiple instances try to claim the same unowned field, race determines winner.

**Rationale**: No "first" writer exists yet - race is acceptable.
**Mitigation**: SSA conflict detection ensures only one winner

## Implementation

### Phase 1: Core Infrastructure

**1. Add `Ownership` field to ResourceGraphDefinition CRD**
```yaml
spec:
  resources:
    - id: string
      ownership: Exclusive | Shared  # default: Exclusive
      template: string
```

**2. Implement `managedFieldsParser`**
- Parse `metadata.managedFields` from Unstructured
- Extract field paths from `fieldsV1` JSON structure
- Build map of field path → manager name

**3. Implement `fieldPathExtractor`**
- Walk desired object tree
- Collect all field paths (e.g., `spec.replicas`, `spec.template.metadata.labels.app`)
- Handle nested objects, arrays, maps

**4. Implement `patchFilter`**
- Given desired object + list of field paths
- Build new Unstructured containing only those fields
- Preserve GVK, namespace, name metadata

### Phase 2: Apply Logic

**5. Modify `ApplySet.applyResource()`**
- Check resource `ownership` mode
- If `Exclusive`: use existing force=true logic
- If `Shared`: use new split-patch algorithm

**6. Implement split-patch algorithm** (as detailed in Final Algorithm section)
- Try force=false first
- On conflict, GET and classify fields
- Apply force patch (non-kro eviction)
- Apply cooperative patch (claim new fields)

**7. Add conflict owner extraction**
- Parse SSA conflict errors
- Extract manager name from error message
- Determine if manager is kro or non-kro

### Phase 3: Error Handling & Observability

**8. Custom error types**
```go
type SharedOwnershipConflictError struct {
    Fields      []string
    Owner       string
    OurManager  string
}
```

**9. Metrics**
```go
resource_apply_split_patches_total{ownership_mode="shared"}
resource_apply_conflicts_total{conflict_type="kro|non-kro"}
resource_field_theft_prevented_total
```

**10. Logging**
- Log field classification decisions
- Log force vs cooperative patch decisions
- Log conflict resolutions

### Phase 4: Testing & Documentation

**11. Unit tests** (see Testing Strategy)
**12. Integration tests** (see Testing Strategy)
**13. E2E tests** (see Testing Strategy)
**14. User documentation**
- When to use Shared vs Exclusive
- How to resolve conflicts
- Example multi-RGD compositions

## Example Use Cases

### Use Case 1: Separation of Concerns

```yaml
# application-rgd.yaml - manages app logic
spec:
  resources:
    - id: app-deployment
      ownership: Shared
      template: |
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: myapp
        spec:
          template:
            spec:
              containers:
                - name: app
                  image: ${schema.image}
                  env: ${schema.env}

---
# scaling-rgd.yaml - manages scaling
spec:
  resources:
    - id: app-deployment
      ownership: Shared
      template: |
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: myapp
        spec:
          replicas: ${schema.replicas}

---
# security-rgd.yaml - manages security posture
spec:
  resources:
    - id: app-deployment
      ownership: Shared
      template: |
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: myapp
          labels:
            security.policy: ${schema.securityLevel}
        spec:
          template:
            metadata:
              annotations:
                seccomp.profile: runtime/default
```

Each RGD manages independent fields of the same Deployment. No conflicts because field paths don't overlap.

### Use Case 2: Conflict Detection

```yaml
# scaling-rgd-1.yaml
spec:
  resources:
    - id: app-deployment
      ownership: Shared
      template: |
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: myapp
        spec:
          replicas: 3

# scaling-rgd-2.yaml (CONFLICTS!)
spec:
  resources:
    - id: app-deployment
      ownership: Shared
      template: |
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: myapp
        spec:
          replicas: 10  # CONFLICT: scaling-rgd-1 already owns spec.replicas
```

User gets clear error pointing to the conflict and which RGD to fix.

## Edge Case Analysis

### Scenario 1: Three Instances, Non-Overlapping Fields

```yaml
Instance A wants: spec.replicas
Instance B wants: spec.strategy
Instance C wants: spec.template.labels
```

**Outcome**: All apply with `force=false` → no conflicts → **all succeed ✓**

**Key**: Each patch only contains that instance's fields. No overlap, no stomping.

### Scenario 2: kubectl Drifts One Owner's Field

```yaml
Instance A owns: spec.replicas
Instance B owns: spec.strategy
Instance C owns: spec.template.labels
kubectl modifies: spec.replicas (drift)
```

**What happens:**
- A reconciles: `force=false` → conflict → GET → `fieldsNeedingForce=[replicas]` → `force=true` on `{replicas: 3}`
- B reconciles: `force=false` → succeeds (no conflict on strategy)
- C reconciles: `force=false` → succeeds (no conflict on labels)

**Outcome**: A evicts kubectl from its field only. B and C unaffected. ✓

**Key**: A's force patch only contains `spec.replicas`, doesn't touch B or C's fields.

### Scenario 3: kubectl Drifts All Fields

```yaml
Instance A owns: spec.replicas
Instance B owns: spec.strategy
Instance C owns: spec.template.labels
kubectl modifies: ALL THREE
```

**What happens**: All three reconcile concurrently:
- A: `force=true` on `{replicas}`
- B: `force=true` on `{strategy}`
- C: `force=true` on `{labels}`

These patches are independent and can interleave safely. No stomping because each patch only contains that instance's fields. ✓

### Scenario 4: Two Instances Want Same New Field

```yaml
Instance A owns: spec.replicas
Instance B owns: spec.strategy
Both now want: spec.template.labels (new field, unowned)
```

**Race**:
```
t1: A: force=false {replicas, labels} → SUCCESS (A owns both)
t2: B: force=false {strategy, labels} → CONFLICT on labels
t3: B: GET → kro-A owns labels → ERROR
```

**Outcome**: First writer (A) wins. B gets clear error. ✓

### Scenario 5: Overlapping Template Declaration (Misconfiguration)

```yaml
RGD-A template: {replicas: 3, strategy: RollingUpdate}
RGD-B template: {strategy: Recreate, labels: {...}}
```

**What happens**:
```
A applies first: owns replicas + strategy
B applies: force=false → conflict on strategy
B: GET → kro-A owns strategy → ERROR
```

**Outcome**: Permanent error state. User must fix RGD templates to not overlap. ✓

**This is correct behavior** - overlapping field declarations are user configuration errors.

### Scenario 6: Both Instances Evict Same Non-kro Field

```yaml
kubectl owns: spec.replicas
Instance A wants: spec.replicas
Instance B wants: spec.replicas
```

**Race**:
```
A: force=false → conflict → force=true {replicas} → SUCCESS
B: force=false → conflict → force=true {replicas} → SUCCESS (takes from A)
Next cycle:
A: force=false → conflict with kro-B → ERROR
B: keeps field ✓
```

**Outcome**: Both detect kubectl ownership and try to evict. Race determines winner. Loser gets permanent error.

**Acceptable**: Neither was "first writer" - both tried to claim simultaneously. Race to evict kubectl determines winner.

### Deadlock Analysis

**Can the system deadlock?**

No true deadlock is possible because:
- ✅ No mutex/lock holding - each reconciliation is stateless
- ✅ No "wait" state - operations either succeed, fail, or retry
- ✅ No circular waiting - each instance operates independently

**Permanent error states (not deadlock):**

**Circular ownership desires:**
```
A owns X, wants Y
B owns Y, wants X
```

Both permanently error with "first-writer-wins" conflicts. This is **correct behavior** - overlapping RGDs are misconfigured. User must fix templates.

**Livelock (temporary, converges):**

If kubectl keeps modifying a field that two instances want:
```
A: force=true → owns field
kubectl: modifies
B: force=true → owns field
kubectl: modifies
A: force=true → owns field
...
```

**Converges** once kubectl stops modifying. Then one instance wins permanently.

### Remaining Race Conditions

**Acceptable race: Non-kro eviction timing**

When multiple instances evict non-kro owners, they may temporarily stomp each other. This is eventual consistency - system converges once non-kro drift stops.

**Not a concern**: Once all non-kro managers are evicted, the system reaches stable state with first-writer-wins enforced by SSA conflicts.

**Bounded impact**: Race only affects fields with active non-kro drift. Other fields remain stable.

## Testing Strategy

### Unit Tests

1. **managedFields parsing**: Extract owner for each field path
2. **Field classification**: Correctly identify fields needing force vs cooperative apply
3. **Patch filtering**: Build partial patches containing only specified fields
4. **Conflict detection**: Identify kro vs non-kro managers from error messages

### Integration Tests

1. **Two RGDs, non-overlapping fields**: Both succeed
2. **Two RGDs, overlapping fields**: Second fails with conflict error
3. **Three+ RGDs, complex field topology**: All non-overlapping fields succeed
4. **RGD evolution**: First RGD adds new field after initial apply
5. **Non-kro manager eviction**: kubectl apply gets overwritten, other kro fields preserved
6. **Owner retains field**: First owner evolves field value, second owner conflicts
7. **Split patch race**: Instance claims field between first and second patch, first-writer-wins preserved
8. **Concurrent non-kro eviction**: Multiple instances evict kubectl, system converges

### E2E Tests

1. **Multi-RGD application composition**: Deploy real workload with multiple RGDs (app, scaling, security)
2. **Conflict resolution workflow**: Demonstrate how to fix overlapping field declarations
3. **Migration from Exclusive to Shared**: Change ownership mode on existing resource
4. **Drift correction**: kubectl modifies fields, kro corrects them
5. **Three-way race**: Multiple instances claim new fields, first-writer-wins verified

## Metrics

```go
resource_apply_conflicts_total{ownership_mode="shared", conflict_type="kro|non-kro"}
resource_ownership_mode{mode="exclusive|shared"}
resource_field_ownership_changes_total
```

## Documentation

### User Guide

#### When to use Shared ownership

Use `ownership: Shared` when:
- Multiple ResourceGraphs need to manage different aspects of the same resource
- You want explicit conflict detection rather than silent overwrites
- Field-level ownership boundaries are clear

Use `ownership: Exclusive` (default) when:
- Single RGD owns the entire resource
- You want simple force-apply semantics
- You're migrating from current behavior

#### Conflict resolution

When you get a field ownership conflict:

1. **Identify the conflicting RGDs**: Check the error message for manager names
2. **Decide who should own the field**: Business logic decision
3. **Update the RGD that should NOT own the field**: Remove the conflicting field from its template
4. **Alternative**: Change one RGD to `ownership: Exclusive` to force ownership

## Security Considerations

None. This is a resource apply strategy change that uses existing Kubernetes RBAC and admission control.

## Alternatives Considered

### Alternative 1: Pure Shared (force=false always)

**Approach**: Always use `force=false` for Shared mode. Let SSA conflicts enforce first-writer-wins. Never evict non-kro managers.

**Pros**:
- Clean SSA semantics
- No risk of stomping other kro instances
- Simple implementation

**Cons**:
- Cannot evict non-kro drift (violates Rule 1)
- kubectl/helm modifications persist
- Users must manually clean up drift

**Decision**: ❌ Rejected - automatic drift correction is a core requirement

---

### Alternative 2: Eventual Consistency (force=true always)

**Approach**: Always use `force=true`. Accept that kro instances will temporarily stomp each other. Rely on reconciliation to restore consistency.

**Pros**:
- Simple implementation
- Evicts non-kro drift

**Cons**:
- No first-writer-wins guarantee (violates Rule 2)
- Continuous flip-flopping between instances
- Higher API churn
- Confusing behavior for users

**Decision**: ❌ Rejected - first-writer-wins is critical for stable ownership

---

### Alternative 3: Manual managedFields Editing

**Approach**: 
1. GET resource and managedFields
2. Remove non-kro managers from managedFields
3. PATCH managedFields back using non-SSA update
4. Apply with force=false (no conflicts)

From [K8s SSA documentation](https://kubernetes.io/docs/reference/using-api/server-side-apply/):
> "It is however possible to change `.metadata.managedFields` through an update... **Doing so is highly discouraged**"

**Pros**:
- Surgical eviction of non-kro only
- Preserves all kro field ownership
- No risk of stomping other kro

**Cons**:
- Explicitly discouraged by Kubernetes
- Brittle if managedFields format changes
- Violates K8s design guidance
- Potential audit trail issues

**Decision**: ❌ Rejected - too hacky, violates best practices

---

### Alternative 4: force=false with selective force=true (NO split)

**Approach**: Try force=false. On conflict, determine if conflicting owner is overrideable. If so, retry with force=true on entire patch.

**Pros**:
- Leverages SSA conflict detection
- Only forces when necessary
- Simple implementation (no patch splitting)

**Cons**:
- **Critical flaw**: Field theft in race conditions
- When forcing to evict kubectl from field A, also steals field B from another kro instance

**Decision**: ❌ Rejected - violates first-writer-wins in important edge case

---

### Alternative 5: Split Patch with Selective Force (CHOSEN)

**Approach**: 
1. Try force=false on all fields
2. On conflict, GET and classify fields
3. Apply force=true on fields with non-kro owners only
4. Apply force=false on remaining fields (catches races)

**Pros**:
- Evicts non-kro drift (Rule 1) ✓
- Preserves first-writer-wins (Rule 2) ✓
- Allows field evolution (Rule 3) ✓
- Standard K8s operations
- Prevents field theft via second apply

**Cons**:
- 2 API calls when drift exists
- More complex implementation
- Still eventual consistency during non-kro eviction

**Decision**: ✅ **Selected** - Best balance of correctness, safety, and complexity

---

### Alternative 6: Namespace-based isolation

**Approach**: Each RGD manages resources in separate namespaces, then link via Services.

**Pros**: Complete isolation, no shared resource conflicts

**Cons**: 
- Not all resources are namespaced
- Doesn't solve multi-RGD composition for same resource
- Operational overhead

**Decision**: ❌ Not sufficient for the use case

---

### Alternative 7: Explicit field ownership annotations

**Approach**: Let users declare which fields each RGD owns:

```yaml
resources:
  - id: deployment
    ownedFields:
      - spec.replicas
      - spec.template.metadata.labels
```

**Pros**: Explicit and clear upfront

**Cons**:
- Duplicates information already in template
- Hard to keep in sync with template evolution
- Doesn't prevent conflicts, just documents them

**Decision**: ❌ Rely on SSA managedFields as source of truth instead

---

### Alternative 8: Optimistic Locking with resourceVersion

**Approach**: Use resourceVersion as precondition for PATCH to detect concurrent modifications.

**Exploration**: 
- Standard PATCH supports resourceVersion preconditions
- Unclear if SSA Apply honors resourceVersion in patch object
- Would prevent races if supported

**Decision**: ⏳ Needs research - may revisit if race conditions prove problematic in production

## Future Work

1. **Field ownership visualization**: CLI/UI showing which RGD owns which fields
2. **Ownership transfer**: Explicit API to transfer field ownership between RGDs
3. **Cross-RGD validation**: Warn at build time if two RGDs declare overlapping fields
4. **Automatic conflict resolution**: Policy-based resolution (newest wins, priority-based, etc.)

## Decision Summary

### Core Decisions

| Decision | Rationale |
|----------|-----------|
| **Two ownership modes**: Exclusive (default), Shared | Backward compatible, opt-in for collaboration |
| **Split patch algorithm** | Prevents field theft, preserves first-writer-wins |
| **force=false first, then selective force=true** | Leverages SSA conflict detection, only forces when necessary |
| **Error on kro conflicts** | First-writer-wins enforced by rejecting subsequent claims |
| **Allow eventual consistency** | Brief stomping during non-kro eviction is acceptable |
| **No automatic conflict resolution** | User must fix overlapping RGD templates |

### Key Insights

1. **SSA's force flag is all-or-nothing** - Cannot force some fields but not others in single apply
2. **Split patches solve field theft** - Separate force and cooperative applies prevent races
3. **First-writer-wins via SSA conflicts** - Let API server detect and reject duplicate claims
4. **Eventual consistency is acceptable** - Converges quickly once non-kro drift stops
5. **No true deadlock possible** - Permanent errors on misconfiguration are correct behavior

### What We're NOT Doing

❌ Manual managedFields editing (too hacky)
❌ Pure force=false (can't evict drift)
❌ Pure force=true (stomps other kro)
❌ Advisory locks (adds complexity)
❌ Automatic conflict resolution (ambiguous)

## Open Questions

1. **Manager name format**: Should we include more context than just instance UID? (e.g., `kro-<rgd-name>-<instance-uid>`)
   - **Consideration**: More context helps debugging but makes manager names longer
   
2. **Conflict retries**: Should we auto-retry conflicts with exponential backoff, or fail fast?
   - **Current**: Fail fast, rely on next reconciliation cycle
   - **Alternative**: Retry 3 times with jitter before failing
   
3. **resourceVersion preconditions**: Can we use resourceVersion to prevent all races?
   - **Status**: Needs research to determine if SSA Apply honors resourceVersion
   - **Benefit**: Would eliminate field theft risk entirely
   
4. **Convergence time tuning**: What reconciliation interval minimizes inconsistency windows?
   - **Trade-off**: Faster reconciliation = less drift window but higher API load
   
5. **Metrics for drift detection**: Should we expose metrics on non-kro manager presence?
   - **Benefit**: Helps operators detect persistent external modifications
   
6. **Migration path**: How to migrate existing Exclusive resources to Shared?
   - **Consideration**: Changing mode on live resource may cause temporary conflicts

## Implementation Considerations

### managedFields Parsing

The `fieldsV1` structure in managedFields is a nested JSON representing field ownership:

```json
{
  "f:metadata": {
    "f:labels": {
      "f:app": {}
    }
  },
  "f:spec": {
    "f:replicas": {}
  }
}
```

**Field path extraction**:
- Walk the JSON tree
- Build dot-notation paths: `metadata.labels.app`, `spec.replicas`
- Handle arrays with merge keys: `k:{"name":"container"}` → `containers[name=container]`

**Complexity**: Requires recursive tree walker with special handling for Kubernetes strategic merge semantics.

### Patch Filtering

To build partial patches containing only specific fields:

```go
func filterFields(desired *unstructured.Unstructured, fields []string) *unstructured.Unstructured {
    result := &unstructured.Unstructured{}
    result.SetGroupVersionKind(desired.GroupVersionKind())
    result.SetNamespace(desired.GetNamespace())
    result.SetName(desired.GetName())
    
    for _, fieldPath := range fields {
        value, found := getNestedField(desired.Object, fieldPath)
        if found {
            setNestedField(result.Object, fieldPath, value)
        }
    }
    
    return result
}
```

**Challenges**:
- Preserving parent structures (to set `spec.replicas`, need `spec: {}` wrapper)
- Handling array elements with merge keys
- Map keys with special characters

### Conflict Owner Extraction

SSA conflict errors contain the conflicting manager name:

```
Apply failed with 1 conflict: conflict with "kro-abc123": 
  .spec.replicas
```

**Parsing strategy**:
- Use regex to extract manager name from error message
- Fall back to checking if "kro-" prefix exists anywhere in error
- Handle potential error message format changes across K8s versions

### Performance Optimizations

**1. Fast path for no conflicts**
- First apply with force=false
- If succeeds, return immediately (no GET, no classification)
- Only pay cost of split patches when conflicts exist

**2. Cache parsed managedFields**
- Parse once per reconciliation
- Reuse for multiple field lookups
- Clear cache on resource version change

**3. Batch field operations**
- Build force patch and cooperative patch in parallel
- Only allocate Unstructured objects once
- Reuse field path extraction

**4. Short-circuit on blocked fields**
- Check for kro conflicts before building force patch
- Error early if any field is blocked
- Avoid unnecessary API calls

### Observability

**Structured logging**:
```go
log.Info("applying resource with shared ownership",
    "resource", resourceKey,
    "fieldsNeedingForce", fieldsNeedingForce,
    "fieldsNoForce", fieldsNoForce,
    "fieldsBlocked", fieldsBlocked)
```

**Metrics**:
```go
resource_apply_duration_seconds{ownership="shared", phase="classify|force|cooperative"}
resource_ownership_conflicts_total{type="kro|non-kro", resolved="true|false"}
resource_split_patches_total{force_fields="N", cooperative_fields="M"}
```

**Events on resource**:
```
Warning  OwnershipConflict  Field spec.replicas owned by another kro instance
Normal   NonKroEvicted      Evicted kubectl from fields: spec.strategy
```

### Backward Compatibility

**Default to Exclusive**: Existing RGDs without `ownership` field use current force=true behavior.

**No breaking changes**: Exclusive mode is byte-for-byte identical to current implementation.

**Opt-in migration**: Users explicitly change `ownership: Shared` when ready.

**Mixed mode support**: Some resources can be Shared while others are Exclusive in same RGD.

## Future Enhancements

### 1. Field Ownership Visualization

CLI command to show field ownership:
```bash
$ kro ownership show deployment/myapp
Field                          Owner
spec.replicas                  rgd-scaling-abc123
spec.template.metadata.labels  rgd-labeling-def456
spec.strategy                  rgd-deployment-ghi789
```

### 2. Ownership Transfer

Explicit API to transfer field ownership:
```yaml
spec:
  resources:
    - id: deployment
      ownership: Shared
      claimFields:
        - spec.replicas  # Take ownership from previous owner
```

### 3. Build-time Validation

Detect overlapping field declarations across RGDs at build time:
```
ERROR: Field conflict detected
  Field: spec.strategy
  Declared in: rgd-a, rgd-b
  Resolution: Remove from one RGD before applying
```

### 4. Ownership Policies

Fine-grained control over field ownership rules:
```yaml
spec:
  resources:
    - id: deployment
      ownership:
        mode: Shared
        evictNonKro: true      # default
        allowKroOverride: false # default (first-writer-wins)
        requireAnnotation: "kro.run/shared-resource" # safety check
```

### 5. Graceful Degradation

If split patch implementation proves too complex, fall back to simpler model:
- Document that Shared mode has eventual consistency
- Accept field theft as rare edge case
- Provide tooling to detect and resolve conflicts

## References

- [Kubernetes Server-Side Apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/)
- [SSA managedFields](https://kubernetes.io/docs/reference/using-api/server-side-apply/#field-management)
- [KEP-3659: kubectl apply --prune](https://github.com/kubernetes/enhancements/tree/master/keps/sig-cli/3659-kubectl-apply-prune)
- [kro ApplySet Package](../../pkg/controller/instance/applyset/)
- [Current kro SSA Implementation](../../pkg/controller/instance/applyset/applyset.go)
