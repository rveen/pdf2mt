package pdf2mt

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	pdfium "github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
)

// rasterizePages converts every page of the PDF to a PNG file in tmpDir.
// Tries pdftoppm first; falls back to go-pdfium (WASM, CGo-free) if not on PATH.
func rasterizePages(pdfPath string, dpi int, tmpDir string) ([]string, error) {
	if _, err := exec.LookPath("pdftoppm"); err == nil {
		return rasterizeWithPdftoppm(pdfPath, dpi, tmpDir)
	}
	return rasterizeAllWithPdfium(pdfPath, dpi, tmpDir)
}

// rasterizePage rasterizes a single page (1-based) and returns PNG bytes.
// Tries pdftoppm first; falls back to go-pdfium (WASM, CGo-free) if not on PATH.
func rasterizePage(pdfPath string, dpi, pageNum int, tmpDir string) ([]byte, error) {
	if _, err := exec.LookPath("pdftoppm"); err == nil {
		return rasterizeOneWithPdftoppm(pdfPath, dpi, pageNum, tmpDir)
	}
	return rasterizeOneWithPdfium(pdfPath, dpi, pageNum)
}

// ---- pdftoppm backend ----

func rasterizeWithPdftoppm(pdfPath string, dpi int, tmpDir string) ([]string, error) {
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

func rasterizeOneWithPdftoppm(pdfPath string, dpi, pageNum int, tmpDir string) ([]byte, error) {
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

// ---- go-pdfium WASM backend ----

var (
	pdfiumOnce     sync.Once
	pdfiumPool     pdfium.Pool
	pdfiumInstance pdfium.Pdfium
	pdfiumInitErr  error
)

func initPdfium() error {
	pdfiumOnce.Do(func() {
		pdfiumPool, pdfiumInitErr = webassembly.Init(webassembly.Config{
			MinIdle:  1,
			MaxIdle:  1,
			MaxTotal: 1,
		})
		if pdfiumInitErr != nil {
			return
		}
		pdfiumInstance, pdfiumInitErr = pdfiumPool.GetInstance(30 * time.Second)
	})
	return pdfiumInitErr
}

func rasterizeAllWithPdfium(pdfPath string, dpi int, tmpDir string) ([]string, error) {
	if err := initPdfium(); err != nil {
		return nil, fmt.Errorf("pdfium init: %w", err)
	}

	pdfBytes, err := os.ReadFile(pdfPath)
	if err != nil {
		return nil, err
	}

	doc, err := pdfiumInstance.OpenDocument(&requests.OpenDocument{File: &pdfBytes})
	if err != nil {
		return nil, fmt.Errorf("pdfium open: %w", err)
	}
	defer pdfiumInstance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})

	countResp, err := pdfiumInstance.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return nil, fmt.Errorf("pdfium page count: %w", err)
	}

	var paths []string
	for i := 0; i < countResp.PageCount; i++ {
		rendered, err := pdfiumInstance.RenderPageInDPI(&requests.RenderPageInDPI{
			DPI: dpi,
			Page: requests.Page{
				ByIndex: &requests.PageByIndex{
					Document: doc.Document,
					Index:    i, // 0-based
				},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("pdfium render page %d: %w", i+1, err)
		}

		p := filepath.Join(tmpDir, fmt.Sprintf("page-%04d.png", i+1))
		f, err := os.Create(p)
		if err != nil {
			rendered.Cleanup()
			return nil, err
		}
		encErr := png.Encode(f, rendered.Result.Image)
		rendered.Cleanup() // MUST be called before next render to release WASM memory
		f.Close()
		if encErr != nil {
			return nil, encErr
		}
		paths = append(paths, p)
	}
	return paths, nil
}

func rasterizeOneWithPdfium(pdfPath string, dpi, pageNum int) ([]byte, error) {
	if err := initPdfium(); err != nil {
		return nil, fmt.Errorf("pdfium init: %w", err)
	}

	pdfBytes, err := os.ReadFile(pdfPath)
	if err != nil {
		return nil, err
	}

	doc, err := pdfiumInstance.OpenDocument(&requests.OpenDocument{File: &pdfBytes})
	if err != nil {
		return nil, fmt.Errorf("pdfium open: %w", err)
	}
	defer pdfiumInstance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})

	rendered, err := pdfiumInstance.RenderPageInDPI(&requests.RenderPageInDPI{
		DPI: dpi,
		Page: requests.Page{
			ByIndex: &requests.PageByIndex{
				Document: doc.Document,
				Index:    pageNum - 1, // 1-based → 0-based
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("pdfium render page %d: %w", pageNum, err)
	}
	defer rendered.Cleanup()

	var buf bytes.Buffer
	if err := png.Encode(&buf, rendered.Result.Image); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
