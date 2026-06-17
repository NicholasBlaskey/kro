#!/bin/bash
set -x

echo "=== Step 1: Apply RGD ==="
kubectl apply -f test-collection-shrinkage.yaml

echo ""
echo "=== Step 2: Wait for RGD to be ready ==="
kubectl wait --for=condition=Available rgd/collection-shrink-bug --timeout=60s

echo ""
echo "=== Step 3: Create instance with count=5 ==="
kubectl apply -f test-shrink-instance-5.yaml

echo ""
echo "=== Step 4: Wait for instance to be ready ==="
sleep 10
kubectl get shrinktest test -o yaml

echo ""
echo "=== Step 5: Check pods - should see 5 pods ==="
kubectl get pods -l kro.run/graph=collection-shrink-bug
echo "Pod count: $(kubectl get pods -l kro.run/graph=collection-shrink-bug --no-headers | wc -l)"

echo ""
echo "=== Step 6: Update instance to count=3 ==="
kubectl apply -f test-shrink-instance-3.yaml

echo ""
echo "=== Step 7: Wait for reconciliation ==="
sleep 10

echo ""
echo "=== Step 8: Check pods - should see 3 pods but... ==="
kubectl get pods -l kro.run/graph=collection-shrink-bug
echo "Pod count: $(kubectl get pods -l kro.run/graph=collection-shrink-bug --no-headers | wc -l)"

echo ""
echo "=== BUG CHECK: Are pod-3 and pod-4 still there? ==="
if kubectl get pod pod-3 2>/dev/null && kubectl get pod pod-4 2>/dev/null; then
    echo "🐛 BUG CONFIRMED: pod-3 and pod-4 are ORPHANED!"
else
    echo "✅ No bug - pods were deleted correctly"
fi
