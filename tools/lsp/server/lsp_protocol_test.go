// Copyright 2025 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

// LSPMessage represents a generic LSP JSON-RPC message
type LSPMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

// LSPClient manages communication with an LSP server process
type LSPClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	t      *testing.T
}

// NewLSPClient starts an LSP server and returns a client
func NewLSPClient(t *testing.T, binary string, args ...string) *LSPClient {
	cmd := exec.Command(binary, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("Failed to get stdin: %v", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to get stdout: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	time.Sleep(500 * time.Millisecond) // Give server time to start

	return &LSPClient{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		t:      t,
	}
}

// Close terminates the LSP server
func (c *LSPClient) Close() {
	c.stdin.Close()
	c.cmd.Process.Kill()
	c.cmd.Wait()
}

// SendMessage sends an LSP message to the server
func (c *LSPClient) SendMessage(msg interface{}) {
	content, err := json.Marshal(msg)
	if err != nil {
		c.t.Fatalf("Failed to marshal message: %v", err)
	}

	message := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(content), content)
	if _, err := c.stdin.Write([]byte(message)); err != nil {
		c.t.Fatalf("Failed to write message: %v", err)
	}
}

// ReadMessage reads a single LSP message from the server
func (c *LSPClient) ReadMessage(timeout time.Duration) *LSPMessage {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		// Read headers
		headers := make(map[string]string)
		for {
			line, err := c.stdout.ReadString('\n')
			if err != nil {
				time.Sleep(50 * time.Millisecond)
				continue
			}

			line = strings.TrimSpace(line)
			if line == "" {
				break // End of headers
			}

			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}

		// Read content
		if lengthStr, ok := headers["Content-Length"]; ok {
			var length int
			fmt.Sscanf(lengthStr, "%d", &length)

			content := make([]byte, length)
			if _, err := io.ReadFull(c.stdout, content); err != nil {
				c.t.Fatalf("Failed to read content: %v", err)
			}

			var msg LSPMessage
			if err := json.Unmarshal(content, &msg); err != nil {
				c.t.Fatalf("Failed to unmarshal message: %v", err)
			}

			return &msg
		}
	}

	return nil
}

// ReadMessageByID reads messages until it finds one with the target ID
func (c *LSPClient) ReadMessageByID(id int, timeout time.Duration) *LSPMessage {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		msg := c.ReadMessage(timeout)
		if msg == nil {
			return nil
		}

		if msg.ID != nil && *msg.ID == id {
			return msg
		}
	}

	return nil
}

func TestLSPProtocol(t *testing.T) {
	client := NewLSPClient(t, "/usr/local/bin/kro", "lsp", "server", "--offline")
	defer client.Close()

	// Initialize
	t.Run("Initialize", func(t *testing.T) {
		id := 1
		client.SendMessage(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  "initialize",
			"params": map[string]interface{}{
				"processId": nil,
				"rootUri":   "file:///tmp",
				"capabilities": map[string]interface{}{},
			},
		})

		resp := client.ReadMessageByID(id, 3*time.Second)
		if resp == nil {
			t.Fatal("No initialize response")
		}

		var result protocol.InitializeResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			t.Fatalf("Failed to unmarshal result: %v", err)
		}

		if result.Capabilities.HoverProvider == nil {
			t.Error("HoverProvider not advertised")
		}
		if result.Capabilities.DefinitionProvider == nil {
			t.Error("DefinitionProvider not advertised")
		}
		if result.Capabilities.CompletionProvider == nil {
			t.Error("CompletionProvider not advertised")
		}
	})

	// Initialized notification
	client.SendMessage(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]interface{}{},
	})
	time.Sleep(300 * time.Millisecond)

	// Open document
	client.SendMessage(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":        "file:///tmp/test.yaml",
				"languageId": "yaml",
				"version":    1,
				"text": `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test
spec:
  schema:
    kind: TestApp
    apiVersion: v1alpha1
  resources:
    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment`,
			},
		},
	})
	time.Sleep(2 * time.Second) // Wait for validation

	// Hover
	t.Run("Hover", func(t *testing.T) {
		id := 2
		client.SendMessage(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  "textDocument/hover",
			"params": map[string]interface{}{
				"textDocument": map[string]interface{}{"uri": "file:///tmp/test.yaml"},
				"position":     map[string]interface{}{"line": 8, "character": 10},
			},
		})

		resp := client.ReadMessageByID(id, 3*time.Second)
		if resp == nil {
			t.Fatal("No hover response")
		}
	})

	// Definition
	t.Run("Definition", func(t *testing.T) {
		id := 3
		client.SendMessage(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  "textDocument/definition",
			"params": map[string]interface{}{
				"textDocument": map[string]interface{}{"uri": "file:///tmp/test.yaml"},
				"position":     map[string]interface{}{"line": 8, "character": 10},
			},
		})

		resp := client.ReadMessageByID(id, 3*time.Second)
		if resp == nil {
			t.Fatal("No definition response")
		}
	})

	// Completion
	t.Run("Completion", func(t *testing.T) {
		id := 4
		client.SendMessage(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  "textDocument/completion",
			"params": map[string]interface{}{
				"textDocument": map[string]interface{}{"uri": "file:///tmp/test.yaml"},
				"position":     map[string]interface{}{"line": 8, "character": 12},
			},
		})

		resp := client.ReadMessageByID(id, 3*time.Second)
		if resp == nil {
			t.Fatal("No completion response")
		}
	})
}

func TestCELTypeErrorReporting(t *testing.T) {
	client := NewLSPClient(t, "/usr/local/bin/kro", "lsp", "server", "--offline")
	defer client.Close()

	// Initialize
	id := 1
	client.SendMessage(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "initialize",
		"params": map[string]interface{}{
			"processId":    nil,
			"rootUri":      "file:///tmp",
			"capabilities": map[string]interface{}{},
		},
	})
	client.ReadMessageByID(id, 3*time.Second)

	// Initialized
	client.SendMessage(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]interface{}{},
	})
	time.Sleep(300 * time.Millisecond)

	// Open document with CEL type error
	client.SendMessage(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":        "file:///tmp/test.yaml",
				"languageId": "yaml",
				"version":    1,
				"text": `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test
spec:
  schema:
    kind: TestApp
    apiVersion: v1alpha1
    spec:
      name: string
      fullDNSName: string
  resources:
    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: ${schema.spec.name}-${1+schema.spec.fullDNSName}`,
			},
		},
	})

	// Read messages for 3 seconds to collect diagnostics
	var diagnostics []protocol.Diagnostic
	timeout := time.After(3 * time.Second)
	readDone := false

	for !readDone {
		select {
		case <-timeout:
			readDone = true
		default:
			msg := client.ReadMessage(500 * time.Millisecond)
			if msg == nil {
				continue
			}

			if msg.Method == "textDocument/publishDiagnostics" {
				var params struct {
					URI         string                   `json:"uri"`
					Diagnostics []protocol.Diagnostic `json:"diagnostics"`
				}
				if err := json.Unmarshal(msg.Params, &params); err != nil {
					continue
				}
				diagnostics = append(diagnostics, params.Diagnostics...)
				readDone = true // Got diagnostics, done reading
			}
		}
	}

	t.Logf("Received %d diagnostics", len(diagnostics))
	for i, diag := range diagnostics {
		t.Logf("Diagnostic %d (line %d): %s", i+1, diag.Range.Start.Line, diag.Message)
	}

	// Check for CEL type error
	found := false
	for _, diag := range diagnostics {
		if strings.Contains(diag.Message, "_+_") && strings.Contains(diag.Message, "int") && strings.Contains(diag.Message, "string") {
			found = true
			t.Logf("Found CEL type error: %s", diag.Message)
			break
		}
	}

	if !found {
		t.Error("Expected CEL type error diagnostic not found")
	}
}
