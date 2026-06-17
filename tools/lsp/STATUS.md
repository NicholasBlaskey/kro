# LSP Server Status

## Current State (2026-05-27)

✅ **WORKING**: `/usr/local/bin/kro lsp server` is fully functional

All features confirmed working:
- ✓ Real-time validation with accurate error positioning
- ✓ **NEW: CEL expression validation with precise error locations**
- ✓ Auto-completion for resource IDs, schema fields, CEL functions
- ✓ Hover documentation showing dependencies
- ✓ Go-to-definition for navigating resources
- ✓ Document symbols/outline

## How to Use

```bash
# Start in offline mode (no cluster required)
kro lsp server --offline

# Start in online mode (validates against cluster CRDs)
kro lsp server

# With debug logging
kro lsp server --offline --log-level debug
```

## Safe Development Workflow

1. **Make changes** to code in `tools/lsp/server/`
2. **Test standalone**: `cd tools/lsp/server && go build .`
3. **Run tests**: `cd tools/lsp/server/test && go test -v`
4. **Test in editor**: The working `/usr/local/bin/kro` should pick up changes
5. **Verify**: Open an RGD file and test hover/completion/go-to-def

## Known Limitations

- The current binary build process is not fully documented
- To rebuild the full CLI with LSP support requires refactoring (see BUILD.md)
- For now, use the working binary and only modify the LSP code itself

## If Something Breaks

1. **Don't panic** - the working binary is backed up at `/usr/local/bin/kro.old`
2. **Restore it**: `sudo cp /usr/local/bin/kro.old /usr/local/bin/kro`
3. **Verify**: `kro lsp server --help`

## Adding Features

See [BUILD.md](BUILD.md) for detailed instructions on adding new LSP features safely.
