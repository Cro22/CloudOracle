package report

import (
	"CloudOracle/internal/shared"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
)

func exportSampleFindings() []shared.Finding {
	return []shared.Finding{
		{
			ResourceID:     "i-1",
			Service:        "ec2",
			ResourceType:   "c5.xlarge",
			Region:         "us-east-2",
			Rule:           "ec2-idle",
			Severity:       shared.SeverityHigh,
			MonthlyCost:    125.50,
			MonthlySavings: 125.50,
			Description:    "CPU <5%",
			Recommendation: "Shut down",
		},
		{
			ResourceID:     "vol-2",
			Service:        "ebs",
			ResourceType:   "gp3",
			Region:         "us-east-2",
			Rule:           "ebs-orphan",
			Severity:       shared.SeverityMedium,
			MonthlyCost:    10.0,
			MonthlySavings: 10.0,
			Description:    "Unattached",
			Recommendation: "Delete, after snapshot",
		},
	}
}

func TestExportJSON_HappyPath(t *testing.T) {
	var buf bytes.Buffer
	if err := ExportJSON(&buf, exportSampleFindings()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded []shared.Finding
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(decoded) != 2 {
		t.Errorf("expected 2 findings, got %d", len(decoded))
	}
	if decoded[0].ResourceID != "i-1" {
		t.Errorf("unexpected first ResourceID: %s", decoded[0].ResourceID)
	}
	if decoded[0].Severity != shared.SeverityHigh {
		t.Errorf("unexpected severity: %s", decoded[0].Severity)
	}
}

func TestExportJSON_EmptyProducesArray(t *testing.T) {
	var buf bytes.Buffer
	if err := ExportJSON(&buf, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if got != "[]" {
		t.Errorf("expected empty array '[]', got %q", got)
	}
}

func TestExportCSV_HeaderAndRows(t *testing.T) {
	var buf bytes.Buffer
	if err := ExportCSV(&buf, exportSampleFindings()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reader := csv.NewReader(&buf)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to parse CSV: %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 rows (header + 2 data), got %d", len(records))
	}

	if records[0][0] != "resource_id" {
		t.Errorf("unexpected first header: %s", records[0][0])
	}
	if records[1][0] != "i-1" {
		t.Errorf("unexpected first data row ID: %s", records[1][0])
	}
	if records[1][6] != "125.50" {
		t.Errorf("unexpected monthly_cost formatting: %s", records[1][6])
	}
}

func TestExportCSV_EscapesCommasAndQuotes(t *testing.T) {
	findings := []shared.Finding{{
		ResourceID:     "r-1",
		Service:        "ec2",
		Description:    `value with, comma and "quote"`,
		Recommendation: "line1\nline2",
	}}

	var buf bytes.Buffer
	if err := ExportCSV(&buf, findings); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Round-trip: if encoding/csv can parse it back identically, escaping is correct.
	reader := csv.NewReader(&buf)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("CSV with special chars is not parseable: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected header + 1 row, got %d", len(records))
	}
	if records[1][8] != `value with, comma and "quote"` {
		t.Errorf("description round-trip failed: got %q", records[1][8])
	}
	if records[1][9] != "line1\nline2" {
		t.Errorf("newline round-trip failed: got %q", records[1][9])
	}
}

func TestExportCSV_EmptyWritesHeaderOnly(t *testing.T) {
	var buf bytes.Buffer
	if err := ExportCSV(&buf, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reader := csv.NewReader(&buf)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to parse CSV: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected header-only (1 row), got %d rows", len(records))
	}
	if len(records[0]) != len(csvHeader) {
		t.Errorf("expected %d columns, got %d", len(csvHeader), len(records[0]))
	}
}
