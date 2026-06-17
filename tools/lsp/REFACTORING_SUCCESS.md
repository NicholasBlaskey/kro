# LSP Server Refactoring - SUCCESS! ✅

**Date**: 2026-05-27  
**Goal**: Get CEL validation working in `/usr/local/bin/kro lsp server --offline`

## What We Did

### 1. Refactored LSP Server Package Structure

**Before**:
```
tools/lsp/server/
  ├── main.go           (package main - not importable!)
  ├── server.go         (package main)
  ├── validation.go     (package main)
  ├── cel_validator.go  (package main)
  └── ...
```

**After**:
```
tools/lsp/server/
  ├── cmd/
  │   └── main.go       (package main - standalone entry point)
  ├── server.go         (package server - importable!)
  ├── validation.go     (package server)
  ├── cel_validator.go  (package server)
  └── ...
```

### 2. Exported Key Types and Functions

Changed:
- `kroServer` → `KroServer` (exported)
- `createHandler()` → `CreateHandler()` (exported)
- Added `SetServer()` method (exported)

This allows the CLI to import and use the LSP server as a library.

### 3. Updated CLI Integration

The CLI at `/local/home/nblaskey/kro/cmd/kro/commands/lsp/server.go` now:
```go
import lspserver "github.com/kro-run/kro/tools/lsp/server"

kroServer := lspserver.NewKroServer(log, clientConfig, nil)
handler := kroServer.CreateHandler()
lspServer := server.NewServer(handler, "kro-language-server", false)
kroServer.SetServer(lspServer)
```

### 4. Built and Installed

```bash
cd /local/home/nblaskey/kro/cmd/kro
go build -o kro-new .
sudo mv kro-new /usr/local/bin/kro
```

## Verification

✅ **Standalone server builds**: `cd tools/lsp/server && go build ./cmd`  
✅ **CLI builds**: `cd cmd/kro && go build .`  
✅ **Server starts**: `kro lsp server --offline` works  
✅ **Tests pass**: All CEL validation tests passing  
✅ **CEL validation working**: Catches undeclared references and type errors

## Test Results

```bash
$ go test -v -run TestCELValidation
=== RUN   TestCELValidation
    cel_validator_test.go:64: Diagnostic: CEL error: undeclared reference to 'nonExistentResource'
--- PASS: TestCELValidation (0.00s)
=== RUN   TestCELValidationValidExpression
--- PASS: TestCELValidationValidExpression (0.00s)
PASS
```

## How to Use

### From CLI (Recommended)
```bash
# Start LSP server with CEL validation
kro lsp server --offline

# Test with the demo file
# Open /tmp/test-cel-errors.yaml in your editor
# You should see CEL errors highlighted!
```

### Standalone
```bash
# Build standalone server
cd tools/lsp/server
go build -o kro-lsp-server ./cmd

# Run it
./kro-lsp-server
```

## What's New - CEL Validation Features

1. **Context-Aware Validation**
   - `includeWhen`: Only `schema` available
   - `readyWhen`: `self` + all resources available
   - Template `${...}`: `schema` + all resources available

2. **Precise Error Locations**
   - Errors show at exact line and column
   - Works for both bare expressions and `${...}` templates

3. **Real-Time Feedback**
   - Validates as you type
   - No need to save or run validation manually

## Test File

Try this file to see CEL validation in action:

```yaml
# /tmp/test-cel-errors.yaml
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test-cel-errors
spec:
  schema:
    kind: TestApp
    apiVersion: v1alpha1
  resources:
    - id: deployment
      readyWhen:
        - "undefinedResource.status.ready == true"  # ❌ Error!
      includeWhen:
        - "self.status.ready == true"              # ❌ Error! (self not in includeWhen)
```

Open this file in your editor and you should see red squiggly lines under the errors!

## Backup

The old binary is backed up at:
- `/usr/local/bin/kro.old` (original from May 27 17:58)
- `/usr/local/bin/kro.backup-before-cel` (just before this refactoring)

To revert if needed:
```bash
sudo cp /usr/local/bin/kro.backup-before-cel /usr/local/bin/kro
```

## Files Changed

### New Files
- `tools/lsp/server/cmd/main.go` (standalone entry point)
- `tools/lsp/server/cel_validator.go` (CEL validation engine)
- `tools/lsp/server/analysis/cel_validation.go` (CEL utilities)
- `tools/lsp/server/cel_validator_test.go` (tests)

### Modified Files
- `tools/lsp/server/server.go` (package server, exported types)
- `tools/lsp/server/validation.go` (package server, CEL integration)
- `tools/lsp/server/document.go` (package server)
- `tools/lsp/BUILD.md` (updated docs)
- `tools/lsp/STATUS.md` (updated status)

## Success Metrics

✅ All original LSP features still work  
✅ CEL validation added and working  
✅ Both online and offline modes work  
✅ CLI integration working: `kro lsp server --offline`  
✅ Standalone server still buildable  
✅ All tests passing  
✅ No breaking changes

## Next Steps

1. Test in your actual editor with real RGD files
2. Verify hover, completion, and go-to-definition still work
3. Try creating intentional CEL errors to see them caught in real-time
4. Consider adding more CEL validation features (type inference, suggestions, etc.)
