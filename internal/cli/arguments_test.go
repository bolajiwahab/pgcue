package cli

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/manual"
)

// Check parameters become run options, so they must not shadow the CLI's own options.
func TestCheckParametersDoNotShadowOptions(t *testing.T) {
	catalog, err := catalogue.LoadCatalogue()
	require.NoError(t, err)
	reserved := slices.Concat(runFlags, connectionFlags,
		[]string{availableFlag, helpFlag, versionFlag})
	for _, check := range catalog.Checks() {
		for name := range check.Parameters {
			require.NotContains(t, reserved, "--"+manual.FlagName(name), "check %s parameter %s", check.ID, name)
		}
	}
}

func TestRunOptions(t *testing.T) {
	const exampleDSN = "postgres://host/db"

	app, _, _ := testApp(t, "")
	require.NoError(t, app.Run(t.Context(), []string{"sql", "storage.table_size"}))
	check, ok := app.catalogue.GetCheck("storage.table_size")
	require.True(t, ok)
	for _, test := range []struct {
		name               string
		args               []string
		dsn, schema, table string
	}{
		{
			name: "separate values",
			args: []string{
				"--schema", "public", "--table", "users",
				dsnFlag, exampleDSN,
				outputFlag, "json", watchFlag, "2", countFlag, "3", timeoutFlag, "5s",
			},
			dsn: exampleDSN, schema: "public", table: "users",
		},
		{
			name: "inline values",
			args: []string{
				"--schema=public", "--table=users", dsnFlag + "=" + exampleDSN,
				outputFlag + "=json", watchFlag + "=2s", countFlag + "=3", timeoutFlag + "=5s",
			},
			dsn: exampleDSN, schema: "public", table: "users",
		},
		{
			name: "empty values",
			args: []string{
				"--schema=", "--table", "", dsnFlag + "=",
				outputFlag + "=json", watchFlag + "=2s", countFlag + "=3", timeoutFlag + "=5s",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			options, values, err := parseRunOptions(check, test.args)
			require.NoError(t, err)
			require.Equal(
				t,
				runOptions{dsn: test.dsn, format: "json", watch: 2 * time.Second, count: 3, timeout: 5 * time.Second},
				options,
			)
			require.Equal(t, map[string]string{"schema": test.schema, "table": test.table}, values)
		})
	}
}

func TestRunOptionErrors(t *testing.T) {
	app, _, _ := testApp(t, "")
	require.NoError(t, app.Run(t.Context(), []string{"sql", "storage.table_size"}))
	check, ok := app.catalogue.GetCheck("storage.table_size")
	require.True(t, ok)
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"missing value", []string{dsnFlag}, dsnFlag + " requires a value"},
		{"option as value", []string{outputFlag, timeoutFlag, "1s"}, outputFlag + " requires a value"},
		{"unknown flag", []string{"--other=1"}, `unknown option "--other"`},
		{"duplicate parameter", []string{"--schema=one", "--schema=two"}, "option --schema specified more than once"},
		{
			"duplicate option",
			[]string{outputFlag + "=json", outputFlag + "=csv"},
			"option " + outputFlag + " specified more than once",
		},
		{"zero watch", []string{watchFlag + "=0s"}, watchFlag + " must be a positive duration"},
		{"unitless zero watch", []string{watchFlag + "=0"}, watchFlag + " must be a positive duration"},
		{"negative watch", []string{watchFlag + "=-1s"}, watchFlag + " must be a positive duration"},
		{"unitless negative watch", []string{watchFlag + "=-1"}, watchFlag + " must be a positive duration"},
		{"malformed watch", []string{watchFlag + "=invalid"}, watchFlag + " must be a positive duration"},
		{"calendar watch", []string{watchFlag + "=1 month"}, watchFlag + " must be a positive duration"},
		{"count without watch", []string{countFlag + "=3"}, countFlag + " requires " + watchFlag},
		{"zero count", []string{watchFlag + "=1s", countFlag + "=0"}, countFlag + " must be a positive whole number"},
		{"fractional count", []string{watchFlag + "=1s", countFlag + "=1.5"}, countFlag + " must be a positive whole number"},
		{"positional", []string{"value"}, `unexpected argument "value"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := parseRunOptions(check, test.args)
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestConnectionOptions(t *testing.T) {
	for _, test := range []struct {
		name, environment string
		args              []string
		want              time.Duration
		wantErr           string
	}{
		{"default", "", nil, 30 * time.Second, ""},
		{"unknown option", "", []string{"--unknown=x"}, 0, `unknown option "--unknown"`},
		{
			"duplicate option", "",
			[]string{dsnFlag + "=x", dsnFlag + "=y"},
			0,
			"option " + dsnFlag + " specified more than once",
		},
		{"missing value", "", []string{timeoutFlag}, 0, timeoutFlag + " requires a value"},
		{"environment", "2m", nil, 2 * time.Minute, ""},
		{"CLI override", "2m", []string{timeoutFlag, "3s"}, 3 * time.Second, ""},
		{"invalid environment", "invalid", nil, 0, timeoutEnv},
		{"CLI overrides invalid environment", "invalid", []string{timeoutFlag + "=0"}, 0, ""},
		{
			"explicit empty timeout", "2m",
			[]string{timeoutFlag + "="},
			0,
			timeoutFlag + " must be a positive duration or 0",
		},
		{
			"negative timeout", "",
			[]string{timeoutFlag + "=-1s"},
			0,
			timeoutFlag + " must be a positive duration or 0",
		},
		{
			"malformed timeout", "",
			[]string{timeoutFlag + "=invalid"},
			0,
			timeoutFlag + " must be a positive duration or 0",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(timeoutEnv, test.environment)
			_, timeout, err := parseConnectionOptions(test.args)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, timeout)
		})
	}
}

func TestParameterNameReversesFlagName(t *testing.T) {
	for _, name := range []string{"minimum_duration", "database"} {
		require.Equal(t, name, parameterName("--"+manual.FlagName(name)))
	}
}
