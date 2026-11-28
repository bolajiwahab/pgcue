package pgcue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/postgres"
	"github.com/bolajiwahab/pgcue/internal/testutil"
)

func TestExecuteCheck(t *testing.T) {
	for _, test := range []struct{ dependency, minimum, statement string }{
		{"postgresql", "14", "SELECT @value::bigint AS value"},
		{"pg_stat_statements", "1.5", "SELECT @value::bigint AS value FROM (SELECT count(*) FROM {{ extension.pg_stat_statements.schema }}.pg_stat_statements) AS statements"},
	} {
		t.Run(test.dependency, func(t *testing.T) {
			session := &Session{conn: testutil.ConnectPostgres(t)}
			check := catalogue.Check{ID: "test.selected", SQL: []catalogue.SQLVariant{
				{Requires: map[string]catalogue.Version{test.dependency: "99"}, Statement: "SELECT 'future' AS value"},
				{
					Requires:  map[string]catalogue.Version{test.dependency: catalogue.Version(test.minimum)},
					Statement: test.statement,
				},
			}}
			require.NoError(
				t,
				session.Execute(t.Context(), check, pgx.NamedArgs{"value": 42}, func(result postgres.Result) error {
					require.Equal(t, []string{"value"}, result.Columns)
					var rows [][]any
					err := result.Rows(func(row []any) error { rows = append(rows, row); return nil })
					require.Equal(t, [][]any{{int64(42)}}, rows)
					return err
				}),
			)
		})
	}
}

func TestExecuteRequiresInstalledExtension(t *testing.T) {
	session := &Session{conn: testutil.ConnectPostgres(t)}
	check := catalogue.Check{ID: "test.extension", SQL: []catalogue.SQLVariant{
		{Requires: map[string]catalogue.Version{"pg_buffercache": "1.3"}, Statement: "SELECT 1"},
	}}
	err := session.Execute(t.Context(), check, nil, func(postgres.Result) error {
		t.Fatal("a check with a missing dependency must not execute")
		return nil
	})
	require.ErrorContains(t, err, "pg_buffercache: not installed")
}

func TestExecutionTimeout(t *testing.T) {
	session := &Session{conn: testutil.ConnectPostgres(t), Timeout: 20 * time.Millisecond}
	check := catalogue.Check{
		ID: "test.timeout",
		SQL: []catalogue.SQLVariant{
			{Requires: map[string]catalogue.Version{"postgresql": "14"}, Statement: "SELECT pg_sleep(1)"},
		},
	}
	err := session.Execute(
		t.Context(),
		check,
		nil,
		func(result postgres.Result) error { return result.Rows(func([]any) error { return nil }) },
	)
	require.ErrorContains(t, err, "test.timeout timed out")
}

func TestExecutionClosesResultsWhenConsumerFails(t *testing.T) {
	session := &Session{conn: testutil.ConnectPostgres(t)}
	check := catalogue.Check{
		ID: "test.consumer",
		SQL: []catalogue.SQLVariant{
			{Requires: map[string]catalogue.Version{"postgresql": "14"}, Statement: "SELECT 1"},
		},
	}
	want := errors.New("consumer failed")
	require.ErrorIs(t, session.Execute(t.Context(), check, nil, func(postgres.Result) error { return want }), want)
	require.NoError(t, session.conn.Ping(t.Context()), "unconsumed results must be closed")
}

func TestUnsupportedDependencies(t *testing.T) {
	for _, test := range []struct {
		version         int
		extension, want string
	}{
		{13, "", "requires PostgreSQL 14 or later"},
		{14, "1.4", "requires pg_stat_statements 1.5 or later"},
	} {
		check := catalogue.Check{
			ID: "test.unsupported",
			SQL: []catalogue.SQLVariant{
				{Requires: map[string]catalogue.Version{"pg_stat_statements": "1.5"}, Statement: "SELECT 1"},
			},
		}
		_, err := executeCheck(
			t.Context(),
			postgres.Executor{},
			databaseEnvironment{
				connectionInfo: postgres.ConnectionInfo{ServerMajorVersion: test.version},
				extensions: map[string]postgres.Extension{
					"pg_stat_statements": {Version: test.extension, Schema: "public"},
				},
			},
			check,
			nil,
		)
		require.ErrorContains(t, err, test.want)
	}
}

func TestOperationTimeoutError(t *testing.T) {
	want := errors.New("inspection failed")
	require.ErrorIs(t, operationError(
		context.Background(),
		context.Background(),
		time.Second,
		"operation",
		want,
	), want)
	checkCtx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-checkCtx.Done()
	require.ErrorContains(t, operationError(
		context.Background(),
		checkCtx,
		time.Nanosecond,
		"operation",
		want,
	), "timed out after 1ns")
}

func TestSessionOperationsRejectInvalidConnections(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(*Session) error
	}{
		{"connect", func(s *Session) error { return s.Connect(context.Background()) }},
		{"execute", func(s *Session) error {
			return s.Execute(context.Background(), catalogue.Check{}, nil, func(postgres.Result) error {
				return errors.New("unexpected consumer call")
			})
		}},
		{"target", func(s *Session) error { _, err := s.Target(context.Background()); return err }},
		{"doctor", func(s *Session) error { _, err := s.Doctor(context.Background(), new(catalogue.Catalogue)); return err }},
		{"availability", func(s *Session) error {
			_, err := s.Availability(context.Background(), new(catalogue.Catalogue))
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &Session{DSN: "postgres://localhost:invalid/pgcue"}
			require.ErrorContains(t, test.run(session), "connect to PostgreSQL")
			require.NoError(t, session.Close())
		})
	}
}

func TestVisibilityWarnings(t *testing.T) {
	for _, test := range []struct {
		role     string
		warnings int
	}{
		{"pgcue_reader", 1},
		{"pg_read_all_stats", 0},
		{"pg_monitor", 0},
	} {
		t.Run(test.role, func(t *testing.T) {
			conn := testutil.ConnectPostgres(t)
			_, err := conn.Exec(t.Context(), "SET ROLE "+pgx.Identifier{test.role}.Sanitize())
			require.NoError(t, err)
			var warnings []string
			session := &Session{conn: conn, Warn: func(message string) error {
				warnings = append(warnings, message)
				return nil
			}}
			check := catalogue.Check{ID: "test.visibility", RequiresMonitor: true, SQL: []catalogue.SQLVariant{
				{Requires: map[string]catalogue.Version{"postgresql": "14"}, Statement: "SELECT 42"},
			}}
			for range 2 {
				require.NoError(t, session.Execute(t.Context(), check, nil, func(result postgres.Result) error {
					return result.Rows(func(row []any) error {
						require.Equal(t, []any{int32(42)}, row)
						return nil
					})
				}))
			}
			require.Len(t, warnings, test.warnings)
			for _, message := range warnings {
				require.Contains(t, message, "Results may be incomplete")
			}
		})
	}
}

func TestVisibilityWarningFailureStopsExecution(t *testing.T) {
	conn := testutil.ConnectPostgres(t)
	_, err := conn.Exec(t.Context(), "SET ROLE pgcue_reader")
	require.NoError(t, err)
	want := errors.New("warning output failed")
	session := &Session{conn: conn, Warn: func(string) error { return want }}
	check := catalogue.Check{ID: "test.warning", RequiresMonitor: true, SQL: []catalogue.SQLVariant{
		{Requires: map[string]catalogue.Version{"postgresql": "14"}, Statement: "SELECT 1/0"},
	}}
	require.ErrorIs(t, session.Execute(t.Context(), check, nil, func(postgres.Result) error {
		t.Fatal("execution must stop when the warning cannot be delivered")
		return nil
	}), want)
}
