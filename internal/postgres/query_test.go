package postgres

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/bolajiwahab/pgcue/internal/testutil"
)

func TestQueryStreamsPostgreSQLValues(t *testing.T) {
	conn := testutil.ConnectPostgres(t)
	result, err := (Executor{Conn: conn}).Query(t.Context(), "types", `
 SELECT n::bigint AS number, interval '1 day' AS duration, numeric '123.450' AS amount,
 B'101' AS flags, '550e8400-e29b-41d4-a716-446655440000'::uuid AS id,
 decode('0001ff','hex') AS data, NULL::text AS empty
 FROM generate_series(1,2) AS n`, nil)
	require.NoError(t, err)
	defer result.Close()
	require.Equal(t, []string{"number", "duration", "amount", "flags", "id", "data", "empty"}, result.Columns)
	var rows [][]any
	require.NoError(t, result.Rows(func(row []any) error { rows = append(rows, row); return nil }))
	require.Equal(t, [][]any{
		{int64(1), "1 day", "123.450", "101", "550e8400-e29b-41d4-a716-446655440000", `\x0001ff`, nil},
		{int64(2), "1 day", "123.450", "101", "550e8400-e29b-41d4-a716-446655440000", `\x0001ff`, nil},
	}, rows)
	require.NoError(t, conn.Ping(t.Context()))
}

// Tables show an array as PostgreSQL writes it; JSON shows its elements.
func TestQueryArrays(t *testing.T) {
	conn := testutil.ConnectPostgres(t)
	result, err := (Executor{Conn: conn}).Query(t.Context(), "arrays",
		"SELECT ARRAY[101, NULL]::int[] AS ids, ARRAY['a b', 'c'] AS names", nil)
	require.NoError(t, err)
	defer result.Close()
	var row []any
	require.NoError(t, result.Rows(func(values []any) error { row = values; return nil }))
	require.Equal(t, []string{"{101,NULL}", `{"a b",c}`}, []string{fmt.Sprint(row[0]), fmt.Sprint(row[1])})
	encoded, err := json.Marshal(row)
	require.NoError(t, err)
	require.JSONEq(t, `[[101, null], ["a b", "c"]]`, string(encoded))
}

func TestQueryClosesRowsWhenConsumerFails(t *testing.T) {
	conn := testutil.ConnectPostgres(t)
	result, err := (Executor{Conn: conn}).Query(t.Context(), "stream", "SELECT generate_series(1,100)", nil)
	require.NoError(t, err)
	defer result.Close()
	want := errors.New("consumer failed")
	require.ErrorIs(t, result.Rows(func([]any) error { return want }), want)
	require.NoError(t, conn.Ping(t.Context()), "connection must be reusable after consumer failure")
}

func TestEmptyResultKeepsColumns(t *testing.T) {
	conn := testutil.ConnectPostgres(t)
	result, err := (Executor{Conn: conn}).Query(t.Context(), "empty", "SELECT 1 AS value WHERE false", nil)
	require.NoError(t, err)
	defer result.Close()
	require.Equal(t, []string{"value"}, result.Columns)
	require.NoError(t, result.Rows(func([]any) error {
		t.Fatal("expected no rows")
		return nil
	}))
}

func TestQueryPreservesPostgreSQLErrors(t *testing.T) {
	for _, test := range []struct{ name, statement, code string }{
		{"query", "SELECT pgcue_missing_column", "42703"},
		{"stream", "SELECT 1/n FROM generate_series(1,0,-1) AS n", "22012"},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := testutil.ConnectPostgres(t)
			result, err := (Executor{Conn: conn}).Query(t.Context(), "test.failure", test.statement, nil)
			if err == nil {
				defer result.Close()
				err = result.Rows(func([]any) error { return nil })
			}
			require.ErrorContains(t, err, "test.failure")
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, test.code, pgErr.Code)
			require.NoError(t, conn.Ping(t.Context()))
		})
	}
}
