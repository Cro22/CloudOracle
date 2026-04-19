package report

import (
	"CloudOracle/internal/shared"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

func ExportJSON(w io.Writer, findings []shared.Finding) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if findings == nil {
		findings = []shared.Finding{}
	}
	if err := enc.Encode(findings); err != nil {
		return fmt.Errorf("encoding findings as JSON: %w", err)
	}
	return nil
}

var csvHeader = []string{
	"resource_id",
	"service",
	"resource_type",
	"region",
	"rule",
	"severity",
	"monthly_cost",
	"monthly_savings",
	"description",
	"recommendation",
}

func ExportCSV(w io.Writer, findings []shared.Finding) error {
	writer := csv.NewWriter(w)
	if err := writer.Write(csvHeader); err != nil {
		return fmt.Errorf("writing CSV header: %w", err)
	}

	for _, f := range findings {
		row := []string{
			f.ResourceID,
			f.Service,
			f.ResourceType,
			f.Region,
			f.Rule,
			string(f.Severity),
			strconv.FormatFloat(f.MonthlyCost, 'f', 2, 64),
			strconv.FormatFloat(f.MonthlySavings, 'f', 2, 64),
			f.Description,
			f.Recommendation,
		}
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("writing CSV row for %s: %w", f.ResourceID, err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flushing CSV: %w", err)
	}
	return nil
}
