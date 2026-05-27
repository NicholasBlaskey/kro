# IntelliJ IDEA / JetBrains IDEs Setup for Kro LSP

This guide shows you how to set up the Kro Language Server Protocol (LSP) in IntelliJ IDEA and other JetBrains IDEs (PyCharm, WebStorm, GoLand, etc.).

## Prerequisites

- IntelliJ IDEA or any JetBrains IDE (2023.2 or later recommended)
- `kro` CLI installed and available in your PATH
- Verify installation: `kro lsp --help`

## Step 1: Install the LSP Support Plugin

1. Open IntelliJ IDEA
2. Go to **Settings/Preferences** → **Plugins**
3. Click the **Marketplace** tab
4. Search for **"LSP Support"** or **"LSP4IJ"**
5. Click **Install** on the plugin
6. **Restart** your IDE when prompted

**Plugin Details:**
- Plugin Name: LSP Support (LSP4IJ)
- Provides: Generic LSP client support for JetBrains IDEs
- Alternative: If "LSP Support" isn't available, search for "Language Server Protocol" or "LSP4IJ"

## Step 2: Configure the Kro Language Server

1. Open **Settings/Preferences** (Ctrl+Alt+S on Windows/Linux, ⌘+, on Mac)
2. Navigate to **Languages & Frameworks** → **Language Server Protocol** → **Server Definitions**
3. Click the **+** button to add a new server
4. Configure the server:

   **Configuration:**
   ```
   Name: Kro LSP
   Extension: yaml
   Command: kro lsp server
   ```

   **Or specify full command with flags:**
   ```
   Command: kro lsp server --log-level info
   ```

   **For offline mode (no cluster connection):**
   ```
   Command: kro lsp server --offline
   ```

5. Click **OK** to save
6. Click **Apply** and **OK** to close settings

## Step 3: Enable LSP for YAML Files

1. Go to **Settings/Preferences** → **Languages & Frameworks** → **Language Server Protocol** → **File Associations**
2. Ensure `*.yaml` files are associated with the Kro LSP server
3. You may need to add a mapping:
   - Pattern: `*.yaml`
   - Server: Kro LSP

## Step 4: Verify Setup

1. Open a Kro ResourceGraphDefinition YAML file
2. Check the IDE status bar - you should see "LSP: Kro" or similar indicator
3. Try these features:

   **Real-time Validation:**
   - Create a duplicate resource ID - you should see a red squiggle
   - Hover over the error to see the message

   **Auto-completion:**
   - Type `${` in a template field
   - You should see completion suggestions for resource IDs

   **Hover Documentation:**
   - Hover over a resource ID reference (e.g., `${deployment}`)
   - You should see documentation with type, dependencies, etc.

   **Go-to-Definition:**
   - Ctrl+Click (Cmd+Click on Mac) on a resource reference
   - You should jump to the resource declaration

   **Document Outline:**
   - Open the Structure view (Alt+7 / ⌘+7)
   - You should see Schema and Resources listed

## Troubleshooting

### LSP Server Not Starting

**Check if `kro` is in PATH:**
```bash
which kro
kro lsp --help
```

**Try absolute path:**
Instead of `kro lsp server`, use the full path:
```
/usr/local/bin/kro lsp server
```

### No Completions or Hover Information

**Check LSP logs:**
1. Go to **Help** → **Diagnostic Tools** → **Debug Log Settings**
2. Add: `#com.redhat.devtools.lsp4ij`
3. Reproduce the issue
4. Check **Help** → **Show Log in Finder/Explorer**

**Verify server is running:**
```bash
# Check if server starts manually
kro lsp server --log-level debug
```

### Features Only Work After File Save

Some versions of the LSP plugin require saving the file to trigger updates.

**Solution:**
- Enable auto-save: **Settings** → **Appearance & Behavior** → **System Settings** → **Save files automatically**

### Server Crashes or Disconnects

**Run in offline mode:**
```
kro lsp server --offline
```

**Check cluster connectivity:**
```bash
kubectl cluster-info
```

If you don't have cluster access, always use `--offline` flag.

## Advanced Configuration

### Debug Mode

To see detailed LSP communication:

1. Configure command with debug logging:
   ```
   kro lsp server --log-level debug
   ```

2. Check IDE logs: **Help** → **Show Log in Finder/Explorer**

### Multiple Clusters

If you work with multiple Kubernetes clusters:

**Option 1: Switch context**
```bash
kubectl config use-context my-cluster
# Then restart LSP server in IDE
```

**Option 2: Use offline mode**
```
kro lsp server --offline
```

## Features Available

✅ **Real-time Validation**
- Syntax errors highlighted as you type
- Accurate error positioning (exact line/column)

✅ **Auto-completion**
- Resource IDs after `${`
- Fields after `${resourceId.`
- CEL functions with signatures

✅ **Hover Documentation**
- Resource type and details
- Dependency information
- External reference details

✅ **Go-to-Definition**
- Jump from reference to declaration
- Works for resources, schema, iterators

✅ **Document Outline**
- Hierarchical view of Schema + Resources
- ForEach iterators shown as children

## Example RGD for Testing

Save this as `test-app.yaml`:

```yaml
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: test-app
spec:
  schema:
    kind: TestApplication
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
          name: ${schema.spec.name}
        spec:
          replicas: ${schema.spec.replicas}

    - id: service
      template:
        apiVersion: v1
        kind: Service
        metadata:
          name: ${schema.spec.name}-svc
        spec:
          selector:
            app: ${deployment.metadata.name}
```

**Try these:**
1. Hover over `${deployment.metadata.name}` - see deployment documentation
2. Type `${` in a new template - see completions
3. Ctrl+Click on `deployment` - jump to its declaration
4. Open Structure view - see schema + resources

## Getting Help

- Kro Documentation: https://github.com/kubernetes-sigs/kro
- LSP Server Issues: https://github.com/kubernetes-sigs/kro/issues
- IntelliJ LSP Plugin: https://plugins.jetbrains.com/plugin/[plugin-id]

## See Also

- [VS Code Setup](./VSCODE_SETUP.md)
- [Neovim Setup](./NEOVIM_SETUP.md)
- [LSP Features Guide](./LSP_FEATURES.md)
