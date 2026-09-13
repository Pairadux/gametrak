// Package output renders aligned plain-text tables for terminal display.
package output

import (
	"io"
	"strings"
	"unicode/utf8"
)

// Align controls how a column's cells are padded to the column width.
type Align int

const (
	Left Align = iota
	Right
)

// Table accumulates rows of cells and renders them as aligned columns.
// Columns whose cells are all empty are dropped entirely, so callers can
// emit optional cells (such as an hours column) without special-casing
// the situation where no row uses them.
type Table struct {
	aligns []Align
	indent string
	gap    string
	gaps   []string
	rows   [][]string
}

// NewTable creates a table with the given per-column alignments. Columns
// beyond the provided alignments default to Left.
func NewTable(aligns ...Align) *Table {
	return &Table{aligns: aligns, indent: "  ", gap: "  "}
}

// Indent sets the prefix written before every row.
func (t *Table) Indent(s string) *Table {
	t.indent = s
	return t
}

// Gap sets the default separator written between columns.
func (t *Table) Gap(s string) *Table {
	t.gap = s
	return t
}

// Gaps overrides individual separators, where gaps[i] precedes column i+1.
// Unspecified separators fall back to the default gap. This lets related cells
// such as a count and its unit sit closer together than unrelated columns.
func (t *Table) Gaps(gaps ...string) *Table {
	t.gaps = gaps
	return t
}

// Row appends a row of cells.
func (t *Table) Row(cells ...string) {
	t.rows = append(t.rows, cells)
}

// Empty reports whether any rows have been added.
func (t *Table) Empty() bool {
	return len(t.rows) == 0
}

// String renders the table, one row per line, each line newline-terminated.
func (t *Table) String() string {
	widths := t.columnWidths()

	var b strings.Builder
	for _, row := range t.rows {
		var line strings.Builder
		line.WriteString(t.indent)
		first := true
		for col, width := range widths {
			if width == 0 {
				continue // column is empty across every row
			}
			if !first {
				line.WriteString(t.gapBefore(col))
			}
			first = false
			line.WriteString(pad(cell(row, col), width, t.align(col)))
		}
		b.WriteString(strings.TrimRight(line.String(), " "))
		b.WriteByte('\n')
	}
	return b.String()
}

// Print writes the rendered table to w.
func (t *Table) Print(w io.Writer) {
	io.WriteString(w, t.String())
}

func (t *Table) columnWidths() []int {
	var widths []int
	for _, row := range t.rows {
		for col, c := range row {
			for len(widths) <= col {
				widths = append(widths, 0)
			}
			if w := utf8.RuneCountInString(c); w > widths[col] {
				widths[col] = w
			}
		}
	}
	return widths
}

// gapBefore returns the separator preceding a column. Dropped columns do not
// shift it: the separator is always the one declared immediately before the
// column actually being written.
func (t *Table) gapBefore(col int) string {
	if col-1 < len(t.gaps) && col > 0 {
		return t.gaps[col-1]
	}
	return t.gap
}

func (t *Table) align(col int) Align {
	if col < len(t.aligns) {
		return t.aligns[col]
	}
	return Left
}

func cell(row []string, col int) string {
	if col < len(row) {
		return row[col]
	}
	return ""
}

func pad(s string, width int, align Align) string {
	padding := width - utf8.RuneCountInString(s)
	if padding <= 0 {
		return s
	}
	if align == Right {
		return strings.Repeat(" ", padding) + s
	}
	return s + strings.Repeat(" ", padding)
}

// Bar renders a proportional bar of the given character width, using solid
// blocks for the filled portion and light blocks for the remainder.
func Bar(value, max float64, width int) string {
	if width <= 0 {
		return ""
	}
	filled := 0
	if max > 0 && value > 0 {
		filled = int(value/max*float64(width) + 0.5)
		if filled < 1 {
			filled = 1 // never render a nonzero value as empty
		}
		if filled > width {
			filled = width
		}
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}
