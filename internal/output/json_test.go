package output

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestJSONDocument(t *testing.T) {
	capturedAt := time.Date(2026, 11, 19, 15, 20, 0, 0, time.UTC)
	for _, test := range []struct {
		name    string
		columns []string
		data    [][]any
		want    string
	}{
		{
			name:    "empty results",
			columns: []string{"name", "active"},
			want: `{"captured_at":"2026-11-19T15:20:00Z","check":"demo.example",` +
				`"columns":["name","active"],"rows":[]}` + "\n",
		},
		{
			name:    "column order and nulls",
			columns: []string{"name", "active"},
			data:    [][]any{{"Alice", true}, {"Bob", nil}},
			want: `{"captured_at":"2026-11-19T15:20:00Z","check":"demo.example","columns":["name","active"],` +
				`"rows":[{"name":"Alice","active":true},{"name":"Bob","active":null}]}` + "\n",
		},
		{
			name:    "escaping",
			columns: []string{"message"},
			data:    [][]any{{"Hello \"world\"\nNext line"}},
			want: `{"captured_at":"2026-11-19T15:20:00Z","check":"demo.example","columns":["message"],` +
				`"rows":[{"message":"Hello \"world\"\nNext line"}]}` + "\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := ReadResult("demo.example", test.columns, stream(test.data))
			require.NoError(t, err)
			var output bytes.Buffer
			require.NoError(t, WriteJSON(&output, CheckDocument{CapturedAt: capturedAt, Result: result}))
			require.Equal(t, test.want, output.String())
		})
	}
}

func TestJSONRowError(t *testing.T) {
	want := errors.New("row iteration failed")
	_, err := ReadResult("demo.example", []string{"name"}, func(func([]any) error) error { return want })
	require.ErrorIs(t, err, want)
}

func TestJSONWriterError(t *testing.T) {
	want := errors.New("write failed")
	require.ErrorIs(t, WriteJSON(failingWriter{err: want}, CheckDocument{}), want)
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }
