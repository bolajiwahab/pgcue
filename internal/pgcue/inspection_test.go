package pgcue

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/postgres"
	"github.com/bolajiwahab/pgcue/internal/testutil"
)

func TestSessionInspection(t *testing.T) {
	conn := testutil.ConnectPostgres(t)
	session := &Session{DSN: conn.Config().ConnString()}
	t.Cleanup(func() { require.NoError(t, session.Close()) })
	require.Nil(t, session.conn, "session must connect lazily")
	catalog, err := catalogue.LoadCatalogue()
	require.NoError(t, err)
	target, err := session.Target(t.Context())
	require.NoError(t, err)
	require.Equal(t, conn.Config().Database, target.Database)
	require.Equal(t, conn.Config().User, target.User)
	require.True(t, target.ReadOnly)
	require.Equal(t, "primary", target.ServerRole)
	connected := session.conn
	report, err := session.Doctor(t.Context(), catalog)
	require.NoError(t, err)
	require.NoError(t, report.Err())
	require.Equal(t, target.Database, report.Connection.Database)
	require.Equal(t, target.ServerRole, report.Connection.ServerRole)
	require.Equal(t, target.ServerMajorVersion, report.Connection.ServerMajorVersion)
	var found bool
	for _, extension := range report.Extensions {
		if extension.Name == "pg_stat_statements" {
			found = true
			require.True(t, extension.Installed())
			require.Contains(t, extension.Checks, "statements.top")
		}
	}
	require.True(t, found, "doctor must include the catalogue's extension dependency")
	available, err := session.Availability(t.Context(), catalog)
	require.NoError(t, err)
	require.True(t, available["activity.connections"])
	require.True(t, available["statements.top"])
	require.Same(t, connected, session.conn, "operations must reuse the connection")
	require.NoError(t, session.Close())
	require.True(t, connected.IsClosed())
}

func TestSessionCachesEnvironmentUntilClose(t *testing.T) {
	setup := testutil.CreatePostgresDatabase(t)
	session := &Session{DSN: setup.Config().ConnString()}
	t.Cleanup(func() { require.NoError(t, session.Close()) })
	check := catalogue.Check{ID: "test.cache", SQL: []catalogue.SQLVariant{
		{Requires: map[string]catalogue.Version{"pg_buffercache": "1.3"}, Statement: "SELECT 1"},
	}}
	consume := func(result postgres.Result) error { return result.Rows(func([]any) error { return nil }) }
	require.ErrorContains(t, session.Execute(t.Context(), check, nil, consume), "pg_buffercache: not installed")
	_, err := setup.Exec(t.Context(), "CREATE EXTENSION pg_buffercache")
	require.NoError(t, err)
	require.ErrorContains(t, session.Execute(t.Context(), check, nil, consume), "pg_buffercache: not installed")
	require.NoError(t, session.Close())
	require.NoError(
		t,
		session.Execute(t.Context(), check, nil, consume),
		"a new connection must refresh dependency metadata",
	)
}
