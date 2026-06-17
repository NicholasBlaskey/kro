# Kro LSP Developer Guide

## Architecture Overview

The Kro LSP server provides IDE features for ResourceGraphDefinition (RGD) files.

```
┌─────────────────────────────────────────────┐
│            Editor (VS Code, Neovim)          │
└──────────────────┬──────────────────────────┘
                   │ LSP Protocol (JSON-RPC)
┌──────────────────┴──────────────────────────┐
│           Server (server.go)                 │
│  - Initialize, DidOpen, DidChange, etc.     │
└───┬────────────┬────────────┬───────────────┘
    │            │            │
    │            │            │
┌───┴────────┐  ┌┴────────┐  ┌┴──────────────┐
│ Validation │  │ Analysis │  │   Services    │
│  Manager   │  │  Layer   │  │  (Features)   │
└───┬────────┘  └┬────────┘  └┬──────────────┘
    │            │             │
    ├─ Parser   ├─ Symbol     ├─ Completion
    ├─ Graph    │  Table      ├─ Hover
    │  Builder  ├─ Position   ├─ Definition
    └─ Error    │  Mapper     └─ Diagnostics
       Handler  └─ Context
                  Analyzer
```

## Key Components

### 1. Server (`server/server.go`)
Entry point for LSP communication. Handles:
- Protocol initialization
- Document lifecycle (open, change, save, close)
- Routing requests to appropriate services

**Key Methods**:
- `Initialize()` - Set up server capabilities
- `DidOpen()` - Handle new document
- `DidChange()` - Handle document edits
- `Completion()` - Trigger completion service
- `Hover()` - Trigger hover service
- `Definition()` - Trigger definition service

### 2. Validation (`server/validation.go`)
Validates RGD files and builds symbol tables.

**Key Responsibilities**:
- Parse YAML with position tracking
- Build graph from RGD
- Generate diagnostics
- Create symbol table
- Build position map

**Key Types**:
```go
type ValidationManager struct {
    symbolTable   *SymbolTable
    positionMap   map[string]protocol.Range
    diagnostics   []protocol.Diagnostic
}
```

### 3. Analysis Layer (`analysis/`)

#### Symbol Table (`analysis/symbol_table.go`)
Stores all symbols in the RGD for fast lookup.

```go
type SymbolTable struct {
    Schema    *SchemaSymbol
    Resources map[string]*ResourceSymbol
    Scopes    map[string]*ResourceScope
}

type ResourceSymbol struct {
    ID           string
    K8sKind      string
    Position     protocol.Range
    Dependencies []string
    Fields       map[string]FieldInfo
}
```

**Usage**:
```go
// Lookup a resource
resource := symbolTable.LookupResource("deployment")

// Get all resources
for id, res := range symbolTable.Resources {
    fmt.Printf("Resource: %s (Kind: %s)\n", id, res.K8sKind)
}

// Get schema fields
for fieldName, fieldInfo := range symbolTable.Schema.SpecFields {
    fmt.Printf("Field: %s (Type: %s)\n", fieldName, fieldInfo.Type)
}
```

#### Position Mapper (`analysis/position_mapper.go`)
Maps YAML paths to source positions.

```go
type PositionMapper struct {
    positions map[string]protocol.Range
}

// Example usage:
pos := positionMapper.GetPosition("spec.resources[0].id")
// Returns: Range{Start: {Line: 10, Char: 8}, End: {Line: 10, Char: 18}}
```

#### Completion Context (`analysis/completion_context.go`)
Determines what kind of completion is needed.

```go
type CompletionContext struct {
    Type       CompletionContextType  // "resource", "field", "function"
    Prefix     string                  // Partial text typed
    ResourceID string                  // For field completions
    InCEL      bool                    // Inside ${...}
}

// Key function:
context := AnalyzeCompletionContext(content, position, symbolTable)
// Returns context based on cursor position
```

**How it works**:
1. Find CEL expression boundaries (`${` and `}`)
2. Extract text before cursor
3. Detect context:
   - After dot → field completion
   - After operator → resource/field completion
   - In function → function argument completion
   - Default → resource completion

#### K8s Schema Provider (`analysis/k8s_schema.go`)
Provides field information for K8s resources.

```go
schemaProvider := NewK8sSchemaProvider()

// Get fields for a path
fields := schemaProvider.GetFields("Pod", "spec.containers.env")
// Returns: ["name", "value", "valueFrom"]

// Get nested fields
fields = schemaProvider.GetFields("Pod", "spec.containers.env.valueFrom")
// Returns: ["configMapKeyRef", "secretKeyRef", "fieldRef", "resourceFieldRef"]
```

**Supported Resources** (25+):
- Core: Pod, Service, Deployment, StatefulSet, DaemonSet, ReplicaSet
- Storage: PersistentVolume, PersistentVolumeClaim, StorageClass, ConfigMap, Secret
- Batch: Job, CronJob
- Networking: Ingress, NetworkPolicy, Endpoints, EndpointSlice
- RBAC: Role, RoleBinding, ClusterRole, ClusterRoleBinding
- Policy: PodDisruptionBudget, ResourceQuota, LimitRange
- Other: Namespace, ServiceAccount, PriorityClass, HorizontalPodAutoscaler

### 4. Services Layer (`services/`)

#### Completion Provider (`services/completion.go`)
Provides auto-completion items.

**Algorithm**:
1. Analyze completion context
2. Determine what to complete:
   - Resource IDs → from symbol table
   - Schema fields → from schema.spec
   - K8s fields → from K8s schema provider
   - CEL functions → from CEL provider
3. Filter by prefix
4. Return CompletionItems

**Key Function**:
```go
func (cp *CompletionProvider) ProvideCompletions(
    content string,
    position protocol.Position,
    context *CompletionContext,
) []protocol.CompletionItem
```

**Completion Types**:
- `CompletionContextResource` - Resource IDs and special identifiers
- `CompletionContextField` - Fields on resources/schema
- `CompletionContextFunction` - CEL function names

#### Hover Provider (`services/hover.go`)
Provides hover information.

**Shows**:
- Resource template/externalRef
- Schema information
- Field types
- Dependencies

**Key Function**:
```go
func (hp *HoverProvider) ProvideHover(
    content string,
    position protocol.Position,
) *protocol.Hover
```

#### Definition Provider (`services/definition.go`)
Provides go-to-definition.

**Features**:
- **Multi-segment highlighting** - Returns separate location for each path segment
- Jumps to resource/schema definition
- Supports forEach iterators

**Key Function**:
```go
func (dp *DefinitionProvider) ProvideDefinition(
    content string,
    position protocol.Position,
) []protocol.Location
```

**How Multi-Segment Works**:
1. Extract full path at cursor: `"schema.spec.fullDNSName"`
2. Split into segments: `["schema", "spec", "fullDNSName"]`
3. Calculate range for each segment
4. Return location for first segment (the resource/schema definition)
5. Return in-place locations for remaining segments (fields)

## Adding New Features

### Adding a New K8s Resource Type

1. **Add to `k8s_schema.go` or `k8s_schema_extended.go`**:
```go
"CustomResource": {
    "spec": {
        "field1",
        "field2",
        "field3",
    },
    "spec.field1": {
        "nestedField1",
        "nestedField2",
    },
    "status": {
        "statusField1",
        "statusField2",
    },
},
```

2. **Add test in `k8s_schema_working_test.go`**:
```go
{
    name:         "CustomResource spec",
    expr:         "${customresource.spec.",
    line:         18,
    char:         35,
    wantContains: []string{"field1", "field2", "field3"},
},
```

### Adding a New Completion Type

1. **Add new `CompletionContextType` in `completion_context.go`**:
```go
const (
    CompletionContextMyNewType CompletionContextType = "my_new_type"
)
```

2. **Detect it in `AnalyzeCompletionContext()`**:
```go
if /* condition for my new type */ {
    return &CompletionContext{
        Type:   CompletionContextMyNewType,
        Prefix: extractedPrefix,
    }
}
```

3. **Handle it in `ProvideCompletions()`**:
```go
case analysis.CompletionContextMyNewType:
    return cp.completeMyNewType(ctx.Prefix, position)
```

### Adding a New LSP Feature

1. **Create service file** (e.g., `services/references.go`):
```go
type ReferenceProvider struct {
    symbolTable *SymbolTable
}

func (rp *ReferenceProvider) ProvideReferences(
    content string,
    position protocol.Position,
) []protocol.Location {
    // Implementation
}
```

2. **Register in `server.go`**:
```go
// In createServerCapabilities():
capabilities.ReferencesProvider = true

// Add handler:
func (s *kroServer) References(
    context *glsp.Context,
    params *protocol.ReferenceParams,
) ([]protocol.Location, error) {
    // Call ReferenceProvider
}
```

3. **Add tests**:
```go
func TestReferences_FindAllUses(t *testing.T) {
    // Test implementation
}
```

## Testing

### Test Structure
```
test/
├── completion_test.go           # LSP client infrastructure
├── completion_nested_test.go    # Nested field completion
├── completion_resource_fields_test.go  # K8s field depth
├── cel_operators_test.go        # CEL operator handling
├── definition_segments_test.go  # Multi-segment definition
├── hover_test.go                # Hover information
├── k8s_schema_working_test.go   # K8s schema coverage
└── test_debug.go                # Debug utilities
```

### Running Tests
```bash
# All tests
cd tools/lsp/server/test
go test -v

# Specific test
go test -v -run TestCompletion_NestedSchema

# With timeout
go test -v -timeout 60s

# Short mode (skip long-running tests)
go test -v -short
```

### Writing Tests

**Completion Test**:
```go
func TestMyCompletion(t *testing.T) {
    client := NewLSPClient(t)
    defer client.Close()
    
    client.Initialize()
    
    doc := strings.Replace(testRGD, "CURSOR_HERE", "${schema.spec.", 1)
    client.OpenDocument("file:///tmp/test.yaml", doc)
    time.Sleep(300 * time.Millisecond)
    
    items := client.RequestCompletion("file:///tmp/test.yaml", 18, 30, 1, "")
    
    assertContains(t, items, "name", "should have name field")
}
```

**Definition Test**:
```go
func TestMyDefinition(t *testing.T) {
    client := NewLSPClient(t)
    defer client.Close()
    
    client.Initialize()
    
    doc := strings.Replace(testRGD, "CURSOR_HERE", "${schema.spec.name}", 1)
    client.OpenDocument("file:///tmp/test.yaml", doc)
    time.Sleep(300 * time.Millisecond)
    
    locations := client.RequestDefinition("file:///tmp/test.yaml", 18, 20)
    
    if len(locations) != 3 {
        t.Errorf("expected 3 locations, got %d", len(locations))
    }
}
```

## Debugging

### Enable Logging
```bash
kro lsp server --offline --log-level=debug
```

### Debug in VS Code
Add to `.vscode/launch.json`:
```json
{
    "type": "go",
    "request": "launch",
    "name": "LSP Server",
    "program": "${workspaceFolder}/tools/lsp/server",
    "args": ["--offline"]
}
```

### Print Debug Info in Tests
```go
t.Logf("Context: %+v", context)
t.Logf("Items: %v", itemLabels(items))
```

### Common Issues

**Issue**: Completion returns nothing
**Debug**: Check if symbol table is built (`doc.SymbolTable != nil`)

**Issue**: Definition goes to wrong location  
**Debug**: Check position mapper accuracy with test RGD

**Issue**: K8s fields don't appear
**Debug**: Verify resource has `K8sKind` set in symbol table

## Performance Tips

1. **Cache symbol tables** - Don't rebuild on every request
2. **Use map lookups** - O(1) for resource/field lookup
3. **Limit K8s schema depth** - Currently capped at 6 levels
4. **Debounce validation** - Don't validate on every keystroke

## Code Style

- **Concise names**: `ctx` not `context`, `pos` not `position`
- **Error handling**: Always check errors, log but don't crash
- **Comments**: Explain WHY not WHAT
- **Tests**: Test behavior, not implementation

## Contributing

1. Write tests first (TDD)
2. Keep PRs focused (one feature per PR)
3. Update this guide when adding major features
4. Run `make test` before committing
5. Follow existing code patterns

## Resources

- LSP Specification: https://microsoft.github.io/language-server-protocol/
- GLSP Library: https://github.com/tliron/glsp
- CEL Language: https://github.com/google/cel-spec
- Kro Docs: https://kro.run/docs
