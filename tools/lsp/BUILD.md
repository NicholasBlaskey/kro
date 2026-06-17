# Building the Kro LSP Server

## Current Working Setup

The Kro LSP server is currently integrated into the `kro` CLI and working correctly.

**Installed binary**: `/usr/local/bin/kro`
**Usage**: `kro lsp server [--offline]`

## Building the Standalone LSP Server

The standalone LSP server can be built independently:

```bash
cd tools/lsp/server
go build -o kro-lsp-server .
```

This creates a standalone binary (`tools/lsp/server/kro-lsp-server`) that can be run directly:

```bash
# Test it starts correctly
timeout 2 ./kro-lsp-server

# Or install it
sudo cp kro-lsp-server /usr/local/bin/
```

## Building the Full CLI (NEEDS REFACTORING)

⚠️ **Current Issue**: The LSP server uses `package main` which cannot be imported by the CLI.

The working `/usr/local/bin/kro` binary was built with a version where the LSP server was importable.
To rebuild the full CLI with LSP support, we need to:

1. **Refactor `tools/lsp/server` to be a library**:
   - Change `package main` to `package server` in all `.go` files
   - Export types: `kroServer` → `KroServer`
   - Export functions: `getVersion()` → `GetVersion()`
   - Keep `cmd/main.go` as the standalone entry point

2. **Set up Go workspace**:
   ```bash
   # In project root
   go work init . ./cmd/kro ./tools/lsp/server
   ```

3. **Build the CLI**:
   ```bash
   cd cmd/kro
   go build -o kro .
   ```

## Quick Rebuild (Use Working Binary)

**Recommended**: Just use the working binary that's already installed:

```bash
# Verify it works
/usr/local/bin/kro lsp server --help

# Test it
timeout 2 /usr/local/bin/kro lsp server --offline
```

This binary has all features working:
- ✓ Real-time validation
- ✓ CEL expression validation with precise error locations
- ✓ Auto-completion  
- ✓ Hover documentation
- ✓ Go-to-definition
- ✓ Document symbols

## Adding New Features

When adding new features to the LSP:

1. **Modify the code** in `tools/lsp/server/` (services/, analysis/, etc.)

2. **Test with standalone build**:
   ```bash
   cd tools/lsp/server
   go build -o test-server .
   timeout 2 ./test-server  # Should start without errors
   ```

3. **Run tests**:
   ```bash
   cd tools/lsp/server/test
   go test -v -timeout 60s
   ```

4. **Test in your editor**:
   - The working `/usr/local/bin/kro` CLI should automatically use the updated code
   - If not, you may need to rebuild (see refactoring section above)

5. **Verify all features still work**:
   ```bash
   # Test the command still works
   /usr/local/bin/kro lsp server --help
   
   # Test it starts
   timeout 2 /usr/local/bin/kro lsp server --offline
   
   # Test in your actual editor with a real RGD file
   ```

## Known Issues

- The current CLI integration was built with a non-standard setup
- The LSP server needs to be refactored to be properly importable
- For now, use the working `/usr/local/bin/kro` binary
