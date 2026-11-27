package cli

import (
	"net"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bolajiwahab/pgcue/internal/output"
	"github.com/bolajiwahab/pgcue/internal/pgcue"
	"github.com/bolajiwahab/pgcue/internal/postgres"
	"github.com/bolajiwahab/pgcue/internal/testutil"
)

func TestDoctorReadiness(t *testing.T) {
	app, out, _ := testApp(t, "")
	require.NoError(t, app.writeDoctor(pgcue.DoctorReport{
		Target: "localhost/app",
		Connection: postgres.ConnectionInfo{
			ServerMajorVersion: 14,
			ServerRole:         "primary",
			Database:           "app",
			User:               "reader",
			ReadOnly:           true,
			PGMonitor:          true,
		},
	}))
	require.Equal(t, `PostgreSQL readiness
  ✓ Connected to localhost/app
  ✓ Server role: primary
  ✓ PostgreSQL 14 is supported
  ✓ Connected as reader to database app
  ✓ Session is read-only
  ✓ Role can read PostgreSQL monitoring views

Optional extensions
`, out.String())
}

func TestDoctorExtensions(t *testing.T) {
	for _, test := range []struct {
		name, version, want string
	}{
		{"missing", "", "! pg_stat_statements is not installed; unavailable: statements.top"},
		{"minimum", "1.5", "✓ pg_stat_statements 1.5 is supported"},
		{"newer", "1.10", "✓ pg_stat_statements 1.10 is supported"},
		{"unsupported", "1.4", "! pg_stat_statements 1.4 is below the supported minimum 1.5"},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, out, _ := testApp(t, "")
			require.NoError(t, app.writeDoctor(pgcue.DoctorReport{
				Connection: postgres.ConnectionInfo{ServerMajorVersion: 14, ReadOnly: true},
				Extensions: []pgcue.DoctorExtension{
					{
						Name:    "pg_stat_statements",
						Version: test.version,
						Minimum: "1.5",
						Checks:  []string{"statements.top"},
					},
				},
			}))
			require.Contains(t, out.String(), "Optional extensions\n")
			require.Contains(t, out.String(), test.want)
			require.Contains(t, out.String(), "! Role is not a member of pg_monitor")
		})
	}
}

func TestDoctorReadinessErrors(t *testing.T) {
	for _, test := range []struct {
		name, want, finding string
		version             int
		readOnly            bool
	}{
		{
			"unsupported server",
			"PostgreSQL 13 is unsupported; pgcue requires PostgreSQL 14 or later",
			"PostgreSQL 13 is unsupported; pgcue requires PostgreSQL 14 or later",
			13, true,
		},
		{"writable connection", "PostgreSQL connection is not read-only", "Session is not read-only", 14, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, out, _ := testApp(t, "")
			app.Style = output.Style{Enabled: true}
			err := app.writeDoctor(pgcue.DoctorReport{
				Connection: postgres.ConnectionInfo{ServerMajorVersion: test.version, ReadOnly: test.readOnly},
			})
			require.EqualError(t, err, test.want)
			require.Contains(t, out.String(), "PostgreSQL readiness")
			require.Contains(t, out.String(), "\x1b[1;31m✗\x1b[0m")
			require.Contains(t, out.String(), test.finding)
		})
	}
}

func TestDoctorWriteError(t *testing.T) {
	app, _, _ := testApp(t, "")
	app.Out = testutil.FailingWriter{}
	require.EqualError(t, app.writeDoctor(pgcue.DoctorReport{}), "write failed")
}

func TestDoctorConnectionError(t *testing.T) {
	app, _, _ := testApp(t, "")
	err := app.Run(t.Context(), []string{"doctor", dsnFlag, invalidDSN})
	require.ErrorContains(t, err, "connect to PostgreSQL")
	require.NotNil(t, app.catalogue)
}

func TestDoctorPostgreSQL(t *testing.T) {
	app, out, stderr := testApp(t, "")
	args := []string{"doctor", dsnFlag, testutil.PostgresDSN(t)}
	require.Equal(t, ExitSuccess, Run(t.Context(), args, app.In, app.Out, app.Err), stderr.String())
	require.Empty(t, stderr.String())
	for _, want := range []string{"PostgreSQL readiness", "Server role: primary", "is supported", "Session is read-only"} {
		require.Contains(t, out.String(), want)
	}
}

func TestDoctorRefusedConnection(t *testing.T) {
	dsn, err := url.Parse(testutil.PostgresDSN(t))
	require.NoError(t, err)
	dsn.Host = net.JoinHostPort(dsn.Hostname(), "1")
	options := dsn.Query()
	options.Set("connect_timeout", "2")
	dsn.RawQuery = options.Encode()
	app, out, stderr := testApp(t, "")
	args := []string{"doctor", dsnFlag, dsn.String()}
	require.Equal(t, ExitFailure, Run(t.Context(), args, app.In, app.Out, app.Err))
	require.Empty(t, out.String())
	require.Contains(t, stderr.String(), "connect to PostgreSQL")
}
