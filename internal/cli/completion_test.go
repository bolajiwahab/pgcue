package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bolajiwahab/pgcue/internal/testutil"
)

func TestCompletionScripts(t *testing.T) {
	for _, test := range []struct {
		shell, registration, arguments string
	}{
		{"bash", "complete -F _pgcue_complete pgcue", `"${COMP_WORDS[@]:1}"`},
		{"fish", "complete -c pgcue", `"$current"`},
		{"zsh", "compdef _pgcue pgcue", `"${words[@]:1}"`},
	} {
		t.Run(test.shell, func(t *testing.T) {
			app, out, _ := testApp(t, "")
			require.NoError(t, app.Run(t.Context(), []string{"completion", test.shell}))
			require.Contains(t, out.String(), "__complete")
			require.Contains(t, out.String(), test.registration)
			require.Contains(t, out.String(), test.arguments)
		})
	}
}

func TestCompletionCandidates(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{nil, strings.Join([]string{
			dsnFlag, helpFlag, timeoutFlag, versionFlag, shortHelpFlag,
			"completion", "describe", "doctor", "find", "help", "investigate", "list", "run", "sql", "topic", "version",
		}, "\n") + "\n"},
		{[]string{"r"}, "run\n"},
		{[]string{"run", "storage.table_s"}, "storage.table_size\n"},
		{[]string{"sql", "storage.table_s"}, "storage.table_size\n"},
		{[]string{"describe", "statements"}, "statements\nstatements.top\n"},
		{[]string{"topic", "va"}, "vacuum\n"},
		{[]string{"investigate", "slow"}, "slow-database\n"},
		{[]string{"completion", ""}, "bash\nfish\nzsh\n"},
		{[]string{"doctor", "--"}, dsnFlag + "\n" + timeoutFlag + "\n"},
		{[]string{"list", "--"}, availableFlag + "\n"},
		{[]string{"list", availableFlag, "--"}, dsnFlag + "\n" + timeoutFlag + "\n"},
		{[]string{"investigate", "slow-database", "--"}, dsnFlag + "\n" + timeoutFlag + "\n"},
		{[]string{dsnFlag, "local", "--t"}, timeoutFlag + "\n"},
		{
			[]string{"run", "storage.table_size", "--"},
			countFlag + "\n" + dsnFlag + "\n" + outputFlag + "\n--schema\n--table\n" + timeoutFlag + "\n" + watchFlag + "\n",
		},
		{[]string{"run", "activity.connections", outputFlag, "j"}, "json\n"},
		{[]string{"run", "slow-database", outputFlag + "=t"}, outputFlag + "=table\n"},
		{[]string{"run", "storage.table_size", "--schema=public", "--ta"}, "--table\n"},
		{[]string{"run", "storage.table_size", "--schema", "public", "--ta"}, "--table\n"},
		{[]string{"run", "storage.table_size", "--schema", "", "--ta"}, "--table\n"},
		{[]string{"doctor", dsnFlag, "local"}, ""},
		{[]string{"run", "storage.table_size", "--schema", "value"}, ""},
		{[]string{"topic", "vacuum", ""}, ""},
		{[]string{"run", "storage.table_size", "extra", ""}, ""},
		{[]string{"list", "extra", ""}, ""},
		{[]string{"unknown", ""}, ""},
		{[]string{outputFlag + "=j"}, ""},
		{[]string{"run", "missing.check", outputFlag + "=j"}, ""},
		{[]string{"investigate", "missing", outputFlag + "=j"}, ""},
		{[]string{"run", "storage.table_size", "--unknown=x", ""}, ""},
		{[]string{"run", "storage.table_size", "--schema", outputFlag, "j"}, ""},
		{[]string{"doctor", outputFlag + "="}, ""},
		{[]string{"investigate", "slow-database", watchFlag}, ""},
	} {
		t.Run(strings.Join(append([]string{"pgcue"}, test.args...), " "), func(t *testing.T) {
			app, out, _ := testApp(t, "")
			require.NoError(t, app.Run(t.Context(), append([]string{"__complete"}, test.args...)))
			require.Equal(t, test.want, out.String())
		})
	}
}

func TestCompletionErrors(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"completion"}, "usage: pgcue completion <bash|zsh|fish>"},
		{[]string{"completion", "powershell"}, `unsupported shell "powershell"; expected bash, zsh, or fish`},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			app, _, _ := testApp(t, "")
			err := app.Run(t.Context(), test.args)
			require.EqualError(t, err, test.want)
			var exitError *ExitError
			require.ErrorAs(t, err, &exitError)
			require.Equal(t, ExitInvalidInput, exitError.Code)
		})
	}
}

func TestCompletionWriteErrors(t *testing.T) {
	for _, args := range [][]string{{"completion", "bash"}, {"__complete", "r"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			app, _, _ := testApp(t, "")
			app.Out = testutil.FailingWriter{}
			require.EqualError(t, app.Run(t.Context(), args), "write failed")
		})
	}
}
