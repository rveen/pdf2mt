package pdf2mt

import (
	"strings"
	"testing"
)

// Malformed rows with trailing comma (empty last field) are dropped.
func TestFixTOON_dropTrailingCommaRows(t *testing.T) {
	input := "name[30]{standard,title}:\n" +
		"  SAE J1715,Hybrid Electric Vehicle (HEV) & Electric Vehicle (EV) Terminology\n" +
		"  Format Guidelines Manual for the Electronic Capture of SAE Ground Vehicle Documents,\n" +
		"  SAE Committee Guidelines Manual,\n" +
		"  SAE J2574,Fuel Cell Vehicle Terminology\n"
	got := fixTOON(input)
	if strings.Contains(got, "Format Guidelines Manual") {
		t.Errorf("malformed row should be dropped, got:\n%s", got)
	}
	if strings.Contains(got, "SAE Committee Guidelines Manual,\n") {
		t.Errorf("malformed row should be dropped, got:\n%s", got)
	}
	if !strings.Contains(got, "SAE J2574") {
		t.Errorf("valid row must be kept, got:\n%s", got)
	}
	if !strings.Contains(got, "name[2]{standard,title}:") {
		t.Errorf("count should reflect 2 remaining valid rows, got:\n%s", got)
	}
}

// Pipe-separator header {f1|f2} is recognized, rows preserved, [N] count updated.
func TestFixTOON_pipeSeparatorHeader(t *testing.T) {
	input := "std[N]{standard|title}:\n" +
		"  ANSI/IEEE C62.41 | Surge Voltages in Low-Voltage AC Power Circuits\n" +
		"  ANSI/IEEE C62.45 | Recommended Practice on Surge Testing\n"
	got := fixTOON(input)
	if !strings.Contains(got, "std[2]{standard|title}:") {
		t.Errorf("pipe header with updated count expected, got:\n%s", got)
	}
	if !strings.Contains(got, "ANSI/IEEE C62.41 | Surge Voltages") {
		t.Errorf("pipe row should be preserved as-is, got:\n%s", got)
	}
}

// Two adjacent pipe-separator blocks with the same field count merge into one.
func TestFixTOON_mergePipeSeparatorBlocks(t *testing.T) {
	input := "std[1]{standard|title}:\n" +
		"  ANSI/IEEE C62.41 | Surge Voltages in Low-Voltage AC Power Circuits\n" +
		"\n" +
		"std[1]{standard|title}:\n" +
		"  ANSI/IEEE C62.45 | Recommended Practice on Surge Testing\n"
	got := fixTOON(input)
	if !strings.Contains(got, "std[2]{standard|title}:") {
		t.Errorf("adjacent pipe blocks should merge, got:\n%s", got)
	}
	if strings.Count(got, "std[") != 1 {
		t.Errorf("merged output should have exactly one TOON header, got:\n%s", got)
	}
}

// Same field count but different separators ({a,b} vs {a|b}) must NOT merge.
func TestFixTOON_noMergeAcrossSeparatorMismatch(t *testing.T) {
	input := "tbl[2]{a,b}:\n" +
		"  x,y\n" +
		"  p,q\n" +
		"\n" +
		"tbl[1]{a|b}:\n" +
		"  foo | bar with, comma\n"
	got := fixTOON(input)
	if !strings.Contains(got, "tbl[2]{a,b}:") {
		t.Errorf("comma block should be flushed with its own count, got:\n%s", got)
	}
	if !strings.Contains(got, "tbl[1]{a|b}:") {
		t.Errorf("pipe block should be separate, got:\n%s", got)
	}
}

// Pipe row with empty last field (trailing " |") is dropped.
func TestFixTOON_dropMalformedPipeRow(t *testing.T) {
	input := "tbl[2]{a|b}:\n" +
		"  good | value\n" +
		"  bad row |\n"
	got := fixTOON(input)
	if strings.Contains(got, "bad row") {
		t.Errorf("malformed pipe row should be dropped, got:\n%s", got)
	}
	if !strings.Contains(got, "tbl[1]{a|b}:") {
		t.Errorf("count should reflect 1 remaining row, got:\n%s", got)
	}
}

// A standalone "foo | bar" line after a comma-mode TOON block is NOT absorbed.
func TestFixTOON_noAbsorbStandalonePipe(t *testing.T) {
	input := "name[1]{id,title}:\n" +
		"  ISO 1234,Some title\n" +
		"\n" +
		"ISO 5678 | Another title\n"
	got := fixTOON(input)
	if !strings.Contains(got, "name[1]{id,title}:") {
		t.Errorf("comma block should have count 1, got:\n%s", got)
	}
	if !strings.Contains(got, "ISO 5678 | Another title") {
		t.Errorf("standalone pipe line should remain as prose, got:\n%s", got)
	}
}
