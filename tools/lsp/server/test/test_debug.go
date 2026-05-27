package test

import (
	"strings"
	"testing"
	"time"
)

func TestCompletion_DebugResourceDepth(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	// Test: deployment.spec.
	doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", "${deployment.spec.", 1)
	client.OpenDocument("file:///tmp/test-debug.yaml", doc)
	time.Sleep(300 * time.Millisecond)

	items := client.RequestCompletion("file:///tmp/test-debug.yaml", 18, 33, 1, "")
	
	t.Logf("For expression '${deployment.spec.' got %d items: %v", len(items), itemLabels(items))
	t.Logf("Expected: 0 items (no K8s schema)")
}
