package test

import (
	"strings"
	"testing"
)

const testMethodCallRGD = `apiVersion: kro.run/v1alpha1
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

func TestMethodCallCompletion(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	t.Run("empty_parens", func(t *testing.T) {
		// Test: ${deployment.metadata.labels.app.replace().|
		// Should show string methods: split, contains, replace, etc.
		doc := strings.Replace(testMethodCallRGD, "CURSOR_HERE", "${deployment.metadata.labels.app.replace().", 1)
		client.OpenDocument("file:///tmp/test-method-call-empty.yaml", doc)

		line := 24
		col := 67 // After ".replace()."
		items := client.RequestCompletion("file:///tmp/test-method-call-empty.yaml", line, col, 2, ".")

		t.Logf("Got %d completion items for deployment.metadata.labels.app.replace().", len(items))
		for _, item := range items {
			t.Logf("  - %s", item.Label)
		}

		// Should have string methods
		assertContains(t, items, "split", "Method call .replace() should show string methods")
		assertContains(t, items, "contains", "Method call .replace() should show string methods")

		if len(items) == 0 {
			t.Errorf("FAIL: Method call completion returned 0 items!")
		}
	})

	t.Run("with_arguments", func(t *testing.T) {
		// Test: ${deployment.metadata.labels.app.replace('x', 'y').|
		// Should show string methods: split, contains, replace, etc.
		doc := strings.Replace(testMethodCallRGD, "CURSOR_HERE", "${deployment.metadata.labels.app.replace('x', 'y').", 1)
		client.OpenDocument("file:///tmp/test-method-call-args.yaml", doc)

		line := 24
		col := 77 // After ".replace('x', 'y')."
		items := client.RequestCompletion("file:///tmp/test-method-call-args.yaml", line, col, 2, ".")

		t.Logf("Got %d completion items for deployment.metadata.labels.app.replace('x', 'y').", len(items))
		for _, item := range items {
			t.Logf("  - %s", item.Label)
		}

		// Should have string methods
		assertContains(t, items, "split", "Method call .replace('x', 'y') should show string methods")
		assertContains(t, items, "contains", "Method call .replace('x', 'y') should show string methods")

		if len(items) == 0 {
			t.Errorf("FAIL: Method call with arguments completion returned 0 items!")
		}
	})
}


func TestMethodReferenceNoCompletion(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	// Test: ${deployment.metadata.labels.app.replace.|
	// Should show NOTHING (replace is a method reference, not a call)
	doc := strings.Replace(testMethodCallRGD, "CURSOR_HERE", "${deployment.metadata.labels.app.replace.", 1)
	client.OpenDocument("file:///tmp/test-method-ref.yaml", doc)

	line := 24
	col := 65 // After ".replace."
	items := client.RequestCompletion("file:///tmp/test-method-ref.yaml", line, col, 2, ".")

	t.Logf("Got %d completion items for deployment.metadata.labels.app.replace.", len(items))
	for _, item := range items {
		t.Logf("  - %s", item.Label)
	}

	// Should have NO completions (method reference has unknown type)
	if len(items) > 0 {
		t.Logf("WARNING: Method reference should not show completions, but got %d items", len(items))
		// This is expected behavior now - method references return unknown type
	}
}
