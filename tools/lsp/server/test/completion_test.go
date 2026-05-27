package test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type LSPClient struct {
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      *bufio.Reader
	stderr      io.ReadCloser
	t           *testing.T
	diagnostics map[string][]Diagnostic // URI -> diagnostics
	responses   chan *Response          // Channel for responses
	done        chan struct{}           // Channel to signal shutdown
}

type Request struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type Notification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ResponseError  `json:"error,omitempty"`
}

type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type CompletionItem struct {
	Label      string `json:"label"`
	Kind       int    `json:"kind"`
	SortText   string `json:"sortText,omitempty"`
	FilterText string `json:"filterText,omitempty"`
}

type Diagnostic struct {
	Range struct {
		Start struct {
			Line      uint32 `json:"line"`
			Character uint32 `json:"character"`
		} `json:"start"`
		End struct {
			Line      uint32 `json:"line"`
			Character uint32 `json:"character"`
		} `json:"end"`
	} `json:"range"`
	Severity *int   `json:"severity,omitempty"`
	Code     string `json:"code,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func NewLSPClient(t *testing.T) *LSPClient {
	cmd := exec.Command("/usr/local/bin/kro", "lsp", "server", "--offline")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("Failed to get stdin: %v", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to get stdout: %v", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("Failed to get stderr: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start LSP: %v", err)
	}

	client := &LSPClient{
		cmd:         cmd,
		stdin:       stdin,
		stdout:      bufio.NewReader(stdout),
		stderr:      stderr,
		t:           t,
		diagnostics: make(map[string][]Diagnostic),
		responses:   make(chan *Response, 100),
		done:        make(chan struct{}),
	}

	// Suppress stderr
	go func() {
		io.Copy(io.Discard, stderr)
	}()

	// Start background goroutine to read all messages
	go client.messageLoop()

	time.Sleep(300 * time.Millisecond)

	return client
}

func (c *LSPClient) Initialize() {
	c.SendRequest(1, "initialize", map[string]interface{}{
		"processId": os.Getpid(),
		"rootUri":   "file:///tmp",
		"capabilities": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"completion": map[string]interface{}{
					"contextSupport": true,
				},
			},
		},
	})
	c.ReadResponse()

	c.SendNotification("initialized", map[string]interface{}{})
}

func (c *LSPClient) OpenDocument(uri, content string) {
	c.SendNotification("textDocument/didOpen", map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri":        uri,
			"languageId": "yaml",
			"version":    1,
			"text":       content,
		},
	})
	time.Sleep(200 * time.Millisecond) // Wait for validation
}

func (c *LSPClient) RequestCompletion(uri string, line, char int, triggerKind int, triggerChar string) []CompletionItem {
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": uri,
		},
		"position": map[string]interface{}{
			"line":      line,
			"character": char,
		},
	}

	if triggerKind > 0 {
		context := map[string]interface{}{
			"triggerKind": triggerKind,
		}
		if triggerChar != "" {
			context["triggerCharacter"] = triggerChar
		}
		params["context"] = context
	}

	requestID := c.nextID()
	c.SendRequest(requestID, "textDocument/completion", params)

	// Read responses until we find the one matching our request ID
	// (there might be notifications in between)
	for i := 0; i < 10; i++ {
		resp := c.ReadResponse()
		if resp == nil {
			continue
		}

		// Skip notifications (ID == 0) and other requests
		if resp.ID != requestID {
			continue
		}

		// Found our response
		if resp.Result == nil {
			return nil
		}

		// Try to unmarshal as CompletionList first (new format)
		var completionList struct {
			IsIncomplete bool             `json:"isIncomplete"`
			Items        []CompletionItem `json:"items"`
		}
		if err := json.Unmarshal(resp.Result, &completionList); err == nil && completionList.Items != nil {
			return completionList.Items
		}

		// Fall back to array format (old format)
		var items []CompletionItem
		if err := json.Unmarshal(resp.Result, &items); err != nil {
			c.t.Logf("Failed to unmarshal completion result: %v", err)
			c.t.Logf("Raw result: %s", string(resp.Result))
			return nil
		}

		return items
	}

	return nil
}

type Location struct {
	URI   string `json:"uri"`
	Range struct {
		Start struct {
			Line      uint32 `json:"line"`
			Character uint32 `json:"character"`
		} `json:"start"`
		End struct {
			Line      uint32 `json:"line"`
			Character uint32 `json:"character"`
		} `json:"end"`
	} `json:"range"`
}

type DocumentHighlight struct {
	Range struct {
		Start struct {
			Line      uint32 `json:"line"`
			Character uint32 `json:"character"`
		} `json:"start"`
		End struct {
			Line      uint32 `json:"line"`
			Character uint32 `json:"character"`
		} `json:"end"`
	} `json:"range"`
	Kind *int `json:"kind,omitempty"`
}

func (c *LSPClient) RequestDocumentHighlight(uri string, line, char int) []DocumentHighlight {
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": uri,
		},
		"position": map[string]interface{}{
			"line":      line,
			"character": char,
		},
	}

	requestID := c.nextID()
	c.SendRequest(requestID, "textDocument/documentHighlight", params)

	// Read responses until we find the one matching our request ID
	for i := 0; i < 10; i++ {
		resp := c.ReadResponse()
		if resp == nil {
			continue
		}

		// Skip notifications and other requests
		if resp.ID != requestID {
			continue
		}

		// Found our response
		if resp.Result == nil {
			return nil
		}

		// Try to unmarshal as array of document highlights
		var highlights []DocumentHighlight
		if err := json.Unmarshal(resp.Result, &highlights); err != nil {
			c.t.Logf("Failed to unmarshal documentHighlight result: %v", err)
			return nil
		}

		return highlights
	}

	return nil
}

func (c *LSPClient) RequestDefinition(uri string, line, char int) []Location {
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": uri,
		},
		"position": map[string]interface{}{
			"line":      line,
			"character": char,
		},
	}

	requestID := c.nextID()
	c.SendRequest(requestID, "textDocument/definition", params)

	// Read responses until we find the one matching our request ID
	for i := 0; i < 10; i++ {
		resp := c.ReadResponse()
		if resp == nil {
			continue
		}

		// Skip notifications and other requests
		if resp.ID != requestID {
			continue
		}

		// Found our response
		if resp.Result == nil {
			return nil
		}

		// Try to unmarshal as array of locations
		var locations []Location
		if err := json.Unmarshal(resp.Result, &locations); err != nil {
			c.t.Logf("Failed to unmarshal definition result: %v", err)
			return nil
		}

		return locations
	}

	return nil
}

var requestID = 1

func (c *LSPClient) nextID() int {
	id := requestID
	requestID++
	return id
}

func (c *LSPClient) SendRequest(id int, method string, params interface{}) {
	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, _ := json.Marshal(req)
	msg := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(data), data)
	c.stdin.Write([]byte(msg))
}

func (c *LSPClient) SendNotification(method string, params interface{}) {
	notif := Notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	data, _ := json.Marshal(notif)
	msg := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(data), data)
	c.stdin.Write([]byte(msg))
}

// messageLoop runs in background to read all LSP messages
// It routes responses to the responses channel and handles notifications (like diagnostics)
func (c *LSPClient) messageLoop() {
	defer close(c.responses)

	for {
		select {
		case <-c.done:
			return
		default:
		}

		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return
		}

		var length int
		n, _ := fmt.Sscanf(line, "Content-Length: %d", &length)
		if n != 1 {
			continue
		}

		c.stdout.ReadString('\n') // Empty line

		content := make([]byte, length)
		_, err = io.ReadFull(c.stdout, content)
		if err != nil {
			return
		}

		// Try to parse as notification first
		var notif struct {
			JSONRPC string          `json:"jsonrpc"`
			Method  string          `json:"method,omitempty"`
			ID      int             `json:"id,omitempty"`
			Params  json.RawMessage `json:"params,omitempty"`
		}

		if err := json.Unmarshal(content, &notif); err != nil {
			continue
		}

		// If it has Method but no ID, it's a notification
		if notif.Method != "" && notif.ID == 0 {
			// Handle diagnostics notification
			if notif.Method == "textDocument/publishDiagnostics" {
				var params PublishDiagnosticsParams
				if err := json.Unmarshal(notif.Params, &params); err == nil {
					c.diagnostics[params.URI] = params.Diagnostics
				}
			}
		} else {
			// It's a response - parse and send to channel
			var resp Response
			if err := json.Unmarshal(content, &resp); err == nil {
				c.responses <- &resp
			}
		}
	}
}

func (c *LSPClient) ReadResponse() *Response {
	select {
	case resp := <-c.responses:
		return resp
	case <-time.After(2 * time.Second):
		return nil
	}
}

// GetDiagnostics returns all diagnostics for all documents
func (c *LSPClient) GetDiagnostics() []Diagnostic {
	var all []Diagnostic
	for _, diags := range c.diagnostics {
		all = append(all, diags...)
	}
	return all
}

// WaitForDiagnostics waits for diagnostics on a specific URI and returns them
func (c *LSPClient) WaitForDiagnostics(uri string, expectedCount int) []Diagnostic {
	// Wait up to 2 seconds
	for i := 0; i < 40; i++ {
		time.Sleep(50 * time.Millisecond)
		if diags, ok := c.diagnostics[uri]; ok {
			if expectedCount == 0 || len(diags) == expectedCount {
				return diags
			}
		}
	}
	// Return whatever we have
	return c.diagnostics[uri]
}

func (c *LSPClient) Close() {
	close(c.done)
	c.SendRequest(999, "shutdown", nil)
	time.Sleep(100 * time.Millisecond)
	c.cmd.Process.Kill()
}

// Test helpers

func assertContains(t *testing.T, items []CompletionItem, label string, msg string) {
	for _, item := range items {
		if item.Label == label {
			return
		}
	}
	t.Errorf("%s: expected to find '%s' in completions, got: %v", msg, label, itemLabels(items))
}

func assertFirst(t *testing.T, items []CompletionItem, label string, msg string) {
	if len(items) == 0 {
		t.Errorf("%s: expected first item to be '%s', but got 0 items", msg, label)
		return
	}
	if items[0].Label != label {
		t.Errorf("%s: expected first item to be '%s', got '%s' (all: %v)", msg, label, items[0].Label, itemLabels(items))
	}
}

func assertCount(t *testing.T, items []CompletionItem, expected int, msg string) {
	if len(items) != expected {
		t.Errorf("%s: expected %d items, got %d: %v", msg, expected, len(items), itemLabels(items))
	}
}

func itemLabels(items []CompletionItem) []string {
	labels := make([]string, len(items))
	for i, item := range items {
		labels[i] = item.Label
	}
	return labels
}

// TESTS START HERE

const testRGD = `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test
spec:
  schema:
    kind: TestApp
    apiVersion: v1alpha1
    spec:
      name: string
      replicas: integer
  resources:
    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: CURSOR_HERE
    - id: service
      template:
        apiVersion: v1
        kind: Service
        metadata:
          name: service-name`

func TestCompletion_ManualTrigger(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	// Open document with cursor at various positions
	doc := strings.Replace(testRGD, "CURSOR_HERE", "${sch", 1)
	client.OpenDocument("file:///tmp/test.yaml", doc)

	// Manual trigger (Ctrl+Space) - triggerKind: 1
	// Line 17 is the "name: ${sch" line (0-indexed)
	// Position 22 is right after "sch"
	items := client.RequestCompletion("file:///tmp/test.yaml", 17, 22, 1, "")

	// When typing "${sch", should only show items starting with "sch"
	assertContains(t, items, "schema", "Manual trigger should include 'schema'")
	assertFirst(t, items, "schema", "Manual trigger should have 'schema' first (sortText)")
	assertCount(t, items, 1, "Manual trigger with prefix 'sch' should have 1 item")
}

func TestCompletion_TriggerOnDollar(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	// Simulate typing '$' - position right after the $
	doc := strings.Replace(testRGD, "CURSOR_HERE", "$", 1)
	client.OpenDocument("file:///tmp/test.yaml", doc)

	// Trigger character '$' - triggerKind: 2
	// Line 17 (0-indexed), column 15 is right after the $
	items := client.RequestCompletion("file:///tmp/test.yaml", 17, 15, 2, "$")

	// Should return completions even though only $ is typed
	if len(items) == 0 {
		t.Errorf("Typing '$' should trigger completions, got 0 items")
	}
}

func TestCompletion_TriggerOnOpenBrace(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	// Simulate typing '${' - position right after the {
	doc := strings.Replace(testRGD, "CURSOR_HERE", "${", 1)
	client.OpenDocument("file:///tmp/test.yaml", doc)

	// Trigger character '{' - triggerKind: 2
	// Line 17 (0-indexed), column 16 is right after the {
	items := client.RequestCompletion("file:///tmp/test.yaml", 17, 16, 2, "{")

	// With no prefix, should show all resources + special identifiers + CEL functions
	assertContains(t, items, "schema", "Typing '${' should show all completions")
	assertContains(t, items, "deployment", "Typing '${' should show all completions")
	assertContains(t, items, "service", "Typing '${' should show all completions")
	assertContains(t, items, "self", "Typing '${' should show all completions")
	assertContains(t, items, "hash", "Typing '${' should show CEL functions")
	assertContains(t, items, "omit", "Typing '${' should show CEL functions")

	// Kro functions should be first (sortText: "0_hash", "0_omit")
	// Then schema/self (sortText: "0_schema", "0_self")
	// Then resources (sortText: "1_deployment", etc.)
	if len(items) > 0 && items[0].Label != "hash" && items[0].Label != "omit" {
		t.Logf("First item is '%s', expected 'hash' or 'omit' (Kro functions have highest priority)", items[0].Label)
	}
}

func TestCompletion_TriggerOnDot(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	// Simulate typing '${schema.' - position right after the dot
	doc := strings.Replace(testRGD, "CURSOR_HERE", "${schema.", 1)
	client.OpenDocument("file:///tmp/test.yaml", doc)

	// Trigger character '.' - triggerKind: 2
	// Line 17 (0-indexed), column 24 is right after the dot
	items := client.RequestCompletion("file:///tmp/test.yaml", 17, 24, 2, ".")

	assertContains(t, items, "spec", "Typing '.' after schema should show 'spec'")

	// spec should be the main suggestion
	if len(items) > 0 {
		found := false
		for _, item := range items {
			if item.Label == "spec" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected 'spec' in completions after 'schema.', got: %v", itemLabels(items))
		}
	}
}

func TestCompletion_PartialTyping(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	// Type '${s' - should filter to items starting with 's'
	doc := strings.Replace(testRGD, "CURSOR_HERE", "${s", 1)
	client.OpenDocument("file:///tmp/test.yaml", doc)

	// Line 17 (0-indexed), column 19 is right after the 's'
	items := client.RequestCompletion("file:///tmp/test.yaml", 17, 19, 1, "")

	assertContains(t, items, "schema", "Partial 's' should include 'schema'")
	assertContains(t, items, "service", "Partial 's' should include 'service'")
	assertContains(t, items, "self", "Partial 's' should include 'self'")

	// Should NOT contain 'deployment' (doesn't start with 's')
	for _, item := range items {
		if item.Label == "deployment" {
			t.Errorf("Partial 's' should NOT include 'deployment'")
		}
	}
}

func TestCompletion_EmptyDocument(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	client.OpenDocument("file:///tmp/empty.yaml", "")

	items := client.RequestCompletion("file:///tmp/empty.yaml", 0, 0, 1, "")

	// Should return empty or YAML keys, not crash
	// This is just a safety test
	t.Logf("Empty document returned %d items", len(items))
}

func TestCompletion_NonRGD(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	pod := `apiVersion: v1
kind: Pod
metadata:
  name: test`

	client.OpenDocument("file:///tmp/pod.yaml", pod)

	items := client.RequestCompletion("file:///tmp/pod.yaml", 3, 10, 1, "")

	// Should handle non-RGD gracefully
	t.Logf("Non-RGD document returned %d items", len(items))
}

func TestDiagnostics_SymbolTableBuilds(t *testing.T) {
	client := NewLSPClient(t)
	defer client.Close()

	client.Initialize()

	// Open a valid RGD and check that symbol table builds
	doc := strings.Replace(testRGD, "CURSOR_HERE", "${", 1)
	client.OpenDocument("file:///tmp/test.yaml", doc)

	// Wait for validation to complete
	time.Sleep(500 * time.Millisecond)

	// Try completion at ${| - should get all resource IDs
	// Line 17 (0-indexed), column 16 is right after ${
	items := client.RequestCompletion("file:///tmp/test.yaml", 17, 16, 1, "")

	t.Logf("After validation, got %d completion items", len(items))

	// Should have schema, self, deployment, service (4 items)
	if len(items) < 4 {
		t.Errorf("Expected at least 4 completion items (symbol table built), got %d", len(items))
	}

	// Verify key items are present
	assertContains(t, items, "schema", "Symbol table should include schema")
	assertContains(t, items, "deployment", "Symbol table should include deployment")
	assertContains(t, items, "service", "Symbol table should include service")
}
