package output

import (
	"bytes"
	"encoding/json"
	"io"
	"time"
)

// Statuses of a check in JSON output.
const (
	Succeeded   = "succeeded"
	Failed      = "failed"
	Unavailable = "unavailable"
)

// Result is a check's outcome in JSON output: its status, with Columns and Rows when it
// succeeded or Error when it did not.
type Result struct {
	Check   string   `json:"check"`
	Status  string   `json:"status,omitempty"`
	Columns []string `json:"columns,omitzero"`
	Rows    []Row    `json:"rows,omitzero"`
	Error   string   `json:"error,omitempty"`
}

// CheckDocument is the JSON output of one run of a check.
type CheckDocument struct {
	CapturedAt time.Time `json:"captured_at"`
	Result
}

// InvestigationDocument is the JSON output of one run of every step of an investigation.
// Error is set instead of Results when no step could run, such as when the connection failed.
type InvestigationDocument struct {
	CapturedAt    time.Time `json:"captured_at"`
	Investigation string    `json:"investigation"`
	Results       []Result  `json:"results,omitzero"`
	Error         string    `json:"error,omitempty"`
}

// ReadResult collects a check's rows for JSON output.
func ReadResult(check string, columns []string, rows Rows) (Result, error) {
	result := Result{Check: check, Columns: columns, Rows: []Row{}}
	err := rows(func(values []any) error {
		result.Rows = append(result.Rows, Row{columns: columns, values: values})
		return nil
	})
	return result, err
}

// WriteJSON writes a document as one line of compact JSON.
func WriteJSON(w io.Writer, document any) error {
	encoded, err := json.Marshal(document)
	if err != nil {
		return err
	}
	_, err = w.Write(append(encoded, '\n'))
	return err
}

// Row preserves PostgreSQL's column order. encoding/json sorts map keys, so
// representing a row as a map would lose the order of the result set.
type Row struct {
	columns []string
	values  []any
}

// MarshalJSON encodes the row as an object with keys in column order.
func (r Row) MarshalJSON() ([]byte, error) {
	var object bytes.Buffer
	object.WriteByte('{')
	for i, column := range r.columns {
		if i > 0 {
			object.WriteByte(',')
		}
		key, _ := json.Marshal(column) // A string can always be encoded as JSON.
		object.Write(key)
		object.WriteByte(':')
		encoded, err := json.Marshal(r.values[i])
		if err != nil {
			return nil, err
		}
		object.Write(encoded)
	}
	object.WriteByte('}')
	return object.Bytes(), nil
}
