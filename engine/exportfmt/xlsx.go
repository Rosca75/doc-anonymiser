// engine/exportfmt/xlsx.go — same-format export for Excel workbooks
// (BUILD-02 Phase 11e), via the already-pinned excelize: open the
// workbook from the import-time bytes, apply the pipeline mapping to
// STRING cells only, and save to new bytes. Shared strings, styles,
// merged cells and column widths survive because excelize preserves
// them; formulas and numeric cells are never touched.
package exportfmt

import (
	"bytes"
	"fmt"

	"github.com/xuri/excelize/v2"
)

// ExportXlsx rewrites an Excel workbook held in raw (the import-time
// bytes) and returns the new file plus the total replacement count.
func ExportXlsx(raw []byte, cfg Config) ([]byte, int, error) {
	f, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, fmt.Errorf("the workbook could not be opened (%v); re-import the original file and try again", err)
	}
	defer f.Close()

	total := 0
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		if err != nil {
			return nil, 0, fmt.Errorf("sheet %q could not be read (%v)", sheet, err)
		}
		for r, row := range rows {
			for c := range row {
				axis, err := excelize.CoordinatesToCellName(c+1, r+1)
				if err != nil {
					continue
				}
				// Value cells only: skip formulas outright.
				if formula, _ := f.GetCellFormula(sheet, axis); formula != "" {
					continue
				}
				// Only string cells are rewritten; numeric, boolean and
				// date cells must never be stringified.
				cellType, _ := f.GetCellType(sheet, axis)
				value, _ := f.GetCellValue(sheet, axis)
				if value == "" {
					continue
				}
				isString := cellType == excelize.CellTypeSharedString || cellType == excelize.CellTypeInlineString
				if !isString {
					continue
				}
				rewritten, n := cfg.AnonymiseText(value)
				if n == 0 {
					continue
				}
				total += n
				if err := f.SetCellStr(sheet, axis, rewritten); err != nil {
					return nil, 0, fmt.Errorf("cell %s on sheet %q could not be rewritten: %w", axis, sheet, err)
				}
			}
		}
	}

	var out bytes.Buffer
	if err := f.Write(&out); err != nil {
		return nil, 0, fmt.Errorf("the anonymised workbook could not be serialised: %w", err)
	}
	return out.Bytes(), total, nil
}
