# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## About this module

`pdf2mt` converts PDFs to token-efficient hybrid documents and defines the canonical LLM-optimized data format specification.

The library package is `pdf2mt` (`convert.go`). The CLI command lives in `pdf2mt/main.go` and imports `golib/ai/pdf2mt`.

### CLI usage

```
pdf2mt [flags] input.pdf
  -o string       output file (default: stdout)
  -dpi int        rasterization DPI (default 150)
  -model string   Claude model (default claude-sonnet-4-6)
  -images         extract figures as PNG files alongside the output
  -image-dpi int  rasterization DPI for figure extraction
```

Requires `ANTHROPIC_API_KEY` in the environment. Pages are rasterized with `pdftoppm` (poppler-utils) and sent to Claude vision one at a time.

## Specification overview (`llm-data-format.md`)

The spec defines a hybrid strategy for encoding data sent to LLMs, optimizing for token efficiency. The core principle: **field names appear once per table, never once per row.**

### Format decision rules

| Situation | Format |
|---|---|
| Document hierarchy / section grouping | Markdown `##` / `###` headers |
| Uniform array, any size, no commas in values | TOON `name[N]{f1,f2}:` |
| Uniform array, any size, commas in values | TOON `name[N]{f1|f2}:` with pipe rows |
| Document title-page metadata | Bare pipe rows (no header line) |
| Deeply nested / irregular structure | Minified JSON or YAML |
| Pure flat table, no context needed | CSV |

### TOON format

TOON is the universal format for all uniform tabular data, regardless of row count. Declare the schema once in a header, then stream values. Choose the separator based on whether any value contains a comma:

**Comma separator** (default):
```
requirements[4]{id,level,description,actions}:
  T-01,*,Req 1, A1; A2
  T-02,B,Req 2, A3; A4; A5
  T-03,A,Req 3, No action needed
  T-04,C,Req 4, A6
```

**Pipe separator** — use `|` between field names when any value contains a comma:
```
std[3]{standard|title}:
  ISO/TC 165/SC 1 | Wood materials -- Durability and preservation
  ISO/TC 165/WG 10 | Characteristic values and design specifications
  ISO/TC 165/WG 11 | Solid and mechanically laminated timber products
```

- `[N]` = exact row count (helps the model anchor its schema)
- The separator between field names in `{fields}` dictates the row separator: `{f1,f2}` → comma rows, `{f1|f2}` → pipe rows
- No escape sequences — use pipe separator instead of `\,`
- Token savings vs. pretty-printed JSON: 30–60% for large uniform arrays

### What to avoid

- **XML** — worst token efficiency
- **Pretty-printed JSON** — field names repeat per row
- **Markdown tables** — `|---|---|` alignment row adds overhead with no LLM benefit
- **Escape sequences (`\,`)** — use pipe-separator TOON variant instead
- Describing schema in prose when a TOON header already declares it

### Caveat

TOON benchmarks used the GPT-5 tokenizer (`o200k_base`). Claude and other models tokenize differently — validate savings against a representative sample before committing at scale.

## Specification files

- `llm-data-format.md` — primary human-readable specification
- `llm-data-format-rfc.txt` — RFC-style formal specification of the same format
