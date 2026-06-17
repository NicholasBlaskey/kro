# Instance Condition Metrics: Architecture Analysis

## Current Implementation Issues

### 1. Delete/Recreate Anti-Pattern
The current implementation uses `GaugeVec` with `DeletePartialMatch()` and recreates metrics on every reconcile. This is problematic:

```go
// Current approach - deletes and recreates gauges
InstanceConditionCurrentStatusSeconds.WithLabelValues(...).Set(duration)
InstanceConditionCurrentStatusSeconds.DeletePartialMatch(...)
```

**Problems:**
- Prometheus scrapers may see inconsistent data during delete/recreate
- High reconciliation rates cause metric churn
- Goes against Prometheus best practices of stable metric identities

### 2. High Cardinality Risk
With 6 labels: `gvr`, `namespace`, `name`, `condition_type`, `condition_status`, `reason`

**Example scale:**
- 10 RGDs × 100 instances each = 1,000 instances
- 4-6 conditions per instance
- 2-3 possible statuses (True/False/Unknown)
- **Total: 8,000-18,000 metric series**

### 3. Stale Data Problem
Current gauge values depend on reconciliation frequency:
- Default resync: ~10 hours → stale metrics
- Requires tuning `--dynamic-controller-default-resync-period`

---

## cert-manager Pattern: Custom Collector

cert-manager solves this with a **custom collector** that computes metrics at scrape time:

```go
type CertificateCollector struct {
    certificatesLister cmlisters.CertificateLister
    certificateReadyStatusMetric *prometheus.Desc
}

func (cc *CertificateCollector) Collect(ch chan<- prometheus.Metric) {
    certsList, err := cc.certificatesLister.List(labels.Everything())
    for _, cert := range certsList {
        // Compute metric value from current state
        metric := prometheus.MustNewConstMetric(
            cc.certificateReadyStatusMetric,
            prometheus.GaugeValue,
            value,
            cert.Name, cert.Namespace, ...
        )
        ch <- metric
    }
}
```

**Key differences:**
1. **No persistent state** - metrics computed fresh on each scrape
2. **No delete/recreate** - ConstMetric is ephemeral
3. **Always fresh** - values reflect current cluster state, not stale reconcile state
4. **Simpler** - no cleanup logic needed

---

## Recommended Architecture

### Option 1: Custom Collector (RECOMMENDED)
**Pros:**
- Always fresh data (computed at scrape time)
- No delete/recreate churn
- Follows Prometheus best practices
- Matches cert-manager pattern

**Cons:**
- ~150 LOC implementation
- Requires access to instance lister

**Implementation sketch:**
```go
type InstanceConditionCollector struct {
    gvr schema.GroupVersionResource
    instanceLister dynamic.NamespaceableResourceInterface
    conditionDurationDesc *prometheus.Desc
}

func (c *InstanceConditionCollector) Collect(ch chan<- prometheus.Metric) {
    instances, _ := c.instanceLister.List(context.Background(), metav1.ListOptions{})
    for _, inst := range instances.Items {
        conditions := extractConditions(inst)
        for _, cond := range conditions {
            duration := time.Since(cond.LastTransitionTime.Time).Seconds()
            ch <- prometheus.MustNewConstMetric(
                c.conditionDurationDesc,
                prometheus.GaugeValue,
                duration,
                c.gvr.String(), inst.GetNamespace(), inst.GetName(),
                string(cond.Type), string(cond.Status), cond.Reason,
            )
        }
    }
}
```

### Option 2: Emit Timestamp Instead of Duration
Emit `kro_instance_condition_last_transition_timestamp_seconds` (Unix timestamp):

```promql
# Query to get duration in PromQL
time() - kro_instance_condition_last_transition_timestamp_seconds
```

**Pros:**
- Simpler controller code
- PromQL computes duration at query time (always fresh)
- No need for custom collector
- No delete/recreate needed (timestamp is stable)

**Cons:**
- Requires users to write PromQL
- Less intuitive than direct duration gauge

**Implementation:**
```go
// Emit once per transition, never delete
InstanceConditionLastTransitionTimestamp.WithLabelValues(
    gvr, namespace, name, conditionType, status, reason,
).Set(float64(cond.LastTransitionTime.Unix()))
```

### Option 3: Keep Current with Improvements
**If staying with GaugeVec:**
1. Only emit metrics on actual state changes (not every reconcile)
2. Reduce cardinality by removing `reason` label
3. Document cardinality expectations clearly
4. Add cardinality monitoring

---

## Comparison Table

| Approach | Freshness | Cardinality | Complexity | Prometheus Pattern |
|----------|-----------|-------------|------------|--------------------|
| Current (GaugeVec) | Stale (resync-dependent) | High (6 labels) | Medium | ❌ Anti-pattern |
| Custom Collector | Fresh (scrape-time) | High (6 labels) | High (~150 LOC) | ✅ Best practice |
| Timestamp Gauge | Fresh (PromQL) | High (6 labels) | Low | ✅ Good |
| Status Gauge (cert-manager style) | Fresh (scrape-time) | Medium (no duration) | Medium | ✅ Best practice |

---

## Recommendation for Alpha Feature

Given this is an **Alpha feature** in an **Alpha project**:

### Short-term (v0.9.2)
**Option: Emit timestamp instead of duration**

Why:
- Minimal code change from current PR
- Fixes the staleness issue
- No delete/recreate needed
- Users can compute duration in PromQL

```go
// Change from:
InstanceConditionCurrentStatusSeconds.WithLabelValues(...).Set(duration)

// To:
InstanceConditionLastTransitionTimestamp.WithLabelValues(...).Set(timestamp)
```

### Long-term (v0.10.0+)
**Option: Custom collector**

Why:
- Proper Prometheus pattern
- Scales better
- Can emit both duration AND timestamp
- Future-proof architecture

---

## Addressing Specific Concerns

### Nick's Concerns
> "It's kinda weird how we are deleting and recreating metrics"

**✅ Valid** - This is a Prometheus anti-pattern. Metrics should have stable identities.

> "Maybe we want to do something like cert-manager with custom collector"

**✅ Correct** - Custom collector is the proper solution for computed-at-scrape metrics.

### Amine's Question
> "Are you saying the metric we should emit should be completely different?"

**Suggestion:** Keep the same labels/dimensions, but emit `last_transition_timestamp_seconds` instead of `current_status_seconds`. This gives users more flexibility and fixes staleness.

### Michael's Point
> "The solution is around 150 LOC. Isn't something we should go forward with for an alpha feature"

**Counter:** 150 LOC is reasonable for a proper foundation. But if time-constrained, the timestamp approach is simpler and still correct.

---

## Code Diff for Timestamp Approach

```diff
- InstanceConditionCurrentStatusSeconds = prometheus.NewGaugeVec(
+ InstanceConditionLastTransitionTimestamp = prometheus.NewGaugeVec(
    prometheus.GaugeOpts{
-       Name: "instance_condition_current_status_seconds",
-       Help: "The current amount of time in seconds that an instance status condition has been in a specific state.",
+       Name: "instance_condition_last_transition_timestamp_seconds",  
+       Help: "Unix timestamp of the last transition time for this condition state.",
    },
    []string{labelGVR, labelNamespace, labelName, labelConditionType, labelConditionStatus, labelReason},
)

func EmitConditionMetrics(...) {
-   var durationSeconds float64
-   if cond.LastTransitionTime != nil {
-       durationSeconds = time.Since(cond.LastTransitionTime.Time).Seconds()
-   }
-   InstanceConditionCurrentStatusSeconds.WithLabelValues(...).Set(durationSeconds)
+   var timestamp float64
+   if cond.LastTransitionTime != nil {
+       timestamp = float64(cond.LastTransitionTime.Unix())
+   }
+   InstanceConditionLastTransitionTimestamp.WithLabelValues(...).Set(timestamp)
}
```

**No cleanup needed** - timestamps are stable, only update on actual transitions.

---

## Query Examples

### With Current Duration Gauge
```promql
# Alert on stuck conditions
kro_instance_condition_current_status_seconds{condition_status="False"} > 3600
```

### With Timestamp Gauge
```promql
# Same alert, computed fresh
(time() - kro_instance_condition_last_transition_timestamp_seconds{condition_status="False"}) > 3600

# Works even with stale scrapes - timestamp is absolute
```

---

## Conclusion

**For unblocking v0.9.2:** Switch to timestamp-based metrics (small change, big improvement)

**For proper long-term solution:** Implement custom collector pattern in v0.10.0

Both approaches are used in production Kubernetes projects and follow Prometheus best practices.
