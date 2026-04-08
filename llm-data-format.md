# Optimized data format for LLM input

## Summary

No single format is optimal across all cases. The best approach is a hybrid: **Markdown for structure and context, TOON blocks for all tabular data.** The guiding principle is: *field names appear once per table, never once per row.*

## Format roles

### Markdown headers

Use `#`, `##`, `###` (and so on), for document hierarchy and section context. Headers are natural-language anchors that cost almost nothing in tokens and give the model structural orientation before it encounters data.

### TOON tabular blocks

Use for any uniform array of objects with a fixed schema, regardless of row count. Declare field names once in the header, stream values as rows. Two separator variants are defined:

**Comma separator** (when no values contain commas):

```
requirements[4]{id,level,description,actions}:
  T-01,*,Req 1, A1; A2
  T-02,B,Req 2, A3; A4; A5
  T-03,A,Req 3, No action needed
  T-04,C,Req 4, A6
```

**Pipe separator** (when any value in the block contains a comma — use `|` between field names too):

```
std[2]{standard|title}:
  ISO/TC 165/SC 1 | Wood materials -- Durability and preservation
  ISO/TC 165/WG 10 | Characteristic values and design specifications
  ISO/TC 165/WG 11 | Solid and mechanically laminated timber products
```

Rules:
- `[N]` = exact integer row count (never a placeholder)
- The separator used between field names in `{fields}` is the same separator used in every data row — `{f1,f2}` means comma-separated rows, `{f1|f2}` means pipe-separated rows
- Choose the separator for the whole block based on whether **any** value contains a comma; never mix separators within a block
- No escape sequences — use the pipe separator instead of `\,`
- Token savings vs. JSON: 30–60% for large uniform arrays; the `[N]` count and `{fields}` header also improve parsing accuracy

> **pdf2mt canonical output:** The reference producer (`pdf2mt`) always emits pipe-separator TOON, regardless of whether values contain commas. This eliminates the separator-selection decision for LLM-generated output, avoiding a common source of format errors. Parsers MUST accept both separators; producers MAY restrict themselves to pipe-only.

### Document metadata

Document title-page metadata (title, issue date, revision, superseding) is encoded as a TOON block, like any other table:

```
# Document Title
meta[3]{key|value}:
  Issued | 1998-06
  Revised | 2010-03
  Superseding | J2344 JUN1998
```


## What to avoid

- **XML**: worst token efficiency of all formats; avoid entirely.
- **Pretty-printed JSON**: field names repeat per row; high token waste for arrays.
- **Markdown tables**: the `|---|---|` alignment row and repeated header add overhead with no benefit over TOON for LLM consumption.
- **Escape sequences (`\,`)**: use the pipe-separator TOON variant (`{f1|f2}`) instead — it is cleaner for the model to parse.
- **Describing the schema in prose per section**: redundant if the TOON header already declares it.
- **Standalone pipe-delimited rows with `# schema` comment lines**: deprecated; use TOON blocks instead.


