package spreadsheet

// FromRowsForTest exposes fromRows to other packages' tests, so UI tests can
// build a Sheet without writing a real workbook to disk.
func FromRowsForTest(rows [][]string) (*Sheet, error) { return fromRows(rows) }
