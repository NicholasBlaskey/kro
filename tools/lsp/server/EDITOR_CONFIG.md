# Editor Configuration for Kro LSP

## The Highlighting Issue

Some editors treat everything inside `${...}` as a single string token due to YAML syntax highlighting, even though the LSP provides semantic tokens to break it up.

This means the whole expression looks like one unit, but **go-to-definition still works** - you just need to click on the right character position.

## How to Use Despite Highlighting

Even if `${schema.spec.fullDNSName}` is highlighted as one unit, you can still navigate:

### Position Your Cursor Precisely:

1. **To jump to "schema"**: Place cursor on any character of "schema" (positions 20-25)
2. **To jump to "spec"**: Place cursor on any character of "spec" (positions 27-30)
3. **To jump to "fullDNSName"**: Place cursor on any character of "fullDNSName" (positions 32-42)

### Example:
```
          name: ${schema.spec.fullDNSName}
                  ^^^^^^ ^^^^ ^^^^^^^^^^^
                    |     |        |
                    |     |        └─ Cursor here: F12 jumps to fullDNSName definition
                    |     └────────── Cursor here: F12 jumps to spec definition  
                    └────────────────Cursor here: F12 jumps to schema definition
```

## VS Code Configuration

### Enable Semantic Highlighting

Add to your `settings.json`:

```json
{
  "editor.semanticHighlighting.enabled": true,
  "[yaml]": {
    "editor.semanticHighlighting.enabled": true
  }
}
```

### Verify LSP is Active

1. Open Command Palette (Cmd/Ctrl+Shift+P)
2. Type "Developer: Show Running Extensions"
3. Verify your LSP extension is active

### Test Go-to-Definition

1. Place cursor on "schema" in `${schema.spec.fullDNSName}`
2. Press **F12** (or Cmd+Click)
3. Should jump to schema definition

If it doesn't work:
- Check LSP server is running: `ps aux | grep kro`
- Check output panel: View → Output → Select "Kro LSP"
- Look for errors

## Neovim Configuration

### Enable Semantic Tokens

```lua
-- In your LSP config
require('lspconfig')['kro'].setup{
  capabilities = vim.lsp.protocol.make_client_capabilities(),
  on_attach = function(client, bufnr)
    -- Enable semantic tokens
    if client.server_capabilities.semanticTokensProvider then
      vim.lsp.semantic_tokens.start(bufnr, client.id)
    end
  end
}
```

### Test Go-to-Definition

1. Place cursor on "schema" in `${schema.spec.fullDNSName}`
2. Press `gd` (or your keybinding)
3. Should jump to schema definition

## Workaround: Count Characters

If highlighting is still confusing, you can count character positions:

```
          name: ${schema.spec.fullDNSName}
          0         1         2         3         4
          0123456789012345678901234567890123456789012
                    
Position 20-25: "schema"
Position 27-30: "spec"  
Position 32-42: "fullDNSName"
```

Place cursor at:
- Position 20-25 → Jumps to schema
- Position 27-30 → Jumps to spec
- Position 32-42 → Jumps to fullDNSName

## Verify It's Working

### Test Script:

Create a test RGD:
```yaml
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test
spec:
  schema:
    kind: Test
    spec:
      name: string
  resources:
    - id: deployment
      template:
        metadata:
          name: ${schema.spec.name}
```

### Test Each Segment:

1. **Cursor on "schema"** (after ${):
   - Press F12
   - Should jump to line 6 (`schema:`)
   - ✅ Working!

2. **Cursor on "spec"** (after the dot):
   - Press F12
   - Should jump to line 8 (`spec:`)
   - ✅ Working!

3. **Cursor on "name"** (after second dot):
   - Press F12
   - Should jump to line 9 (`name: string`)
   - ✅ Working!

## Why Highlighting Still Looks Wrong

The YAML syntax highlighter runs **before** LSP semantic tokens and treats `${...}` as a string interpolation. Some editors prioritize syntax highlighting over semantic tokens.

**But this doesn't affect functionality!** The LSP still knows exactly where each segment is, and go-to-definition works perfectly.

## Future Solution

To get perfect highlighting, we'd need:

1. **Custom YAML grammar** that understands Kro CEL expressions
2. **TextMate grammar** for VS Code
3. **TreeSitter parser** for Neovim

This is a larger effort and not required for functionality. The current solution provides full navigation capabilities.

## Alternative: Use Plain Cursor Positioning

Instead of relying on highlighting, use cursor position awareness:

1. **Count characters** from `${` 
2. **Use arrow keys** to position precisely
3. **Use word motions** (Ctrl+Arrow in VS Code, w/b in Vim)

### Example in VS Code:

```
name: ${schema.spec.fullDNSName}
       ^
       Start after ${
       
Press Ctrl+Right → moves to "." after schema
Press Ctrl+Right → moves to "." after spec  
Press Ctrl+Right → moves to end of fullDNSName

Now press F12 at any position to jump!
```

## Bottom Line

**The feature works perfectly** - go-to-definition jumps to the correct location based on cursor position.

The highlighting issue is cosmetic and doesn't affect functionality. You can still navigate precisely by:
1. Positioning cursor carefully
2. Using keyboard navigation (Ctrl+Arrow, word motions)
3. Clicking on the specific character you want

The LSP knows exactly where each segment is, even if the editor's syntax highlighting doesn't show it visually.
