package importexport

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

const PreviewLimit = 20

// ParseFile detects CSV vs XLSX from content type / filename and parses the sheet.
func ParseFile(fileName, contentType string, data []byte) (*ParsedSheet, error) {
	name := strings.ToLower(fileName)
	ct := strings.ToLower(contentType)
	switch {
	case strings.HasSuffix(name, ".xlsx") ||
		strings.Contains(ct, "spreadsheetml") ||
		strings.Contains(ct, "excel"):
		return ParseXLSX(data)
	default:
		return ParseCSV(data)
	}
}

// ParseCSV reads UTF-8 CSV (optional BOM) into headers + data rows.
func ParseCSV(data []byte) (*ParsedSheet, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	r := csv.NewReader(bytes.NewReader(data))
	r.TrimLeadingSpace = true
	r.FieldsPerRecord = -1

	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("csv parse: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("csv is empty")
	}
	headers := trimRow(records[0])
	rows := make([][]string, 0, len(records)-1)
	for _, rec := range records[1:] {
		row := trimRow(rec)
		if rowEmpty(row) {
			continue
		}
		rows = append(rows, padRow(row, len(headers)))
	}
	return &ParsedSheet{Headers: headers, Rows: rows}, nil
}

// ParseXLSX reads the first sheet of an Excel workbook.
func ParseXLSX(data []byte) (*ParsedSheet, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("xlsx open: %w", err)
	}
	defer func() { _ = f.Close() }()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("xlsx has no sheets")
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, fmt.Errorf("xlsx rows: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("xlsx sheet is empty")
	}
	headers := trimRow(rows[0])
	out := make([][]string, 0, len(rows)-1)
	for _, rec := range rows[1:] {
		row := trimRow(rec)
		if rowEmpty(row) {
			continue
		}
		out = append(out, padRow(row, len(headers)))
	}
	return &ParsedSheet{Headers: headers, Rows: out}, nil
}

// PreviewRows returns up to PreviewLimit data rows.
func PreviewRows(rows [][]string) [][]string {
	if len(rows) <= PreviewLimit {
		return rows
	}
	return rows[:PreviewLimit]
}

// BuildCSV writes rows as UTF-8 CSV with BOM (AR-safe Excel).
func BuildCSV(headers []string, rows [][]string) ([]byte, error) {
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(&buf)
	if err := w.Write(headers); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if err := w.Write(padRow(row, len(headers))); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// BuildCSVFromMaps builds CSV from ExportRow maps using header order.
func BuildCSVFromMaps(headers []string, rows []ExportRow) ([]byte, error) {
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		line := make([]string, len(headers))
		for i, h := range headers {
			line[i] = r[h]
		}
		out = append(out, line)
	}
	return BuildCSV(headers, out)
}

// ErrorsCSV builds a row-error report download.
func ErrorsCSV(errs []RowError) ([]byte, error) {
	headers := []string{"row_number", "field", "message"}
	rows := make([][]string, 0, len(errs))
	for _, e := range errs {
		rows = append(rows, []string{
			fmt.Sprintf("%d", e.RowNumber),
			e.Field,
			e.Message,
		})
	}
	return BuildCSV(headers, rows)
}

func trimRow(row []string) []string {
	out := make([]string, len(row))
	for i, c := range row {
		out[i] = strings.TrimSpace(c)
	}
	return out
}

func rowEmpty(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

func padRow(row []string, n int) []string {
	if len(row) >= n {
		return row[:n]
	}
	out := make([]string, n)
	copy(out, row)
	return out
}
