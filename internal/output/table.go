package output

import (
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

const (
	tableDefaultWidth = 120
	tableSpacing      = "  "
	tabWidth          = 8
)

// TableRenderer automatically chooses aligned or expanded table output.
type TableRenderer struct{ Style Style }

// Render inspects the complete result set before choosing a layout.
func (r TableRenderer) Render(w io.Writer, columns []string, rows Rows) error {
	var data [][]string
	if err := rows(func(row []any) error {
		values := displayRow(row)
		for i, value := range values {
			values[i] = terminalText(value)
		}
		data = append(data, values)
		return nil
	}); err != nil {
		return err
	}

	style := r.Style
	if len(data) == 0 {
		_, err := fmt.Fprintln(w, style.Yellow("No rows returned."))
		return err
	}
	availableWidth := detectTableWidth(w, term.GetSize)
	widths, fits := alignedTableWidths(columns, data, availableWidth)
	if fits {
		return writeAlignedTable(w, columns, data, widths, style)
	}
	return writeExpandedTable(w, columns, data, availableWidth, style)
}

// terminalText expands tabs, keeps line breaks, escapes other control characters, and
// replaces invalid UTF-8, so database values such as query text cannot move the cursor or
// send terminal escape sequences.
func terminalText(value string) string {
	if utf8.ValidString(value) &&
		!strings.ContainsFunc(value, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) {
		return value
	}
	var text strings.Builder
	column := 0
	for _, r := range strings.ReplaceAll(value, "\r\n", "\n") {
		var escaped string
		switch {
		case r == '\n':
			text.WriteRune(r)
			column = 0
			continue
		case r == '\t':
			escaped = strings.Repeat(" ", tabWidth-column%tabWidth)
		case r == '\r':
			escaped = `\r`
		case unicode.IsControl(r):
			escaped = fmt.Sprintf(`\x%02X`, r)
		default:
			text.WriteRune(r)
			column += runewidth.RuneWidth(r)
			continue
		}
		text.WriteString(escaped)
		column += len(escaped)
	}
	return text.String()
}

func alignedTableWidths(columns []string, data [][]string, availableWidth int) ([]int, bool) {
	widths := make([]int, len(columns))

	for i, column := range columns {
		widths[i] = runewidth.StringWidth(column)
	}

	for _, row := range data {
		for i, value := range row {
			if strings.ContainsRune(value, '\n') {
				return nil, false
			}
			widths[i] = max(widths[i], runewidth.StringWidth(value))
		}
	}

	total := max(0, len(columns)-1) * len(tableSpacing)
	for _, width := range widths {
		total += width
	}

	return widths, total <= availableWidth
}

func detectTableWidth(w io.Writer, getSize func(int) (int, int, error)) int {
	type fileDescriptor interface{ Fd() uintptr }
	if output, ok := w.(fileDescriptor); ok {
		if width, _, err := getSize(int(output.Fd())); err == nil && width > 0 {
			return width
		}
	}
	return tableDefaultWidth
}

func writeAlignedTable(w io.Writer, columns []string, data [][]string, widths []int, style Style) error {
	if err := writeTableRow(w, columns, widths, style.Heading); err != nil {
		return err
	}
	rules := make([]string, len(widths))
	for i, width := range widths {
		rules[i] = strings.Repeat("─", width)
	}
	if err := writeTableRow(w, rules, widths, style.Dim); err != nil {
		return err
	}
	for _, row := range data {
		if err := writeTableRow(w, row, widths, nil); err != nil {
			return err
		}
	}
	return nil
}

func writeExpandedTable(w io.Writer, columns []string, data [][]string, availableWidth int, style Style) error {
	labelWidth := 0
	for _, column := range columns {
		labelWidth = max(labelWidth, runewidth.StringWidth(column))
	}
	for record, row := range data {
		if err := writeExpandedHeading(w, record+1, availableWidth, style); err != nil {
			return err
		}
		for i, column := range columns {
			if err := writeExpandedField(w, column, row[i], labelWidth, style); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeExpandedHeading(w io.Writer, recordNumber, availableWidth int, style Style) error {
	heading := fmt.Sprintf("-[ RECORD %d ]", recordNumber)
	rule := strings.Repeat("-", max(0, availableWidth-runewidth.StringWidth(heading)))
	header := style.Heading(heading) + style.Dim(rule)
	if recordNumber > 1 {
		header = "\n" + header
	}
	_, err := fmt.Fprintln(w, header)
	return err
}

// writeExpandedField writes each line of a value, labelling the first.
func writeExpandedField(w io.Writer, label, value string, labelWidth int, style Style) error {
	separator := style.Dim(" | ")
	label = style.Heading(label) + strings.Repeat(" ", labelWidth-runewidth.StringWidth(label))
	for line := range strings.SplitSeq(value, "\n") {
		if _, err := fmt.Fprintln(w, label+separator+line); err != nil {
			return err
		}
		label = strings.Repeat(" ", labelWidth)
	}
	return nil
}

func writeTableRow(w io.Writer, values []string, widths []int, decorate func(string) string) error {
	for i, value := range values {
		if i > 0 {
			if _, err := io.WriteString(w, tableSpacing); err != nil {
				return err
			}
		}
		cell := value + strings.Repeat(" ", widths[i]-runewidth.StringWidth(value))
		if decorate != nil {
			cell = decorate(cell)
		}
		if _, err := io.WriteString(w, cell); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "\n")
	return err
}
