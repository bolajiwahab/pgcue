// Package output renders query results and terminal messages.
package output

import "fmt"

// Rows streams query rows.
type Rows func(func([]any) error) error

func display(value any) string {
	if value == nil {
		return "NULL"
	}
	return fmt.Sprint(value)
}

func displayRow(row []any) []string {
	values := make([]string, len(row))
	for i, value := range row {
		values[i] = display(value)
	}
	return values
}
