package cli

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTypoSuggestions(t *testing.T) {
	app, _, _ := testApp(t, "")
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"hlep"}, `did you mean "help"`},
		{[]string{"run", "storage.table_szie"}, `did you mean "storage.table_size"`},
		{[]string{"describe", "storage.table_szie"}, `did you mean "storage.table_size"`},
		{[]string{"sql", "storage.table_szie"}, `did you mean "storage.table_size"`},
		{[]string{"run", "storage.table_size", "--schmea", "value"}, `did you mean "--schema"`},
	} {
		require.ErrorContains(t, app.Run(
			context.Background(),
			test.args,
		), test.want)
	}
}

func TestUnknownValueError(t *testing.T) {
	for _, test := range []struct {
		name, value    string
		candidates     []string
		fallback, want string
	}{
		{"suggestion takes precedence", "hlep", []string{"help"}, "use the list", `unknown command "hlep"; did you mean "help"?`},
		{"fallback without a close match", "unrelated", []string{"help"}, "use the list", `unknown command "unrelated"; use the list`},
		{"no candidates or fallback", "unknown", nil, "", `unknown command "unknown"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.EqualError(t, unknownValueError("command", test.value, test.candidates, test.fallback), test.want)
		})
	}
}

func TestClosestMatch(t *testing.T) {
	for _, test := range []struct {
		name, value string
		candidates  []string
		want        string
	}{
		{"case insensitive", "HLEP", []string{"Help", "doctor"}, "Help"},
		{"nearest candidate", "lis", []string{"last", "list", "lost"}, "list"},
		{"alphabetical tie", "cat", []string{"cut", "bat"}, "bat"},
		{"short input allows two edits", "abcde", []string{"abXYe"}, "abXYe"},
		{"short input rejects three edits", "abcdefg", []string{"abcXYZg"}, ""},
		{"long input allows three edits", "abcdefgh", []string{"abcXYZgh"}, "abcXYZgh"},
		{"long input rejects four edits", "abcdefgh", []string{"abcdWXYZ"}, ""},
		{"Unicode matching", "héłp", []string{"help"}, "help"},
		{"threshold counts runes", "éééé", []string{"éxyz"}, ""},
		{"no candidates", "help", nil, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := slices.Clone(test.candidates)
			require.Equal(t, test.want, closestMatch(test.value, test.candidates))
			require.Equal(t, original, test.candidates, "suggestions must not reorder caller-owned candidates")
		})
	}
}
