package output

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type rejectingWriter struct{ needle string }

func (w rejectingWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), w.needle) {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}

func stream(rows [][]any) Rows {
	return func(yield func([]any) error) error {
		for _, row := range rows {
			if err := yield(row); err != nil {
				return err
			}
		}
		return nil
	}
}

type fakeTerminalWriter struct {
	bytes.Buffer
}

func (*fakeTerminalWriter) Fd() uintptr {
	return 42
}

func TestAlignedTableWidths(t *testing.T) {
	for _, test := range []struct {
		name     string
		columns  []string
		rows     [][]string
		terminal int
		widths   []int
		fits     bool
	}{
		{
			"exact width",
			[]string{"a", "b"},
			[][]string{{"123", "4567"}},
			9,
			[]int{3, 4},
			true,
		},
		{
			"too wide",
			[]string{"a", "b"},
			[][]string{{"123", "4567"}},
			8,
			[]int{3, 4},
			false,
		},
		{
			"long column fits",
			[]string{"value"},
			[][]string{{strings.Repeat("x", 61)}},
			80,
			[]int{61},
			true,
		},
		{
			"long column exceeds width",
			[]string{"value"},
			[][]string{{strings.Repeat("x", 61)}},
			60,
			[]int{61},
			false,
		},
		{
			"wide Unicode",
			[]string{"value"},
			[][]string{{strings.Repeat("界", 30)}},
			60,
			[]int{60},
			true,
		},
		{
			"combining characters",
			[]string{"value"},
			[][]string{{strings.Repeat("e\u0301", 60)}},
			60,
			[]int{60},
			true,
		},
		{
			"multiline value",
			[]string{"value"},
			[][]string{{"a\nb"}},
			120, nil, false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			widths, fits := alignedTableWidths(test.columns, test.rows, test.terminal)
			require.Equal(t, test.widths, widths)
			require.Equal(t, test.fits, fits)
		})
	}
}

func TestTableRenderer(t *testing.T) {
	for _, test := range []struct {
		name    string
		columns []string
		rows    [][]any
		want    string
	}{
		{
			name:    "aligned",
			columns: []string{"name", "count"},
			rows:    [][]any{{"Ada!", "three"}},
			want: "name  count\n" +
				"────  ─────\n" +
				"Ada!  three\n",
		},
		{
			name:    "empty",
			columns: []string{"name"},
			want:    "No rows returned.\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer

			err := (TableRenderer{}).Render(&out, test.columns, stream(test.rows))
			require.NoError(t, err)
			require.Equal(t, test.want, out.String())
		})
	}
}

func TestTableRendererExpandedRecords(t *testing.T) {
	var out bytes.Buffer
	rows := [][]any{
		{12345, 2500, "SELECT id, email, created_at\nFROM users\nWHERE created_at > NOW() - INTERVAL '7 days'"},
		{67890, 1200, "SELECT * FROM orders"},
	}

	err := (TableRenderer{}).Render(
		&out,
		[]string{"query_id", "calls", "query"},
		stream(rows),
	)
	require.NoError(t, err)

	rule := strings.Repeat("-", 107)
	require.Equal(t,
		"-[ RECORD 1 ]"+rule+"\n"+
			"query_id | 12345\n"+
			"calls    | 2500\n"+
			"query    | SELECT id, email, created_at\n"+
			"         | FROM users\n"+
			"         | WHERE created_at > NOW() - INTERVAL '7 days'\n"+
			"\n-[ RECORD 2 ]"+rule+"\n"+
			"query_id | 67890\n"+
			"calls    | 1200\n"+
			"query    | SELECT * FROM orders\n",
		out.String(),
	)
}

func TestTableRendererExpandedValues(t *testing.T) {
	for _, test := range []struct {
		name    string
		columns []string
		values  []any
		want    string
	}{
		{
			"Unicode and NULL",
			[]string{"界", "id"},
			[]any{"first\nsecond", nil},
			"界 | first\n   | second\nid | NULL\n",
		},
		{
			"blank lines",
			[]string{"query"},
			[]any{"\n\ntail\n"},
			"query | \n      | \n      | tail\n      | \n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			err := (TableRenderer{}).Render(
				&out, test.columns, stream([][]any{test.values}),
			)
			require.NoError(t, err)

			_, fields, ok := strings.Cut(out.String(), "\n")
			require.True(t, ok)
			require.Equal(t, test.want, fields)
		})
	}
}

func TestTableRendererUsesFallbackWidth(t *testing.T) {
	var out bytes.Buffer
	first := strings.Repeat("a", 60)
	second := strings.Repeat("b", 60)

	// The combined table width is 122, exceeding the fallback of 120.
	err := (TableRenderer{}).Render(
		&out,
		[]string{"first", "second"},
		stream([][]any{{first, second}}),
	)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(out.String(), "-[ RECORD 1 ]"))
	require.Contains(t, out.String(), "first  | "+first)
	require.Contains(t, out.String(), "second | "+second)
}

func TestTableTerminalWidth(t *testing.T) {
	for _, test := range []struct {
		name  string
		width int
		err   error
		want  int
	}{
		{"terminal", 80, nil, 80},
		{"invalid size", 0, nil, tableDefaultWidth},
		{"lookup failure", 80, errors.New("not a terminal"), tableDefaultWidth},
	} {
		t.Run(test.name, func(t *testing.T) {
			width := detectTableWidth(&fakeTerminalWriter{}, func(fd int) (int, int, error) {
				require.Equal(t, 42, fd)
				return test.width, 24, test.err
			})
			require.Equal(t, test.want, width)
		})
	}

	width := detectTableWidth(&bytes.Buffer{}, func(int) (int, int, error) {
		t.Fatal("nonterminal writer should not request terminal size")
		return 0, 0, nil
	})
	require.Equal(t, tableDefaultWidth, width)
}

func TestTableRendererColor(t *testing.T) {
	var out bytes.Buffer

	err := (TableRenderer{Style: Style{Enabled: true}}).Render(
		&out,
		[]string{"name"},
		stream([][]any{{"Alice"}}),
	)

	require.NoError(t, err)
	require.Contains(t, out.String(), (Style{Enabled: true}).Heading("name "))
}

func TestTableRendererWriteErrors(t *testing.T) {
	for _, test := range []struct {
		name    string
		columns []string
		rows    [][]any
		needle  string
	}{
		{"aligned header", []string{"name"}, [][]any{{"Alice"}}, "name"},
		{"aligned row", []string{"name"}, [][]any{{"Alice"}}, "Alice"},
		{"expanded heading", []string{"query"}, [][]any{{"first\nsecond"}}, "RECORD 1"},
		{"expanded value", []string{"query"}, [][]any{{"first\nsecond"}}, "second"},
		{"next record", []string{"query"}, [][]any{{"first\nsecond"}, {"another"}}, "RECORD 2"},
		{"empty result", []string{"name"}, nil, "No rows returned."},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := (TableRenderer{}).Render(
				rejectingWriter{needle: test.needle},
				test.columns,
				stream(test.rows),
			)
			require.ErrorIs(t, err, io.ErrClosedPipe)
		})
	}
}

func TestTableRendererStreamError(t *testing.T) {
	var out bytes.Buffer
	want := errors.New("stream failed")

	rows := Rows(func(yield func([]any) error) error {
		if err := yield([]any{"first row"}); err != nil {
			return err
		}
		return want
	})

	err := (TableRenderer{}).Render(&out, []string{"value"}, rows)
	require.ErrorIs(t, err, want)
	require.Empty(t, out.String())
}

// Database values such as query text are shown as text, so they cannot move the cursor or
// send escape sequences to the terminal.
func TestTerminalText(t *testing.T) {
	for _, test := range []struct{ name, value, want string }{
		{"plain text", "SELECT 1", "SELECT 1"},
		{"tab expands to the next stop", "a\tb", "a       b"},
		{"tab stop after a wide character", "界\tb", "界      b"},
		{"tab stop after a line break", "abc\n\tb", "abc\n        b"},
		{"CRLF line ending", "first\r\nsecond", "first\nsecond"},
		{"carriage return", "progress\rdone", `progress\rdone`},
		{"escape sequence", "\x1b[2Jcleared", `\x1B[2Jcleared`},
		{"invalid UTF-8", "bad\xffbyte", "bad\uFFFDbyte"},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, terminalText(test.value))
		})
	}
}
