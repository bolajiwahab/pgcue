package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Connection provides the database operations needed for inspection and execution.
type Connection interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// Executor inspects a database and executes selected SQL.
type Executor struct{ Conn Connection }

// Result contains query columns and rows.
type Result struct {
	Columns []string
	// Rows streams normalized row values to yield, stopping at its first error.
	Rows  func(yield func([]any) error) error
	close func()
}

// Close releases the result rows.
func (r Result) Close() {
	if r.close != nil {
		r.close()
	}
}

// Query executes SQL selected by the application and streams normalized values.
func (e Executor) Query(ctx context.Context, id, statement string, arguments pgx.NamedArgs) (Result, error) {
	// Text results carry PostgreSQL's own rendering of each value.
	rows, err := e.Conn.Query(ctx, statement, pgx.QueryResultFormats{pgx.TextFormatCode}, arguments)
	if err != nil {
		return Result{}, fmt.Errorf("execute %s: %w", id, err)
	}
	fields := rows.FieldDescriptions()
	result := Result{
		Columns: make([]string, len(fields)),
		close:   rows.Close,
	}
	for i, field := range fields {
		result.Columns[i] = string(field.Name)
	}
	result.Rows = func(yield func([]any) error) error {
		defer rows.Close()
		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				return fmt.Errorf("read %s result: %w", id, err)
			}
			if err := yield(rowValues(rows.RawValues(), values)); err != nil {
				return err
			}
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("read %s result: %w", id, err)
		}
		return nil
	}
	return result, nil
}

// rowValues keeps values that JSON represents directly and uses PostgreSQL's own text for
// the rest, such as timestamps, intervals, and numerics. Arrays keep both forms. pgx returns
// one raw value per decoded value.
func rowValues(rawValues [][]byte, values []any) []any {
	for i, value := range values {
		switch value := value.(type) {
		case nil, bool, int16, int32, int64, uint32, float32, float64, string:
		case []any:
			values[i] = Array{text: string(rawValues[i]), elements: value}
		default:
			values[i] = string(rawValues[i])
		}
	}
	return values
}

// Array is an array column value: PostgreSQL's text form for tables, and its elements for JSON.
type Array struct {
	text     string
	elements []any
}

// String returns the array as PostgreSQL writes it, such as {101,NULL} or {"a b",c}.
func (a Array) String() string { return a.text }

// MarshalJSON encodes the array's elements as a JSON array.
func (a Array) MarshalJSON() ([]byte, error) { return json.Marshal(a.elements) }
