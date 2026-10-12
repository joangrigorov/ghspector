package ui

import (
	"strings"

	"ghspector/internal/layout"
)

// tableIndent is the gutter every list row starts with, so a selected row's
// marker sits clear of the terminal edge.
const tableIndent = "  "

// tableGap is the space between two columns.
const tableGap = 1

// tableColumn is one column of a list table: the label above it and the width
// budget it is allocated.
type tableColumn struct {
	Header string
	layout.Column
}

// fitTable allocates the columns of a table that fills a row of width cells.
func fitTable(cols []tableColumn, width int) []tableColumn {
	plain := make([]layout.Column, len(cols))
	for i, c := range cols {
		plain[i] = c.Column
	}
	fitted := layout.FitColumns(plain, width, layout.Width(tableIndent), tableGap)
	out := make([]tableColumn, len(cols))
	for i := range cols {
		out[i] = tableColumn{Header: cols[i].Header, Column: fitted[i]}
	}
	return out
}

// tableRow renders one row's cells against the allocated columns. Each cell is
// truncated and padded to its column, and a column that was dropped is skipped,
// so the header and every row stay aligned.
func tableRow(cols []tableColumn, cells []string) string {
	parts := make([]string, 0, len(cols))
	for i, c := range cols {
		if c.Width <= 0 || i >= len(cells) {
			continue
		}
		parts = append(parts, layout.PadRight(layout.Truncate(cells[i], c.Width), c.Width))
	}
	return tableIndent + strings.Join(parts, strings.Repeat(" ", tableGap))
}

// tableHeaderRow renders the column labels above the rows.
func tableHeaderRow(cols []tableColumn) string {
	labels := make([]string, len(cols))
	for i, c := range cols {
		labels[i] = c.Header
	}
	return tableRow(cols, labels)
}
