package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bolajiwahab/pgcue/internal/testutil"
)

const invalidDSN = "postgres://localhost:invalid/pgcue"

func testApp(t *testing.T, input string) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	t.Setenv(timeoutEnv, "")

	out, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	return &App{
		In:  io.NopCloser(strings.NewReader(input)),
		Out: out,
		Err: stderr,
	}, out, stderr
}

func TestInvalidCommands(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"unknown command", []string{"unknown"}, "unknown command"},
		{"unexpected help argument", []string{"help", "extra"}, "help accepts no arguments"},
		{"missing description ID", []string{"describe"}, "usage: pgcue describe"},
		{"missing search terms", []string{"find"}, "usage: pgcue find"},
		{"extra topic argument", []string{"topic", "one", "two"}, "usage: pgcue topic [topic]"},
		{"unexpected check argument", []string{"run", "storage.table_size", "value"}, "unexpected argument"},
		{"unsupported option", []string{"doctor", outputFlag + "=json"}, "unknown option"},
		{"option for command without options", []string{"help", "--verbose"}, `help does not accept option "--verbose"`},
		{"similar option", []string{"doctor", "--dns", "x"}, `unknown option "--dns"; did you mean "--dsn"?`},
		{"argument after options", []string{"doctor", dsnFlag, "x", "extra"}, `unexpected argument "extra"`},
		{"investigation options without ID", []string{"investigate", dsnFlag, "x"}, "usage: pgcue investigate"},
		{"unknown investigation", []string{"investigate", "missing"}, `unknown investigation "missing"`},
		{"unknown investigation option", []string{"investigate", "slow-database", "--unknown"}, `unknown option "--unknown"`},
		{
			"missing parameter as flag",
			[]string{"run", "storage.table_size", dsnFlag, invalidDSN},
			"missing required parameter --schema",
		},
		{
			"invalid parameter value as flag",
			[]string{"run", "activity.connections", "--limit", "0", dsnFlag, invalidDSN},
			`invalid value "0" for --limit: must be at least 1`,
		},
		{"top-level option", []string{dsnFlag + "=x", "doctor"}, "options must follow their command"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			app, _, _ := testApp(t, "")
			err := app.Run(t.Context(), tt.args)

			var exitError *ExitError
			require.ErrorAs(t, err, &exitError)
			require.Equal(t, ExitInvalidInput, exitError.Code)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestInformationalCommandsDoNotLoadCatalogue(t *testing.T) {
	for _, args := range [][]string{
		{"help"},
		{"version"},
		{"completion", "bash"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			app, out, _ := testApp(t, "")

			require.NoError(t, app.Run(t.Context(), args))
			require.NotEmpty(t, out.String())
			require.Nil(t, app.catalogue)
		})
	}
}

func TestCatalogueCommands(t *testing.T) {
	for _, args := range [][]string{
		{"list"},
		{"describe", "storage.table_size"},
		{"sql", "storage.table_size"},
		{"topic"},
		{"find", "connections"},
		{"investigate"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			app, out, _ := testApp(t, "")
			t.Setenv("PGPORT", "invalid")

			require.NoError(t, app.Run(t.Context(), args))
			require.NotEmpty(t, out.String())
			require.NotNil(t, app.catalogue)
		})
	}
}

func TestHelp(t *testing.T) {
	app, out, _ := testApp(t, "")

	require.NoError(t, app.Run(t.Context(), []string{"help"}))
	help := out.String()

	for _, want := range []string{
		"Usage:",
		"Start the interactive browser.",
		"pgcue completion",
		"pgcue describe",
		"pgcue investigate",
		"pgcue list",
		"pgcue run",
		"pgcue topic",
		"Options:",
		"Environment:",
	} {
		require.Contains(t, help, want)
	}

	app, out, _ = testApp(t, "")
	require.NoError(t, app.Run(t.Context(), []string{helpFlag}))
	require.Equal(t, help, out.String())
}

func TestInteractiveRoot(t *testing.T) {
	app, out, _ := testApp(t, "q\n")

	require.NoError(t, app.Run(t.Context(), nil))
	require.Contains(t, out.String(), "Select a topic")
}

func TestInvestigationPostgreSQL(t *testing.T) {
	app, out, stderr := testApp(t, "r\n\ns\ns\ns\n")
	args := []string{"investigate", "connection-exhaustion", dsnFlag, testutil.PostgresDSN(t)}
	require.Equal(t, ExitSuccess, Run(t.Context(), args, app.In, app.Out, app.Err), stderr.String())
	require.Empty(t, stderr.String())
	require.Contains(t, out.String(), "database_name")
	require.Contains(t, out.String(), "Investigation complete.")
	require.Contains(t, out.String(), "Succeeded: 1")
}

func TestInvalidOutputFormat(t *testing.T) {
	for _, args := range [][]string{
		{
			"run", "activity.connections",
			outputFlag + "=xml",
			dsnFlag + "=" + invalidDSN,
		},
		{
			"run", "slow-database",
			outputFlag + "=xml",
			dsnFlag + "=" + invalidDSN,
		},
	} {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			app, _, _ := testApp(t, "")
			err := app.Run(t.Context(), args)

			var exitError *ExitError
			require.ErrorAs(t, err, &exitError)
			require.Equal(t, ExitInvalidInput, exitError.Code)
			require.ErrorContains(t, err, `invalid output format "xml"`)
		})
	}
}

func TestRunExitCodes(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	for _, tt := range []struct {
		name     string
		ctx      context.Context
		args     []string
		writer   io.Writer
		wantCode int
		wantOut  string
		wantErr  string
	}{
		{
			name:     "version",
			args:     []string{versionFlag},
			wantCode: ExitSuccess,
			wantOut:  "pgcue " + version + ", Compiler: gc go",
		},
		{
			name:     "invalid command",
			args:     []string{"unknown"},
			wantCode: ExitInvalidInput,
			wantErr:  "unknown command",
		},
		{
			name:     "output failure",
			args:     []string{versionFlag},
			writer:   testutil.FailingWriter{},
			wantCode: ExitFailure,
			wantErr:  "write failed",
		},
		{
			name:     "interrupted",
			ctx:      canceled,
			args:     []string{"run", "storage.database_size"},
			wantCode: ExitInterrupted,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			ctx := tt.ctx
			if ctx == nil {
				ctx = t.Context()
			}

			writer := tt.writer
			if writer == nil {
				writer = &stdout
			}

			code := Run(
				ctx,
				tt.args,
				io.NopCloser(strings.NewReader("")),
				writer,
				&stderr,
			)

			require.Equal(t, tt.wantCode, code)

			if tt.wantOut != "" {
				require.Contains(t, stdout.String(), tt.wantOut)
			}
			if tt.wantErr != "" {
				require.Contains(t, stderr.String(), tt.wantErr)
			}
		})
	}
}

func TestOfflineWriteErrors(t *testing.T) {
	for _, args := range [][]string{
		{"list"},
		{"describe", "storage.table_size"},
		{"describe", "slow-database"},
		{"sql", "storage.table_size"},
		{"find", "dynamic"},
		{"investigate"},
		{"topic"},
		{"topic", "wal"},
		{"help"},
		{"version"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			app, _, _ := testApp(t, "")
			app.Out = testutil.FailingWriter{}
			require.EqualError(t, app.Run(t.Context(), args), "write failed")
		})
	}
}
