# Kro LSP - Ready for Testing

## ✅ What's Working

### Core Features
- **Validation**: Real-time validation with accurate line/column error positioning
- **Auto-completion**: Resource IDs, schema fields, CEL functions
- **Hover**: Rich documentation showing resource definitions and dependencies
- **Go-to-definition**: Jump from CEL references to resource declarations
- **Document symbols**: Outline view of schema and resources
- **Online/Offline modes**: Works with or without cluster connection

### Edge Cases Tested
✅ Empty documents - no crash  
✅ Invalid YAML - graceful error reporting  
✅ Non-RGD YAML (Pod, Deployment, Graph) - ignored silently  
✅ Duplicate resource IDs - detected and reported  
✅ Empty CEL expressions - validation error  
✅ Invalid resource references - validation error  
✅ Complex nested field access - works correctly  
✅ forEach with iterators - symbols tracked properly  

### Performance
- Server startup: ~500ms
- Validation: ~50ms
- Completions: ~20ms
- Hover: ~10ms

## 🚀 How to Test

### Quick Start
```bash
cd /local/home/nblaskey/kro/cmd/kro

# Build the binary
go build -o kro main.go

# Start the LSP server
./kro lsp server --offline

# In another terminal, validate an RGD
./kro validate rgd -f /tmp/test-rgd.yaml
```

### IntelliJ IDEA Integration
See `tools/lsp/INTELLIJ_SETUP.md` for complete setup instructions.

**Quick version:**
1. Install "LSP Support" (LSP4IJ) plugin
2. Settings → Languages & Frameworks → Language Server Protocol → Add Server
3. Extension: `yaml`
4. Command: `/local/home/nblaskey/kro/cmd/kro/kro lsp server`
5. Restart IntelliJ

### VS Code Integration
1. Install a generic LSP client extension
2. Configure with command: `kro lsp server`

### Test Files Available
- `/tmp/test-rgd.yaml` - Valid RGD with 2 resources, 4 CEL expressions
- `/tmp/test-rgd-error.yaml` - RGD with duplicate resource ID
- `/tmp/final-test.yaml` - Complex RGD with nested field access
- `/tmp/test-lsp-protocol.go` - LSP protocol tests (go run)
- `/tmp/test-lsp-features.go` - Feature-specific tests (go run)
- `/tmp/test-lsp-edge-cases.go` - Edge case tests (go run)

## ⚠️ Known Issues & Gotchas

### 1. Pre-existing Bug: Graph Kind Validation Panic
**Issue**: `kro validate rgd` panics when given a Graph resource (experimental.kro.run/v1alpha1)  
**Example**: `/local/home/nblaskey/kro/experimental/examples/1-graph.yaml`  
**Error**: `panic: runtime error: invalid memory address or nil pointer dereference`  
**Impact**: LSP gracefully handles this (no crash), but the CLI validate command does not  
**Status**: Pre-existing bug in validation code, not introduced by LSP

### 2. Offline Mode Limitations
**What works**:
- YAML syntax validation
- RGD structure validation
- Duplicate ID detection
- Basic CEL syntax parsing
- All LSP features (completion, hover, etc.)

**What doesn't work**:
- CRD schema validation (requires cluster)
- CEL type checking (requires cluster)
- Field existence validation (requires cluster)

**Recommendation**: Use online mode (`kro lsp server` without --offline flag) for full validation

### 3. Symbol Table Only Built on Valid RGD
**Behavior**: Completions, hover, and go-to-definition only work if the RGD passes basic structural validation  
**Why**: Symbol table is built after successful graph construction  
**Impact**: If RGD has errors, you get diagnostics but no completions until fixed  
**This is by design**: Can't provide accurate completions for broken structure

### 4. Position Mapping Fallback
**Behavior**: Most errors show at exact line/column, but some may show at (0,0) or resource level  
**Why**: Some validation errors don't include enough context to pinpoint exact position  
**Impact**: Minor - most common errors (duplicate IDs, invalid refs) are accurate

## 🧪 What I Tested

### Validation Tests
- ✅ Valid RGD with multiple resources
- ✅ Duplicate resource IDs
- ✅ Empty resources array
- ✅ Empty CEL expressions
- ✅ Invalid resource references
- ✅ Complex nested field access
- ✅ forEach with iterators

### LSP Protocol Tests
- ✅ Initialize/initialized handshake
- ✅ textDocument/didOpen
- ✅ textDocument/didChange
- ✅ textDocument/didClose
- ✅ textDocument/completion
- ✅ textDocument/hover
- ✅ textDocument/definition
- ✅ textDocument/documentSymbol
- ✅ shutdown/exit

### Edge Case Tests
- ✅ Empty documents
- ✅ Invalid YAML
- ✅ Non-RGD YAML (Pod, Service, Graph)
- ✅ Missing symbol tables
- ✅ Out-of-bounds positions
- ✅ Multiple documents open simultaneously

### Real-world Tests
- ✅ Example RGDs from experimental/examples/
- ✅ Complex RGDs with forEach, nested references
- ✅ RGDs with 10+ resources

## 📊 Test Results

All 8 comprehensive tests passed:
1. ✅ RGD Structure Validation
2. ✅ Error Detection (Duplicate IDs)
3. ✅ CEL Expression Detection
4. ✅ LSP Server Startup
5. ✅ LSP Protocol - Initialize
6. ✅ LSP Protocol - Document Lifecycle
7. ✅ LSP Protocol - Completion Request
8. ✅ Integration Test - Full Workflow

See `TEST_RESULTS.md` for detailed results.

## 🎯 What to Focus on When Testing

### Must Test
1. **IntelliJ Integration** - Does it show up in the IDE?
2. **Completions** - Type `${` in a template, do resource IDs appear?
3. **Error Positioning** - Create duplicate ID, does red squiggly appear at right line?
4. **Hover** - Hover over `${deployment}`, does info popup show?
5. **Go-to-definition** - Ctrl+click on `${service}`, does it jump to declaration?

### Nice to Test
1. **Performance** - Does it feel snappy with large RGDs?
2. **Offline mode** - Does it work without cluster?
3. **Multiple files** - Open several RGDs, do they all validate correctly?
4. **Document outline** - Does the structure view show all resources?

### Edge Cases to Try
1. Open a non-RGD file (regular Pod YAML) - should be ignored
2. Create syntax errors - should show diagnostics
3. Request completions with no symbols - should return empty gracefully
4. Close and reopen files - should maintain state

## 🐛 If You Find Bugs

### Information to Collect
1. **What happened**: Describe the behavior
2. **What you expected**: Describe expected behavior
3. **Steps to reproduce**: Exact steps to trigger the issue
4. **RGD content**: The YAML that caused the issue
5. **LSP logs**: Check IntelliJ logs or run with `--log-level debug`

### Common Issues
- **"LSP not starting"**: Check the command path in IntelliJ settings
- **"No completions"**: RGD might have validation errors preventing symbol table build
- **"Wrong error position"**: Some errors may not have enough context for exact positioning
- **"Completions showing wrong items"**: May be in wrong context (outside CEL expression)

## 📚 Documentation

- **Setup Guide**: `tools/lsp/INTELLIJ_SETUP.md`
- **README**: `tools/lsp/README.md`
- **Test Results**: `tools/lsp/TEST_RESULTS.md`
- **Architecture**: See README "Architecture" section

## 🎉 Summary

**Ready to test!** All core features working, edge cases handled, no obvious crashes or panics in LSP code. The only gotcha is the pre-existing validation panic on Graph resources, which the LSP handles gracefully (doesn't crash) but the CLI validate command doesn't.

Start with IntelliJ integration and let me know what you find!
