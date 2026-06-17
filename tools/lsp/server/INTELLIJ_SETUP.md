# IntelliJ IDEA / LSP4IJ Setup Guide

## Problem
Diagnostics not showing up for `.kroyaml` files even though the LSP server is working.

## Root Cause
IntelliJ's LSP4IJ plugin requires explicit file pattern mappings to know which files should be handled by the language server.

## Solution

### Step 1: Open LSP Settings
1. Go to: **Settings → Languages & Frameworks → Language Server Protocol**
2. Find your "Kro Language Server" configuration
3. Click "Edit" or "Configure"

### Step 2: Configure File Mappings
Click on the **"Mappings"** tab and add file name patterns for Kro files:

**File name patterns to add:**
- `*.yaml` - Standard YAML files
- `*.yml` - Alternative YAML extension  
- `*.kroyaml` - Kro-specific YAML files
- `*.kro.yaml` - Another common Kro pattern

**Language ID:** `yaml`

### Step 3: Verify Configuration
Your configuration should look like:

```
Language Server: Kro Language Server
Command: kro lsp server --offline

Mappings:
  File name pattern: *.yaml, *.yml, *.kroyaml, *.kro.yaml
  Language ID: yaml
```

### Step 4: Restart Language Server
1. In the LSP console, find your "Kro Language Server"
2. Right-click → Restart Language Server
3. Or restart IntelliJ IDEA

### Step 5: Test
1. Open a `.kroyaml` file with CEL errors
2. Diagnostics should now appear in:
   - The editor (red squiggles)
   - Problems panel (View → Tool Windows → Problems)

## Alternative: Use YAML File Type
If file name patterns don't work, you can associate `.kroyaml` with the YAML file type:

1. Go to: **Settings → Editor → File Types**
2. Find "YAML" in the list
3. Click the "+" under "File name patterns"
4. Add: `*.kroyaml`
5. Apply and restart

Then in LSP settings, use:
- **File type:** YAML
- **Language ID:** yaml

## Verification
To verify diagnostics are working:

1. Open LSP Console (View → Tool Windows → LSP Console)
2. Look for `textDocument/publishDiagnostics` notifications
3. Check that diagnostics have proper severity (1 = Error, 2 = Warning)
4. Verify the URI matches your file path

## Debugging
Enable LSP trace logging:
1. LSP Console → Right-click server → Configure
2. Set Trace level to "Verbose"
3. Watch for `textDocument/publishDiagnostics` messages

Example successful diagnostic:
```json
{
  "uri": "file:///path/to/file.kroyaml",
  "diagnostics": [{
    "range": {"start": {"line": 20, "character": 38}},
    "severity": 1,
    "source": "cel",
    "message": "found no matching overload for '_+_' applied to '(int, string)'"
  }]
}
```

## Common Issues

### Issue: "method not supported: workspace/didChangeConfiguration"
This warning is harmless. The LSP server doesn't implement this optional method.

### Issue: Server starts but no diagnostics
- Check file pattern mappings include your file extension
- Verify Language ID is set to "yaml"
- Check Problems panel is not filtered (click funnel icon)

### Issue: Diagnostics for .yaml work but not .kroyaml
- Add `*.kroyaml` to file name patterns
- Or associate .kroyaml with YAML file type

## Testing Your Setup
Run the built-in test to verify the server is working:

```bash
cd /local/home/nblaskey/kro/tools/lsp/server
go test -v -run TestCELTypeErrorReporting
```

If the test passes but IntelliJ doesn't show diagnostics, it's a file mapping issue.
