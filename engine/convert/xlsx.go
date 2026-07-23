// engine/convert/xlsx.go — .xlsx → per-sheet Grid or JSON (one-way).
//
// Uses excelize v2.9.x (pinned, CLAUDE.md §7). Each sheet becomes one
// result, routed by the nb1 smart-routing rules (CLAUDE.md §5):
//
//	FLAT    — no merged cells, contiguous data bounds (no fully-empty row
//	          or column inside the used range) and a header-like first row
//	          (every cell non-empty and non-numeric)
//	          → Grid model: identical downstream behaviour to a CSV import,
//	            including CSV round-trip export.
//	COMPLEX — anything else
//	          → structured JSON (cell address → value) rendered by the
//	            caller in a fenced code block and anonymised as text.
//
// Trailing empty rows/columns are trimmed via data-bounds detection before
// routing, so a stray formatted-but-empty region does not force a sheet
// into the COMPLEX route.
package convert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Sheet is the conversion result for one worksheet.
type Sheet struct {
	// Name is the worksheet name (the caller composes the final Document
	// name as "<workbook>.xlsx#<sheet>").
	Name string
	// Flat reports which route the sheet took: true → Grid is set,
	// false → JSON is set.
	Flat bool
	// Grid is the rectangular cell model for FLAT sheets (nil otherwise).
	Grid [][]string
	// JSON is the pretty-printed {"A1": "value", …} object for COMPLEX
	// sheets ("" otherwise).
	JSON string
	// Warnings holds per-sheet routing notes for the import UI.
	Warnings []string
}

// Xlsx converts raw .xlsx bytes into one Sheet per non-empty worksheet.
// Workbook-level warnings (e.g. skipped empty sheets) are returned
// separately from the per-sheet ones.
func Xlsx(raw []byte) (sheets []Sheet, warnings []string, err error) {
	f, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, fmt.Errorf(
			"the file is not a valid .xlsx (%v) — if it is an old binary .xls file, open it in Excel and save it as .xlsx first", err)
	}
	defer f.Close()

	for _, name := range f.GetSheetList() {
		rows, err := f.GetRows(name)
		if err != nil {
			return nil, nil, fmt.Errorf("could not read sheet %q: %w — the workbook may be corrupted; try re-saving it in Excel", name, err)
		}

		grid := trimDataBounds(rows)
		if len(grid) == 0 {
			warnings = append(warnings, fmt.Sprintf("sheet %q is empty — skipped", name))
			continue
		}

		merged, err := f.GetMergeCells(name)
		if err != nil {
			return nil, nil, fmt.Errorf("could not inspect merged cells of sheet %q: %w", name, err)
		}

		if reason := complexReason(grid, len(merged)); reason == "" {
			sheets = append(sheets, Sheet{Name: name, Flat: true, Grid: grid})
		} else {
			js, err := gridToCellJSON(grid)
			if err != nil {
				return nil, nil, fmt.Errorf("could not serialise sheet %q to JSON: %w", name, err)
			}
			sheets = append(sheets, Sheet{
				Name: name,
				Flat: false,
				JSON: js,
				Warnings: []string{fmt.Sprintf(
					"sheet %q has a complex layout (%s) — it was converted to structured JSON and will be anonymised as text; it cannot round-trip to CSV", name, reason)},
			})
		}
	}
	if len(sheets) == 0 {
		return nil, nil, fmt.Errorf(
			"the workbook contains no data (all sheets are empty) — nothing to anonymise")
	}
	return sheets, warnings, nil
}

// trimDataBounds cuts trailing empty rows and columns and pads every row
// to the same width, returning a rectangular grid of the used range.
// excelize's GetRows already stops at the last non-empty row, but rows can
// still be ragged and may carry trailing empty cells from formatting.
func trimDataBounds(rows [][]string) [][]string {
	// Last row containing any value.
	lastRow := -1
	for i, row := range rows {
		for _, c := range row {
			if strings.TrimSpace(c) != "" {
				lastRow = i
				break
			}
		}
	}
	if lastRow < 0 {
		return nil
	}
	rows = rows[:lastRow+1]

	// Last column containing any value, across the kept rows.
	lastCol := -1
	for _, row := range rows {
		for j, c := range row {
			if strings.TrimSpace(c) != "" && j > lastCol {
				lastCol = j
			}
		}
	}

	// Cut and pad to a rectangle of width lastCol+1.
	out := make([][]string, len(rows))
	for i, row := range rows {
		r := make([]string, lastCol+1)
		copy(r, row) // shorter rows leave "" padding in place
		out[i] = r
	}
	return out
}

// complexReason returns "" when the sheet qualifies for the FLAT route, or
// a short human-readable reason for the COMPLEX route (used in the warning
// so the user understands the decision).
func complexReason(grid [][]string, mergedCount int) string {
	if mergedCount > 0 {
		return fmt.Sprintf("%d merged cell range(s)", mergedCount)
	}
	// Contiguity: a fully-empty row or column INSIDE the trimmed bounds
	// means the sheet holds several disjoint blocks, not one table.
	for i, row := range grid {
		empty := true
		for _, c := range row {
			if strings.TrimSpace(c) != "" {
				empty = false
				break
			}
		}
		if empty {
			return fmt.Sprintf("empty row %d inside the data range", i+1)
		}
	}
	for j := 0; j < len(grid[0]); j++ {
		empty := true
		for _, row := range grid {
			if strings.TrimSpace(row[j]) != "" {
				empty = false
				break
			}
		}
		if empty {
			return fmt.Sprintf("empty column %d inside the data range", j+1)
		}
	}
	// Header heuristic: the first row must look like column titles —
	// every cell non-empty and none of them numeric.
	for _, c := range grid[0] {
		if strings.TrimSpace(c) == "" {
			return "first row has empty cells (no header row)"
		}
		if _, err := strconv.ParseFloat(strings.TrimSpace(c), 64); err == nil {
			return "first row contains numbers (no header row)"
		}
	}
	return ""
}

// gridToCellJSON renders a grid as a pretty-printed JSON object keyed by
// cell address ("A1", "B2", …), skipping empty cells. json.Marshal sorts
// map keys, so the output is deterministic (stable golden tests).
func gridToCellJSON(grid [][]string) (string, error) {
	cells := map[string]string{}
	for i, row := range grid {
		for j, val := range row {
			if strings.TrimSpace(val) == "" {
				continue
			}
			addr, err := excelize.CoordinatesToCellName(j+1, i+1)
			if err != nil {
				return "", err
			}
			cells[addr] = val
		}
	}
	out, err := json.MarshalIndent(cells, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}
