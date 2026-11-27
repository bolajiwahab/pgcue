// Package postgres provides PostgreSQL connections, inspection, and query execution.
package postgres

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// Connect opens a read-only PostgreSQL connection.
func Connect(ctx context.Context, dsn string) (*pgx.Conn, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	return connectConfig(ctx, config)
}

func connectConfig(ctx context.Context, config *pgx.ConnConfig) (*pgx.Conn, error) {
	config.RuntimeParams["application_name"] = "pgcue"
	config.RuntimeParams["default_transaction_read_only"] = "on"
	config.RuntimeParams["search_path"] = "pg_catalog, pg_temp"
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf(
			"connect to PostgreSQL (%s): %w",
			Target(config),
			err,
		)
	}
	return conn, nil
}

// Target returns a credential-free host, port, and database.
func Target(config *pgx.ConnConfig) string {
	target := net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port)))
	if config.Database != "" {
		target += "/" + config.Database
	}
	return target
}

const connectionCloseTimeout = 5 * time.Second

type connectionCloser interface{ Close(context.Context) error }

// Close releases a connection using a bounded context independent of cancellation.
// When a cancelled context interrupted a query, pgx abandons the connection and sends
// PostgreSQL a cancel request in the background; Close waits for that cleanup, so the
// query does not keep running on the server after pgcue exits.
func Close(conn *pgx.Conn) error {
	return closeConnection(conn, conn.PgConn().CleanupDone())
}

func closeConnection(conn connectionCloser, cleanupDone <-chan struct{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), connectionCloseTimeout)
	defer cancel()
	err := conn.Close(ctx)
	select {
	case <-cleanupDone:
	case <-ctx.Done():
	}
	return err
}
