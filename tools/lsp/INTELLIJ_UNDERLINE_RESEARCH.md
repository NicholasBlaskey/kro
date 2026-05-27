# IntelliJ Ctrl+Hover Underline — Research & Workaround Options

## The Problem

In `${schema.spec.name}`, Ctrl+hovering underlines the **entire** expression instead of just the segment under the cursor (e.g. just `schema` or `spec` or `name`).

The LSP server correctly returns `LocationLink` with `originSelectionRange` scoped to the individual segment. VS Code respects this. IntelliJ does not.

## Root Cause

IntelliJ's navigation underline is driven by the **PSI tree**, not the LSP response.

The call chain:
```
CtrlMouseHandler2
  → gtdProviders.kt → file.findElementAt(offset)
    → gets PSI leaf element (the YAML TEXT token)
      → getReferenceRanges(leafElement)
        → returns full text range of that PSI element
```

YAML's lexer (`YAMLFlexLexer` with `MergingLexerAdapter`) merges adjacent characters into single `TEXT` tokens. So `schema.spec.name` is one token — one underline.

The `originSelectionRange` field from `LocationLink` is never read in this code path.

## Why .rs Files Work

For file types **without** a bundled IntelliJ plugin/parser (Rust via LSP, TextMate languages), LSP4IJ provides a custom `FileViewProvider` that uses **semantic tokens** to build the PSI tree. In that case, each semantic token becomes a PSI leaf, and the underline scopes to that token.

YAML has a **bundled plugin** (`org.jetbrains.plugins.yaml`) that claims all `.yaml`/.yml` files. Its lexer wins. LSP4IJ's semantic token file view only activates when no existing PSI parser is registered for the file type.

## What We've Confirmed

- [x] Server returns correct `originSelectionRange` per segment
- [x] Server returns correct `LocationLink` JSON
- [x] Client reports `linkSupport: true`
- [x] Go-to-definition navigation **works correctly** (jumps to right place based on cursor position)
- [x] Only the visual underline is wrong — it's cosmetic, not functional
- [ ] `textDocument/documentHighlight` does NOT affect this underline
- [ ] `DocumentLinkProvider` removal did not fix it
- [ ] Semantic tokens are correctly per-segment but don't override YAML's PSI

## Workaround Options

### Option 1: Custom File Type Registration (Most Promising)

Register KRO RGD files as a **distinct file type** so the YAML plugin doesn't claim them.

**Sub-options:**

**A) Distinct extension (`.kro.yaml` or `.rg.yaml`)**
- Simplest approach
- Register a TextMate grammar or plain language for this extension
- LSP4IJ's semantic token FileViewProvider kicks in
- Downside: users rename files or configure associations

**B) Content-based file type detection**
- IntelliJ supports `FileTypeDetector` that can sniff content
- Detect `kind: ResourceGraphDefinition` and claim as custom type
- Works on plain `.yaml` files
- More complex to implement

**C) Language injection**
- IntelliJ supports injecting a language into a region of another file
- Could inject a custom "CEL" language into YAML string values matching `${...}`
- The injected language's tokens would drive the underline
- Moderate complexity, very targeted

### Option 2: TextMate Grammar Bundle

LSP4IJ supports bundling a TextMate grammar with the LSP server configuration. If the TextMate grammar claims the file first, its tokens become the PSI leaves.

- Define a `.tmLanguage.json` that tokenizes CEL expressions within YAML
- Each segment (`schema`, `spec`, `name`) gets its own TextMate scope
- Each TextMate token = one PSI leaf = one underline unit

**Challenge:** Need to either:
- Convince IntelliJ to prefer the TextMate grammar over the YAML plugin for our files
- Or use a custom file extension so there's no conflict

### Option 3: IntelliJ Plugin with Custom Annotator

Write a thin IntelliJ plugin that:
1. Registers a `GotoDeclarationHandler` for YAML files with KRO content
2. Overrides the underline range computation
3. Delegates the actual definition resolution to the LSP server

The plugin's `GotoDeclarationHandler` can return targets AND control the range via `PsiElement` tricks (creating fake "lightweight" PSI elements scoped to just the segment).

### Option 4: Custom `TargetElementEvaluator`

LSP4IJ already has `LSPTargetElementEvaluator`. IntelliJ's `TargetElementEvaluatorEx2` has methods like `getElementByReference` that can narrow the "interesting" element. If we can get this evaluator into the CtrlMouse path, we could narrow the range.

**Status:** The existing `LSPTargetElementEvaluator` uses `getWordRangeAt()` which treats dots as separators (`Character.isJavaIdentifierPart('.')` is false). But this evaluator is currently only consulted in Find Usages, NOT in CtrlMouseHandler. Might be possible to wire it in.

### Option 5: Disable YAML Plugin for RGD Files (Nuclear)

IntelliJ allows disabling plugins per-project or configuring file type associations. Users could:
1. Associate `.yaml` files in their KRO project with "Plain Text"
2. Let LSP4IJ's semantic token provider handle tokenization

**Downside:** Loses all YAML IntelliJ features (folding, structure view, etc.)

### Option 6: Fork/Patch LSP4IJ

LSP4IJ is open source. We could:
1. Modify the GotoDeclaration handler to check `originSelectionRange` from cached definition results
2. Override `getReferenceRanges` with the origin range when available
3. Submit as PR to LSP4IJ

**This is the proper fix** and benefits the entire LSP4IJ ecosystem.

## Recommendation

**Short term:** Option 3 (thin IntelliJ plugin) or Option 2 (TextMate grammar with custom extension)

**Long term:** Option 6 (contribute to LSP4IJ) — this is a legitimate gap in their implementation. The LSP spec explicitly says `originSelectionRange` controls the underline span. Filing an issue + PR is the right move.

**Quickest experiment:** Try Option 2 — create a TextMate grammar that scopes CEL expression segments, register it for `.kro.yaml` or for files matching `kind: ResourceGraphDefinition`, and see if the underline narrows.

## References

- LSP4IJ source: https://github.com/redhat-developer/lsp4ij
- LSP4IJ semantic tokens FileViewProvider: PR #882 (merged March 2025)
- IntelliJ YAML plugin: bundled, source in intellij-community
- LSP spec on `originSelectionRange`: https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#locationLink
- IntelliJ `CtrlMouseHandler2`: platform source, `platform/lang-impl`

## Key Insight

The underline is determined by **which plugin owns the file type's PSI**. If we can get LSP4IJ (or our own plugin) to own the PSI for RGD files — even partially via language injection — we control the token boundaries and thus the underline granularity.
