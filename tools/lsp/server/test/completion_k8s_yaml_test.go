package test

import (
	"strings"
	"testing"
)

// TestCompletion_K8sYAMLFields_ArrayElement tests completion for new array elements
// When user types "- " in a list like ports:, should suggest port element fields
func TestCompletion_K8sYAMLFields_ArrayElement(t *testing.T) {
	tests := []struct {
		name         string
		document     string
		line         int
		char         int
		triggerChar  string
		wantContains []string
		description  string
	}{
		{
			name: "new port element after dash and space",
			document: `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test
spec:
  schema:
    apiVersion: v1alpha1
    kind: Test
    spec:
      name: string
  resources:
    - id: service
      template:
        apiVersion: v1
        kind: Service
        spec:
          ports:
            - `,
			line:         17,
			char:         14, // After "- "
			triggerChar:  " ",
			wantContains: []string{"name", "protocol", "port", "targetPort"},
			description:  "After '- ' in ports list, should suggest port element fields",
		},
		{
			name: "new container element with existing siblings",
			document: `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test
spec:
  schema:
    apiVersion: v1alpha1
    kind: Test
    spec:
      name: string
  resources:
    - id: pod
      template:
        apiVersion: v1
        kind: Pod
        spec:
          containers:
            - name: nginx
              image: nginx
            - `,
			line:         19,
			char:         14, // After "- " on new element
			triggerChar:  " ",
			wantContains: []string{"name", "image"},
			description:  "After '- ' for new container, should suggest container fields",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()
			client.Initialize()

			uri := "file:///tmp/test-array-" + tt.name + ".yaml"
			client.OpenDocument(uri, tt.document)

			items := client.RequestCompletion(uri, tt.line, tt.char, 2, tt.triggerChar)

			labels := make([]string, len(items))
			for i, item := range items {
				labels[i] = item.Label
			}

			for _, want := range tt.wantContains {
				found := false
				for _, label := range labels {
					if label == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s: expected %q in completions, got: %v", tt.description, want, labels)
				}
			}
		})
	}
}

// TestCompletion_K8sYAMLFields_NewlineTrigger tests that completions fire after pressing Enter
// (newline character creates a new indented line, triggering completion)
func TestCompletion_K8sYAMLFields_NewlineTrigger(t *testing.T) {
	doc := `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test
spec:
  schema:
    apiVersion: v1alpha1
    kind: Test
    spec:
      name: string
  resources:
    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        spec:
          replicas: 1
          `

	client := NewLSPClient(t)
	defer client.Close()
	client.Initialize()

	uri := "file:///tmp/test-newline.yaml"
	client.OpenDocument(uri, doc)

	// After pressing Enter, cursor is on a new indented line
	// Line 17 (0-indexed) is the empty indented line
	items := client.RequestCompletion(uri, 17, 10, 2, "\n")

	if len(items) == 0 {
		t.Fatal("Expected completions after newline trigger, got none")
	}

	labels := make([]string, len(items))
	for i, item := range items {
		labels[i] = item.Label
	}

	wantContains := []string{"selector", "template"}
	for _, want := range wantContains {
		found := false
		for _, label := range labels {
			if label == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Newline trigger should return %q, got: %v", want, labels)
		}
	}
}

// TestCompletion_K8sYAMLFields_SpaceTrigger tests that completions fire when user types space
// (the space character before a YAML key) and that subsequent letter typing returns filtered results.
// This simulates the actual user flow: indenting via spaces, then typing the key name.
func TestCompletion_K8sYAMLFields_SpaceTrigger(t *testing.T) {
	// Document with cursor at end of indented line (10 spaces) under spec:
	// The user has just finished indenting and is about to type a key
	doc := `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test
spec:
  schema:
    apiVersion: v1alpha1
    kind: Test
    spec:
      name: string
  resources:
    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        spec:
          `

	client := NewLSPClient(t)
	defer client.Close()
	client.Initialize()

	uri := "file:///tmp/test-space-trigger.yaml"
	client.OpenDocument(uri, doc)

	// Line 16 (0-indexed) is the empty indented line under spec:
	// Char 10 is right after the 10 spaces of indentation
	// Simulate typing a space (triggerKind=2 = TriggerCharacter)
	items := client.RequestCompletion(uri, 16, 10, 2, " ")

	if len(items) == 0 {
		t.Fatal("Expected completions when triggering with space, got none. " +
			"This is the critical test - if space trigger doesn't work, YAML completion won't auto-popup.")
	}

	labels := make([]string, len(items))
	for i, item := range items {
		labels[i] = item.Label
	}

	// Should suggest Deployment spec fields
	wantContains := []string{"replicas", "selector", "template"}
	for _, want := range wantContains {
		found := false
		for _, label := range labels {
			if label == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Space trigger should return %q, got: %v", want, labels)
		}
	}
}

// TestCompletion_K8sYAMLFields_TypedPrefix tests that typing a partial word triggers completions
// This simulates user typing 'r' to get 'replicas'
func TestCompletion_K8sYAMLFields_TypedPrefix(t *testing.T) {
	doc := `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test
spec:
  schema:
    apiVersion: v1alpha1
    kind: Test
    spec:
      name: string
  resources:
    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        spec:
          r`

	client := NewLSPClient(t)
	defer client.Close()
	client.Initialize()

	uri := "file:///tmp/test-k8s-typed.yaml"
	client.OpenDocument(uri, doc)

	// Cursor is right after 'r' on line 16 (0-indexed)
	// Line 16: "          r"
	// Char 11 is right after the 'r'
	items := client.RequestCompletion(uri, 16, 11, 2, "r")

	labels := make([]string, len(items))
	for i, item := range items {
		labels[i] = item.Label
	}

	// Should suggest fields starting with 'r' like 'replicas', 'revisionHistoryLimit'
	wantOne := []string{"replicas", "revisionHistoryLimit"}
	foundAny := false
	for _, want := range wantOne {
		for _, label := range labels {
			if label == want {
				foundAny = true
				break
			}
		}
	}
	if !foundAny {
		t.Errorf("Expected at least one of %v, got: %v", wantOne, labels)
	}
}

// TestCompletion_K8sYAMLFields tests autocomplete for K8s resource fields in YAML (not CEL)
func TestCompletion_K8sYAMLFields(t *testing.T) {
	tests := []struct {
		name          string
		document      string
		line          int
		char          int
		wantContains  []string
		wantExcludes  []string
		description   string
	}{
		{
			name: "deployment spec fields",
			document: `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test
spec:
  schema:
    apiVersion: v1alpha1
    kind: Test
    spec:
      name: string
  resources:
    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: test
        spec:
          CURSOR_HERE`,
			line:         18,
			char:         10,
			wantContains: []string{"replicas", "selector", "template", "strategy"},
			wantExcludes: []string{"metadata", "status"}, // These are top-level, not spec fields
			description:  "Should suggest Deployment spec fields",
		},
		{
			name: "deployment spec.template fields",
			document: `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test
spec:
  schema:
    apiVersion: v1alpha1
    kind: Test
    spec:
      name: string
  resources:
    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        spec:
          replicas: 1
          template:
            CURSOR_HERE`,
			line:         18, // The line where CURSOR_HERE is (0-indexed)
			char:         12,
			wantContains: []string{"metadata", "spec"},
			description:  "Should suggest PodTemplate fields",
		},
		{
			name: "service spec fields",
			document: `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test
spec:
  schema:
    apiVersion: v1alpha1
    kind: Test
    spec:
      name: string
  resources:
    - id: service
      template:
        apiVersion: v1
        kind: Service
        spec:
          CURSOR_HERE`,
			line:         16,
			char:         10,
			wantContains: []string{"type", "selector", "ports", "clusterIP"},
			description:  "Should suggest Service spec fields",
		},
		{
			name: "service spec.ports fields",
			document: `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test
spec:
  schema:
    apiVersion: v1alpha1
    kind: Test
    spec:
      name: string
  resources:
    - id: service
      template:
        apiVersion: v1
        kind: Service
        spec:
          ports:
            - CURSOR_HERE`,
			line:         17,
			char:         14,
			wantContains: []string{"name", "protocol", "port", "targetPort"},
			description:  "Should suggest Service port fields",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(tt.document, "CURSOR_HERE", "", 1)
			uri := "file:///tmp/test-k8s-yaml.yaml"
			client.OpenDocument(uri, doc)

			// Request completions (triggerKind: 1 = Invoked, triggerChar: "")
			items := client.RequestCompletion(uri, tt.line, tt.char, 1, "")

			// Extract labels for easier checking
			labels := make([]string, len(items))
			for i, item := range items {
				labels[i] = item.Label
			}

			// Check that wanted items are present
			for _, want := range tt.wantContains {
				found := false
				for _, label := range labels {
					if label == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s: expected completion %q not found. Got: %v", tt.description, want, labels)
				}
			}

			// Check that excluded items are NOT present
			for _, exclude := range tt.wantExcludes {
				for _, label := range labels {
					if label == exclude {
						t.Errorf("%s: excluded completion %q was found. Got: %v", tt.description, exclude, labels)
					}
				}
			}
		})
	}
}
