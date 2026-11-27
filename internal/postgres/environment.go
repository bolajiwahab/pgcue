package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const connectionInfoSQL = `
WITH session_context AS (
    SELECT CAST(pg_catalog.current_setting('server_version_num') AS integer) / 10000 AS server_major_version
         , CASE
             WHEN pg_catalog.pg_is_in_recovery()
               THEN 'replica'
             ELSE 'primary'
           END AS server_role
         , pg_catalog.current_database() AS database_name
         , CURRENT_USER AS user_name
)
SELECT server_major_version
     , server_role
     , database_name
     , user_name
     , CAST(pg_catalog.current_setting('default_transaction_read_only') AS boolean) AS default_read_only
     , pg_catalog.pg_has_role(session_context.user_name, 'pg_monitor', 'MEMBER') AS has_pg_monitor
     , pg_catalog.pg_has_role(session_context.user_name, 'pg_read_all_stats', 'USAGE') AS has_pg_read_all_stats
  FROM session_context;`

const extensionLookupSQL = `
SELECT e.extversion
     , n.nspname
  FROM pg_catalog.pg_extension AS e
 INNER JOIN pg_catalog.pg_namespace AS n
    ON n.oid = e.extnamespace
 WHERE e.extname = $1;`

// Extension identifies an extension and contains its installed version and schema, if present.
type Extension struct {
	Name    string
	Version string
	Schema  string
}

// Installed reports whether the extension was found in the database.
func (extension Extension) Installed() bool { return extension.Version != "" }

// ConnectionInfo describes the current database session.
type ConnectionInfo struct {
	ServerMajorVersion int
	ServerRole         string
	Database           string
	User               string
	ReadOnly           bool
	PGMonitor          bool
	ReadAllStats       bool
}

// ConnectionInfo inspects the current database session.
func (e Executor) ConnectionInfo(ctx context.Context) (ConnectionInfo, error) {
	var info ConnectionInfo
	err := e.Conn.QueryRow(ctx, connectionInfoSQL).Scan(
		&info.ServerMajorVersion,
		&info.ServerRole,
		&info.Database,
		&info.User,
		&info.ReadOnly,
		&info.PGMonitor,
		&info.ReadAllStats,
	)
	if err != nil {
		return ConnectionInfo{}, fmt.Errorf("inspect PostgreSQL connection: %w", err)
	}
	return info, nil
}

// LookupExtension always returns the requested name; a missing extension has no version.
func (e Executor) LookupExtension(ctx context.Context, name string) (Extension, error) {
	extension := Extension{Name: name}
	err := e.Conn.QueryRow(ctx, extensionLookupSQL, name).Scan(&extension.Version, &extension.Schema)
	if errors.Is(err, pgx.ErrNoRows) {
		return extension, nil
	}
	if err != nil {
		return Extension{Name: name}, fmt.Errorf("determine %s extension metadata: %w", name, err)
	}
	return extension, nil
}
