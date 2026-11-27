package postgres

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bolajiwahab/pgcue/internal/testutil"
)

func TestInspectDatabase(t *testing.T) {
	conn := testutil.ConnectPostgres(t)
	_, err := conn.Exec(t.Context(), "SET default_transaction_read_only = on")
	require.NoError(t, err)
	executor := Executor{Conn: conn}
	info, err := executor.ConnectionInfo(t.Context())
	require.NoError(t, err)
	require.Equal(t, conn.Config().Database, info.Database)
	require.Equal(t, conn.Config().User, info.User)
	require.True(t, info.ReadOnly)
	require.Equal(t, "primary", info.ServerRole)
	require.GreaterOrEqual(t, info.ServerMajorVersion, 14)
	extension, err := executor.LookupExtension(t.Context(), "pgcue_missing_extension")
	require.NoError(t, err)
	require.False(t, extension.Installed())
	require.Equal(t, Extension{Name: "pgcue_missing_extension"}, extension)
}

func TestInspectExtension(t *testing.T) {
	conn := testutil.ConnectPostgres(t)
	extension, err := (Executor{Conn: conn}).LookupExtension(t.Context(), "pg_stat_statements")
	require.NoError(t, err)
	require.True(t, extension.Installed())
	require.Equal(t, "pg_stat_statements", extension.Name)
	require.Equal(t, "monitoring", extension.Schema)
	require.NotEmpty(t, extension.Version)
	t.Logf("pg_stat_statements %s in schema %s", extension.Version, extension.Schema)
}

func TestInspectionErrors(t *testing.T) {
	conn := testutil.ConnectPostgres(t)
	require.NoError(t, Close(conn))
	executor := Executor{Conn: conn}
	_, err := executor.ConnectionInfo(t.Context())
	require.ErrorContains(t, err, "inspect PostgreSQL connection")
	extension, err := executor.LookupExtension(t.Context(), "pg_stat_statements")
	require.ErrorContains(t, err, "extension metadata")
	require.False(t, extension.Installed())
	require.Equal(t, Extension{Name: "pg_stat_statements"}, extension)
}
