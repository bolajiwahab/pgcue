package output

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
)

func TestStyle(t *testing.T) {
	for _, test := range []struct {
		name, want string
		format     func(Style, string) string
	}{
		{"heading", "\x1b[1;36mx\x1b[0m", Style.Heading},
		{"bold", "\x1b[1mx\x1b[0m", Style.Bold},
		{"dim", "\x1b[2mx\x1b[0m", Style.Dim},
		{"red", "\x1b[1;31mx\x1b[0m", Style.Red},
		{"green", "\x1b[32mx\x1b[0m", Style.Green},
		{"cyan", "\x1b[36mx\x1b[0m", Style.Cyan},
		{"yellow", "\x1b[33mx\x1b[0m", Style.Yellow},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, test.format(Style{Enabled: true}, "x"))
			require.Equal(t, "x", test.format(Style{}, "x"))
		})
	}
}

func TestAutoColor(t *testing.T) {
	master, terminal, err := pty.Open()
	if errors.Is(err, pty.ErrUnsupported) {
		t.Skip("pseudo-terminals are unsupported on this platform")
	}
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, master.Close())
		require.NoError(t, terminal.Close())
	})

	for _, test := range []struct {
		name    string
		term    string
		noColor bool
		value   string
		want    bool
	}{
		{"terminal", "xterm-256color", false, "", true},
		{"NO_COLOR", "xterm-256color", true, "1", false},
		{"empty NO_COLOR", "xterm-256color", true, "", false},
		{"dumb terminal", "dumb", false, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("TERM", test.term)
			t.Setenv("NO_COLOR", test.value)
			if !test.noColor {
				require.NoError(t, os.Unsetenv("NO_COLOR"))
			}

			require.Equal(t, test.want, Auto(terminal).Enabled)
		})
	}
}

func TestAutoColorNonterminalOutput(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	require.NoError(t, os.Unsetenv("NO_COLOR"))

	require.False(t, Auto(&bytes.Buffer{}).Enabled)

	file, err := os.Open(os.DevNull)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })

	require.False(t, Auto(file).Enabled)
}
