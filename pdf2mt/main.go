// Command pdf2mt converts a PDF to a token-efficient hybrid document.
//
// Usage:
//
//	pdf2mt [flags] input.pdf
//
// Flags:
//
//	-o string      output file (default: stdout)
//	-dpi int       rasterization DPI (default 150)
//	-model str     Claude model (default claude-sonnet-4-6)
//	-images        extract figures as PNG files alongside the output
//	-image-dpi int rasterization DPI for figure extraction (default: same as -dpi)
//
// Requires ANTHROPIC_API_KEY in the environment.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	pdf2mt "github.com/rveen/pdf2mt"
)

var Usage = func() {
	fmt.Fprintf(flag.CommandLine.Output(), "PDF to MT (Markdown+TOON) converter\nUsage of %s:\n", os.Args[0])
	flag.PrintDefaults()
	fmt.Fprintln(flag.CommandLine.Output(), "\n  The ANTHROPIC_API_KEY must be set in the environment.")
}

func main() {
	var (
		outputFile = flag.String("o", "", "output file (default: stdout)")
		dpi        = flag.Int("dpi", 0, "rasterization DPI (default 150)")
		model      = flag.String("model", "", "Claude model (default claude-sonnet-4-6)")
		images     = flag.Bool("images", false, "extract figures as PNG files alongside the output")
		imageDPI   = flag.Int("image-dpi", 0, "rasterization DPI for figure extraction (default: same as -dpi)")
	)

	flag.Usage = Usage
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "PDF to MT (Markdown+TOON) converter\nUsage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr, "\n  The ANTHROPIC_API_KEY must be set in the environment.")
		os.Exit(1)
	}
	
	if *outputFile == "" {
		fmt.Fprintln(os.Stderr, "Set an output file with -o (otherwise you could loose your money)")
		os.Exit(1)
	}

	pdfPath := flag.Arg(0)
	opts := &pdf2mt.Options{
		DPI:           *dpi,
		Model:         *model,
		OutputFile:    *outputFile,
		ExtractImages: *images,
		ImageDPI:      *imageDPI,
	}

	result, err := pdf2mt.ConvertPDF(context.Background(), pdfPath, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pdf2mt: %v\n", err)
		os.Exit(1)
	}

	if *outputFile == "" {
		fmt.Print(result)
	}
}
