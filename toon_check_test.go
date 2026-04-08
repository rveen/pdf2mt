package pdf2mt

import (
	"strings"
	"testing"
)

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

// A standalone "foo | bar" line after a TOON block is NOT absorbed as a data row.
func TestFixTOON_noAbsorbStandalonePipe(t *testing.T) {
	input := "name[1]{id|title}:\n" +
		"  ISO 1234 | Some title\n" +
		"\n" +
		"ISO 5678 | Another title\n"
	got := fixTOON(input)
	if !strings.Contains(got, "name[1]{id|title}:") {
		t.Errorf("pipe block should have count 1, got:\n%s", got)
	}
	if !strings.Contains(got, "ISO 5678 | Another title") {
		t.Errorf("standalone pipe line should remain as prose, got:\n%s", got)
	}
}

// Document metadata encoded as a TOON block is processed like any other block.
func TestFixTOON_metadataTOONBlock(t *testing.T) {
	input := "# My Document\n" +
		"meta[N]{key|value}:\n" +
		"  Issued | 2020-01\n" +
		"  Revised | 2023-06\n" +
		"  Superseding | DOC-001\n"
	got := fixTOON(input)
	if !strings.Contains(got, "meta[3]{key|value}:") {
		t.Errorf("metadata TOON block count should be updated to 3, got:\n%s", got)
	}
	if !strings.Contains(got, "Issued | 2020-01") {
		t.Errorf("metadata rows should be preserved, got:\n%s", got)
	}
}
