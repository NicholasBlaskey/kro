package test

import (
	"strings"
	"testing"
)

const testBoolMethodRGD = `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test
spec:
  schema:
    kind: TestApp
    apiVersion: v1alpha1
    spec:
      name: string
  resources:
    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: test
          labels:
            app: myapp
    - id: service
      template:
        apiVersion: v1
        kind: Service
        metadata:
          name: CURSOR_HERE
`

func TestBoolMethodCompletion(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	t.Run("with_arguments", func(t *testing.T) {
		// Test: ${deployment.metadata.labels.app.contains('x').|
		// contains() returns bool, so should show NO string methods
		doc := strings.Replace(testBoolMethodRGD, "CURSOR_HERE", "${deployment.metadata.labels.app.contains('x').", 1)
		client.OpenDocument("file:///tmp/test-bool-method-args.yaml", doc)

		line := 24
		col := 75 // After ".contains('x')."
		items := client.RequestCompletion("file:///tmp/test-bool-method-args.yaml", line, col, 2, ".")

		t.Logf("Got %d completion items for contains('x')", len(items))
		for _, item := range items {
			t.Logf("  - %s", item.Label)
		}

		// Should NOT have string methods
		for _, item := range items {
			if item.Label == "split" || item.Label == "replace" || item.Label == "contains" {
				t.Errorf("Bool type should NOT have string method %q!", item.Label)
			}
		}
	})

	t.Run("empty_parens", func(t *testing.T) {
		// Test: ${deployment.metadata.labels.app.contains().|
		// contains() returns bool, so should show NO string methods
		doc := strings.Replace(testBoolMethodRGD, "CURSOR_HERE", "${deployment.metadata.labels.app.contains().", 1)
		client.OpenDocument("file:///tmp/test-bool-method-empty.yaml", doc)

		line := 24
		col := 67 // After ".contains()."
		items := client.RequestCompletion("file:///tmp/test-bool-method-empty.yaml", line, col, 2, ".")

		t.Logf("Got %d completion items for contains()", len(items))
		for _, item := range items {
			t.Logf("  - %s", item.Label)
		}

		// Should NOT have string methods
		for _, item := range items {
			if item.Label == "split" || item.Label == "replace" || item.Label == "contains" {
				t.Errorf("Bool type should NOT have string method %q!", item.Label)
			}
		}
	})
}
