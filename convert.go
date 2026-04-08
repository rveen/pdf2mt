// Package pdf2mt converts a PDF to a token-efficient hybrid document following
// the LLM Data Format strategy defined in llm-data-format.md:
//
//   - Markdown headers for section hierarchy
//   - TOON blocks for all uniform arrays, comma separator by default:
//     name[N]{f1,f2}: / pipe separator when values contain commas: name[N]{f1,f2}(|):
//   - Plain Markdown prose for narrative text
//
// Conversion is done via the Claude API with vision input (one page at a time).
// Pages are rasterized using pdftoppm (from poppler-utils).
//
// Environment:
//
//	ANTHROPIC_API_KEY  — required
package pdf2mt

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Options controls conversion behaviour.
type Options struct {
	// DPI used when rasterizing pages (default 150).
	DPI int
	// Model is the Claude model (default "claude-sonnet-4-6").
	Model string
	// OutputFile, if set, writes the resulting document to this path.
	OutputFile string
	// ExtractImages, if true, extracts figures that cannot be represented as
	// text or a table and writes them as PNG files alongside the output document.
	// Each extracted figure replaces its "> [Figure N: ...]" marker with a
	// Markdown image link "![Figure N: ...](filename.png)".
	ExtractImages bool
	// ImageDPI, if non-zero, overrides DPI when rasterizing pages for figure
	// extraction. Use a higher value (e.g. 300) for better image quality.
	// Has no effect when ExtractImages is false. Defaults to DPI.
	ImageDPI int
}

func (o *Options) dpi() int {
	if o == nil || o.DPI == 0 {
		return 150
	}
	return o.DPI
}

func (o *Options) imageDPI() int {
	if o == nil || o.ImageDPI == 0 {
		return o.dpi()
	}
	return o.ImageDPI
}

func (o *Options) model() string {
	if o == nil || o.Model == "" {
		return "claude-sonnet-4-6"
	}
	return o.Model
}

// figurePrefix returns the path prefix used when naming extracted figure files.
// E.g. OutputFile "path/to/out.md" → "path/to/out-fig-"; "" → "fig-".
func (o *Options) figurePrefix() string {
	if o == nil || o.OutputFile == "" {
		return "fig-"
	}
	ext := filepath.Ext(o.OutputFile)
	return strings.TrimSuffix(o.OutputFile, ext) + "-fig-"
}

// ConvertPDF converts the PDF at pdfPath to a token-efficient hybrid document.
// Requires ANTHROPIC_API_KEY in the environment and pdftoppm on PATH.
func ConvertPDF(ctx context.Context, pdfPath string, opts *Options) (string, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY is not set")
	}

	tmpDir, err := os.MkdirTemp("", "pdf2mt-*")
	if err != nil {
		return "", fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	pngPaths, err := rasterizePages(pdfPath, opts.dpi(), tmpDir)
	if err != nil {
		return "", fmt.Errorf("rasterize: %w", err)
	}

	total := len(pngPaths)
	var sections []string
	var prevTail string
	var figNum int

	for i, pngPath := range pngPaths {
		pageNum := i + 1

		pngBytes, err := os.ReadFile(pngPath)
		if err != nil {
			return "", fmt.Errorf("read page %d: %w", pageNum, err)
		}
		pngB64 := base64.StdEncoding.EncodeToString(pngBytes)

		text, err := callClaude(ctx, apiKey, opts.model(), pageNum, total, prevTail, pngB64)
		if err != nil {
			return "", fmt.Errorf("claude page %d: %w", pageNum, err)
		}

		// Optionally extract figures that cannot be represented as text/tables.
		if opts != nil && opts.ExtractImages && reFigureMarker.MatchString(text) {
			imgBytes := pngBytes
			if opts.imageDPI() != opts.dpi() {
				if hires, err := rasterizePage(pdfPath, opts.imageDPI(), pageNum, tmpDir); err == nil {
					imgBytes = hires
				}
			}
			if pageImg, err := png.Decode(bytes.NewReader(imgBytes)); err == nil {
				if bboxes, err := extractFigureBBoxes(ctx, apiKey, opts.model(), pngB64); err == nil {
					markers := reFigureMarker.FindAllString(text, -1)
					prefix := opts.figurePrefix()
					for j, bb := range bboxes {
						if j >= len(markers) {
							break
						}
						figNum++
						fname := fmt.Sprintf("%s%03d.png", prefix, figNum)
						if err := saveFigure(pageImg, bb, fname); err == nil {
							caption := strings.TrimPrefix(strings.TrimSuffix(markers[j], "]"), "> [")
							imgLink := fmt.Sprintf("![%s](%s)", caption, filepath.Base(fname))
							text = strings.Replace(text, markers[j], imgLink, 1)
						}
					}
				}
			}
		}

		sections = append(sections, text)

		// Keep the last 300 chars as context for the next page.
		if len(text) > 300 {
			prevTail = text[len(text)-300:]
		} else {
			prevTail = text
		}
	}

	result := assemble(sections)

	if opts != nil && opts.OutputFile != "" {
		dir := filepath.Dir(opts.OutputFile)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return result, fmt.Errorf("mkdir: %w", err)
			}
		}
		if err := os.WriteFile(opts.OutputFile, []byte(result), 0o644); err != nil {
			return result, fmt.Errorf("write output: %w", err)
		}
	}

	return result, nil
}

// ----------------------------------------------------------------- rasterizer

// rasterizePages converts every page of the PDF to a PNG file in tmpDir using
// pdftoppm (part of poppler-utils). Returns paths sorted by page order.
func rasterizePages(pdfPath string, dpi int, tmpDir string) ([]string, error) {
	prefix := filepath.Join(tmpDir, "page")
	cmd := exec.Command("pdftoppm",
		"-r", strconv.Itoa(dpi),
		"-png",
		pdfPath,
		prefix,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("pdftoppm: %w: %s", err, out)
	}

	matches, err := filepath.Glob(prefix + "-*.png")
	if err != nil || len(matches) == 0 {
		return nil, fmt.Errorf("no pages produced by pdftoppm")
	}
	sort.Strings(matches)
	return matches, nil
}

// rasterizePage rasterizes a single page of the PDF at the given DPI and returns
// the PNG bytes. pageNum is 1-based.
func rasterizePage(pdfPath string, dpi, pageNum int, tmpDir string) ([]byte, error) {
	prefix := filepath.Join(tmpDir, fmt.Sprintf("hires-p%d", pageNum))
	cmd := exec.Command("pdftoppm",
		"-r", strconv.Itoa(dpi),
		"-png",
		"-f", strconv.Itoa(pageNum),
		"-l", strconv.Itoa(pageNum),
		pdfPath,
		prefix,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("pdftoppm: %w: %s", err, out)
	}
	matches, err := filepath.Glob(prefix + "-*.png")
	if err != nil || len(matches) == 0 {
		return nil, fmt.Errorf("no output from pdftoppm for page %d", pageNum)
	}
	return os.ReadFile(matches[0])
}

// ----------------------------------------------------------------- Claude API

const systemPrompt = `You are a document converter that produces token-efficient output for LLM consumption.

## Output format rules

Apply these rules to every page you receive:

1. **Markdown headers** — use standard Markdown heading levels for document hierarchy:
   - # for the document title (see rule 2)
   - ## for top-level chapters or numbered sections (e.g. "## 4 Safety Guidelines")
   - ### for subsections (e.g. "### 4.1.2 High Voltage Isolation")
   - #### and deeper for further nesting, following the document's own numbering depth

2. **Document metadata** (title, issue date, revision, superseding) — emit on page 1 only.
   Use the document title as a single # header, then a TOON block for the metadata:
     # Document Title
     meta[3]{key|value}:
       Issued | 1998-06
       Revised | 2010-03
       Superseding | J2344 JUN1998

3. **TOON blocks** — for ANY uniform list/array of items that share the same fields,
   regardless of row count (even 1–4 rows). Always use pipe separator:
       name[N]{field1|field2|...}:
         value1 | value2 | ...
   Rules:
   - Always use pipe (|) between field names and between row values.
   - N must be the exact integer count of rows in this block (never the letter N).
   - If a list spans a page break, start a fresh TOON block with the same header
     for the remaining rows; they will be merged automatically in post-processing.

4. **Plain Markdown prose** — for narrative paragraphs, guidelines, explanations.
   Preserve all technical detail verbatim. Do not summarize.

5. **Figures** — replace with a single line: > [Figure N: brief description]

## What to omit

Silently skip the following. Produce absolutely no output for them — not even a comment,
placeholder, or note like "(skipped)" or "(Table of Contents page — skipped)":
- Page numbers, running headers, document numbers repeated at the top/bottom of pages
- Decorative horizontal rules and whitespace-only lines
- The Table of Contents and any page whose entire content is a Table of Contents

## Continuation rule

If this is not page 1 (indicated in the user message), do NOT re-emit the document title
or metadata. Continue directly from wherever the previous page left off. If a section
started on the previous page and continues on this one, continue inside that section
without reopening the header unless you are starting a new numbered section.

Output ONLY the converted content — no preamble, no "Here is the conversion:", no fences.`

func callClaude(ctx context.Context, apiKey, model string, pageNum, total int, prevTail, pngB64 string) (string, error) {
	var userText string
	if pageNum == 1 {
		userText = fmt.Sprintf("Page %d of %d. Convert this page.", pageNum, total)
	} else {
		userText = fmt.Sprintf(
			"[continuing from page %d; last output was:\n%s\n]\nPage %d of %d. Convert this page.",
			pageNum-1, prevTail, pageNum, total,
		)
	}

	body := map[string]any{
		"model":      model,
		"max_tokens": 4096,
		"system":     systemPrompt,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "image",
						"source": map[string]any{
							"type":       "base64",
							"media_type": "image/png",
							"data":       pngB64,
						},
					},
					{"type": "text", "text": userText},
				},
			},
		},
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("claude API %d: %s", resp.StatusCode, respBytes)
	}

	var apiResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBytes, &apiResp); err != nil {
		return "", err
	}

	var text string
	for _, block := range apiResp.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	return strings.TrimSpace(text), nil
}

// ----------------------------------------------------------------- figure extraction

// reFigureMarker matches the figure placeholder lines emitted by the main conversion,
// e.g. "> [Figure 1: Hazardous voltage symbol]".
var reFigureMarker = regexp.MustCompile(`(?m)^> \[Figure \d+:[^\]]*\]$`)

const figurePrompt = `You are an image analysis tool. The user will send you a document page.
Return a JSON array of bounding boxes for every figure on the page that
cannot be represented as text or a table (e.g. diagrams, photos, symbols, charts).
Each element must be: {"x0":f,"y0":f,"x1":f,"y1":f} with normalized 0.0–1.0
coordinates, origin top-left, in top-to-bottom order.
If there are no such figures, return [].
Return ONLY the JSON array — no prose, no code fences.`

// figBBox holds normalized (0–1) bounding box coordinates for one figure.
type figBBox struct {
	X0 float64 `json:"x0"`
	Y0 float64 `json:"y0"`
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
}

// extractFigureBBoxes asks Claude to locate all non-text, non-table figures on
// the given page (supplied as a base64 PNG) and returns their bounding boxes.
func extractFigureBBoxes(ctx context.Context, apiKey, model, pngB64 string) ([]figBBox, error) {
	body := map[string]any{
		"model":      model,
		"max_tokens": 512,
		"system":     figurePrompt,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "image",
						"source": map[string]any{
							"type":       "base64",
							"media_type": "image/png",
							"data":       pngB64,
						},
					},
					{"type": "text", "text": "Extract figure bounding boxes from this page."},
				},
			},
		},
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("claude API %d: %s", resp.StatusCode, respBytes)
	}

	var apiResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBytes, &apiResp); err != nil {
		return nil, err
	}
	var text string
	for _, block := range apiResp.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}

	var bboxes []figBBox
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &bboxes); err != nil {
		return nil, nil // non-fatal: model returned something unparseable
	}
	return bboxes, nil
}

// saveFigure crops the given bounding box from pageImg and writes it as a PNG file.
func saveFigure(pageImg image.Image, b figBBox, path string) error {
	cropped := cropRelative(pageImg, b.X0, b.Y0, b.X1, b.Y1)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, cropped)
}

// cropRelative returns the sub-image defined by normalized (0–1) coordinates.
func cropRelative(src image.Image, x0, y0, x1, y1 float64) image.Image {
	b := src.Bounds()
	w := float64(b.Max.X - b.Min.X)
	h := float64(b.Max.Y - b.Min.Y)
	rx0 := b.Min.X + int(x0*w)
	ry0 := b.Min.Y + int(y0*h)
	rx1 := b.Min.X + int(x1*w)
	ry1 := b.Min.Y + int(y1*h)
	if rx1 > b.Max.X {
		rx1 = b.Max.X
	}
	if ry1 > b.Max.Y {
		ry1 = b.Max.Y
	}
	type subImager interface {
		SubImage(image.Rectangle) image.Image
	}
	if s, ok := src.(subImager); ok {
		return s.SubImage(image.Rect(rx0, ry0, rx1, ry1))
	}
	dst := image.NewRGBA(image.Rect(0, 0, rx1-rx0, ry1-ry0))
	for y := ry0; y < ry1; y++ {
		for x := rx0; x < rx1; x++ {
			dst.Set(x-rx0, y-ry0, src.At(x, y))
		}
	}
	return dst
}

// ----------------------------------------------------------------- assembly

// rePageArtifact matches standalone page numbers and SAE document identifiers
// that appear as isolated lines (running headers/footers).
var rePageArtifact = regexp.MustCompile(`(?m)^[ \t]*(?:\d+|SAE J\d+[A-Z]*)[ \t]*$`)

// assemble joins per-page outputs, strips artifacts, and post-processes TOON blocks.
func assemble(sections []string) string {
	var sb strings.Builder
	for i, s := range sections {
		s = rePageArtifact.ReplaceAllString(s, "")
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(s)
	}
	return fixTOON(sb.String()) + "\n"
}

// reTOONHeader matches a TOON block header line, e.g. "name[N]{std|title}:".
// Capture groups: (1) name, (2) count-or-N, (3) fields.
// pdf2mt always emits pipe-separator blocks; field names are separated by "|".
var reTOONHeader = regexp.MustCompile(`^(\w+)\[(\d+|N)\]\{([^}]+)\}:\s*$`)

// fixTOON post-processes the assembled text:
//   - Merges adjacent TOON blocks with the same field count (page-boundary splits).
//   - Drops malformed rows whose last field is empty (trailing " |").
//   - Replaces every [N] placeholder and any stale count with the real row count.
func fixTOON(text string) string {
	type toon struct {
		name   string
		fields string
		nf     int
		rows   []string
	}

	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	var cur *toon
	var pendingBlanks []string

	flush := func() {
		if cur == nil {
			return
		}
		valid := cur.rows[:0]
		for _, r := range cur.rows {
			if strings.HasSuffix(strings.TrimSpace(r), " |") {
				continue // trailing " |" means empty last field
			}
			valid = append(valid, r)
		}
		cur.rows = valid
		if len(cur.rows) > 0 {
			out = append(out, fmt.Sprintf("%s[%d]{%s}:", cur.name, len(cur.rows), cur.fields))
			out = append(out, cur.rows...)
		}
		cur = nil
	}

	for _, line := range lines {
		if m := reTOONHeader.FindStringSubmatch(line); m != nil {
			name, fields := m[1], m[3]
			nf := strings.Count(fields, "|") + 1

			if cur != nil && cur.nf == nf {
				// Same field count — continuation block; drop inter-block blanks.
				pendingBlanks = nil
			} else {
				flush()
				out = append(out, pendingBlanks...)
				pendingBlanks = nil
				cur = &toon{name: name, fields: fields, nf: nf}
			}
		} else if cur != nil && strings.HasPrefix(line, "  ") {
			// TOON data row.
			out = append(out, pendingBlanks...)
			pendingBlanks = nil
			cur.rows = append(cur.rows, line)
		} else if cur != nil && strings.TrimSpace(line) == "" {
			// Blank line — may be a gap between adjacent blocks.
			pendingBlanks = append(pendingBlanks, line)
		} else if cur != nil {
			// Non-row, non-blank content while a TOON block is open — flush and emit.
			flush()
			out = append(out, pendingBlanks...)
			pendingBlanks = nil
			out = append(out, line)
		} else {
			out = append(out, pendingBlanks...)
			pendingBlanks = nil
			out = append(out, line)
		}
	}
	flush()
	out = append(out, pendingBlanks...)

	return strings.Join(out, "\n")
}
