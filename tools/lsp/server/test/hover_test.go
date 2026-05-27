package test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type HoverResponse struct {
	Contents struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	} `json:"contents"`
	Range *struct {
		Start struct {
			Line      uint32 `json:"line"`
			Character uint32 `json:"character"`
		} `json:"start"`
		End struct {
			Line      uint32 `json:"line"`
			Character uint32 `json:"character"`
		} `json:"end"`
	} `json:"range,omitempty"`
}

func (c *LSPClient) RequestHover(uri string, line, char int) *HoverResponse {
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
	c.SendRequest(requestID, "textDocument/hover", params)

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

		// Try to unmarshal as hover response
		var hover HoverResponse
		if err := json.Unmarshal(resp.Result, &hover); err != nil {
			c.t.Logf("Failed to unmarshal hover result: %v", err)
			return nil
		}

		return &hover
	}

	return nil
}

// TestHover_ResourceReferences tests hover information for resource references
func TestHover_ResourceReferences(t *testing.T) {
	tests := []struct {
		name             string
		expr             string
		line             int
		char             int
		wantContains     []string
		wantKind         string
		description      string
	}{
		{
			name:         "hover over schema reference",
			expr:         "${schema.spec.name}",
			line:         18,
			char:         20, // Over "schema"
			wantContains: []string{"schema", "TestApp"},
			wantKind:     "markdown",
			description:  "Should show schema information",
		},
		{
			name:         "hover over resource reference",
			expr:         "${deployment.spec.replicas}",
			line:         18,
			char:         20, // Over "deployment"
			wantContains: []string{"deployment", "template"},
			wantKind:     "markdown",
			description:  "Should show resource information",
		},
		{
			name:         "hover over service reference",
			expr:         "${service.spec.type}",
			line:         18,
			char:         20, // Over "service"
			wantContains: []string{"service", "template"},
			wantKind:     "markdown",
			description:  "Should show service information",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-hover.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			hover := client.RequestHover("file:///tmp/test-hover.yaml", tt.line, tt.char)

			if hover == nil {
				t.Errorf("%s: expected hover information, got nil", tt.name)
				return
			}

			t.Logf("%s: %s - got hover with kind=%s, content length=%d",
				tt.name, tt.description, hover.Contents.Kind, len(hover.Contents.Value))

			if hover.Contents.Kind != tt.wantKind {
				t.Errorf("%s: expected kind %s, got %s", tt.name, tt.wantKind, hover.Contents.Kind)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(hover.Contents.Value, want) {
					t.Errorf("%s: expected hover to contain '%s', got: %s",
						tt.name, want, hover.Contents.Value)
				}
			}
		})
	}
}

// TestHover_FieldPaths tests hover over field paths
func TestHover_FieldPaths(t *testing.T) {
	tests := []struct {
		name         string
		expr         string
		line         int
		char         int
		wantHover    bool
		description  string
	}{
		{
			name:        "hover over spec field",
			expr:        "${schema.spec.name}",
			line:        18,
			char:        25, // Over "spec"
			wantHover:   true,
			description: "Should show information about spec field",
		},
		{
			name:        "hover over nested field",
			expr:        "${deployment.spec.replicas}",
			line:        18,
			char:        30, // Over "replicas"
			wantHover:   true,
			description: "Should show information about replicas field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-hover-fields.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			hover := client.RequestHover("file:///tmp/test-hover-fields.yaml", tt.line, tt.char)

			if tt.wantHover && hover == nil {
				t.Errorf("%s: expected hover information, got nil", tt.name)
			} else if !tt.wantHover && hover != nil {
				t.Errorf("%s: expected no hover, got: %v", tt.name, hover.Contents.Value)
			}

			if hover != nil {
				t.Logf("%s: ✅ got hover content: %s", tt.name, hover.Contents.Value)
			}
		})
	}
}

// TestHover_OperatorExpressions tests hover in expressions with operators
func TestHover_OperatorExpressions(t *testing.T) {
	tests := []struct {
		name        string
		expr        string
		line        int
		char        int
		wantHover   bool
		description string
	}{
		{
			name:        "hover in concatenation",
			expr:        "${schema.spec.name+\"-suffix\"}",
			line:        18,
			char:        20, // Over "schema"
			wantHover:   true,
			description: "Should show schema info even in concatenation",
		},
		{
			name:        "hover after operator",
			expr:        "${schema.spec.replicas>0?deployment.spec.replicas:1}",
			line:        18,
			char:        40, // Over "deployment"
			wantHover:   true,
			description: "Should show deployment info in ternary expression",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewLSPClient(t)
			defer client.Close()

			client.Initialize()

			doc := strings.Replace(testRGDWithSchema, "CURSOR_HERE", tt.expr, 1)
			client.OpenDocument("file:///tmp/test-hover-operators.yaml", doc)
			time.Sleep(300 * time.Millisecond)

			hover := client.RequestHover("file:///tmp/test-hover-operators.yaml", tt.line, tt.char)

			if tt.wantHover && hover == nil {
				t.Errorf("%s: expected hover information, got nil", tt.name)
			}

			if hover != nil {
				t.Logf("%s: ✅ %s - got hover", tt.name, tt.description)
			}
		})
	}
}
