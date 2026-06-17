# gofmt Blank Line Handling - Complete Research

## Executive Summary

gofmt uses a **preserve and normalize** approach:
- Preserves 0 or 1 blank lines from source
- Normalizes 2+ blank lines to 1 blank line
- Uses `maxNewlines = 2` constant (max 1 blank line in output)
- May enforce minimum blank lines in certain structural contexts

## Key Source Code

**File**: `go/printer/printer.go` (Go 1.26.2)

### The Magic Constant

```go
const maxNewlines = 2     // max. number of newlines between source text
```

### Core Functions

```go
// nlimit limits n to maxNewlines.
func nlimit(n int) int {
    return min(n, maxNewlines)
}
```

Applied in main printing loop (lines 1006-1019):
```go
// intersperse extra newlines if present in the source and
// if they don't cause extra semicolons
if !p.impliedSemi {
    n := nlimit(next.Line - p.pos.Line)
    // don't exceed maxNewlines if we already wrote one
    if wroteNewline && n == maxNewlines {
        n = maxNewlines - 1
    }
    if n > 0 {
        ch := byte('\n')
        if droppedFF {
            ch = '\f'
        }
        p.writeByte(ch, n)
        impliedSemi = false
    }
}
```

### Structural Blank Lines

**File**: `go/printer/nodes.go`

The `linebreak` function enforces minimum spacing:

```go
func (p *printer) linebreak(line, min int, ws whiteSpace, newSection bool) (nbreaks int) {
    n := max(nlimit(line-p.pos.Line), min)
    // ...
}
```

Used in `declList` (lines 1965-1990):
```go
min := 1
if prev != tok || getDoc(d) != nil {
    min = 2  // enforce blank line between declaration types or before docs
}
p.linebreak(p.lineFor(d.Pos()), min, ignore, tok == token.FUNC && p.numLines(d) > 1)
```

## Algorithm Breakdown

### Step 1: Calculate Source Spacing
```go
n := next.Line - p.pos.Line  // number of line breaks in source
```

### Step 2: Apply Maximum Limit
```go
n = nlimit(n)  // cap at maxNewlines (2)
```

### Step 3: Apply Minimum Requirements
```go
n = max(n, min)  // ensure structural minimums
```

### Step 4: Output
```go
// Write n newline characters
// n=1 → 0 blank lines (consecutive)
// n=2 → 1 blank line
```

## Relationship: Newlines vs Blank Lines

```
Newlines  │ Blank Lines │ Visual
─────────┼─────────────┼────────
    1     │      0      │ line1
          │             │ line2
─────────┼─────────────┼────────
    2     │      1      │ line1
          │             │ (blank)
          │             │ line2
─────────┼─────────────┼────────
    3     │      2      │ line1
          │             │ (blank)
          │             │ (blank)
          │             │ line2
```

**Formula**: `blank_lines = newlines - 1`

Therefore: `maxNewlines=2` → `max_blank_lines=1`

## Test Results

### Preservation
```go
// 1 blank line → preserved
func test1() {}

func test2() {}  // ✓ 1 blank preserved
```

### Normalization
```go
// 3 blank lines → normalized to 1
func test3() {}



func test4() {}

// Formatted:
func test3() {}

func test4() {}  // ✓ normalized to 1 blank
```

### Zero blanks (consecutive lines)
```go
// Inside functions - preserved
func test() {
    x := 1
    y := 2  // ✓ consecutive lines preserved
}
```

## Implementation for YAML

To replicate gofmt's behavior:

```go
const maxBlankLines = 1  // equivalent to maxNewlines=2 in gofmt

func normalizeBlankLines(sourceBlankCount int) int {
    return min(sourceBlankCount, maxBlankLines)
}
```

### Practical Rules

1. **Preserve 0 blank lines** (don't add blanks that weren't there)
2. **Preserve 1 blank line** (keep it)
3. **Collapse 2+ blank lines** to 1 blank line
4. **Never insert** blank lines not in source (unless structural rules require it)

### Algorithm

```
For each pair of adjacent elements:
1. Count blank lines between them in source
2. If count == 0: output 0 blanks
3. If count >= 1: output 1 blank (normalized)
```

Pseudo-code:
```go
if sourceBlanks == 0 {
    writeBlanks(0)
} else {
    writeBlanks(1)  // normalize any non-zero to 1
}
```

Or simply:
```go
writeBlanks(min(sourceBlanks, 1))
```

## Key Insights

1. **Preservation, not insertion**: gofmt preserves spacing intent from source
2. **Normalization boundary**: The line is drawn at 1 blank line
3. **Structural minimums**: Some contexts enforce minimums (e.g., between import and declarations)
4. **The constant is about newlines**: `maxNewlines=2` is counting `\n` characters, not blank lines
5. **Simplicity**: The algorithm is remarkably simple - just `min(n, maxNewlines)`

## Source File Locations

- Main logic: `/home/nblaskey/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.2.linux-amd64/src/go/printer/printer.go`
  - Line 22: `const maxNewlines = 2`
  - Line 863-865: `nlimit()` function
  - Line 1007: Application in print loop
  
- Structural logic: `/home/nblaskey/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.2.linux-amd64/src/go/printer/nodes.go`
  - Line 46: `linebreak()` function
  - Line 1965: `declList()` function with min=2 logic

## Conclusion

gofmt's blank line handling is a **normalization strategy** with a clear threshold:
- Keep 0 or 1 blank lines as-is
- Normalize 2+ to 1

This creates consistency without being overly aggressive about inserting blanks everywhere.
