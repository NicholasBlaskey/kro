package test

import (
	"strings"
	"testing"
)

const testUserBugRGD = `apiVersion: kro.run/v1alpha1
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
    - id: service
      template:
        apiVersion: v1
        kind: Service
        metadata:
          name: test
          labels:
            app: myapp
    - id: config
      template:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: CURSOR_HERE
`

func TestUserBugCompletion(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	// User's exact expression: ${service.metadata.labels.app.replace('x').contains().contains().
	doc := strings.Replace(testUserBugRGD, "CURSOR_HERE", "${service.metadata.labels.app.replace('x').contains().contains().", 1)
	client.OpenDocument("file:///tmp/test-user-bug.yaml", doc)

	// Debug: find the line with our expression
	lines := strings.Split(doc, "\n")
	targetLine := -1
	for i, l := range lines {
		if strings.Contains(l, "replace('x').contains") {
			targetLine = i
			t.Logf("Found expression on line %d: %q", i, l)
			t.Logf("Length: %d characters", len(l))
			break
		}
	}
	if targetLine == -1 {
		t.Fatal("Could not find expression in document!")
	}

	line := targetLine
	col := len(lines[targetLine]) // At the end of the line (after the dot)
	items := client.RequestCompletion("file:///tmp/test-user-bug.yaml", line, col, 2, ".")

	t.Logf("Got %d completion items for user's expression", len(items))
	for _, item := range items {
		t.Logf("  - %s", item.Label)
	}

	// Should NOT have string methods (this is unknown or bool type)
	hasStringMethods := false
	for _, item := range items {
		if item.Label == "split" || item.Label == "replace" || item.Label == "substring" {
			t.Errorf("Should NOT have string method %q after bool.contains()!", item.Label)
			hasStringMethods = true
		}
	}

	if hasStringMethods {
		t.Errorf("FAIL: Showing string methods after bool expression!")
	}
}
