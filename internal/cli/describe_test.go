package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bolajiwahab/pgcue/internal/output"
)

func TestDescribeCheck(t *testing.T) {
	for _, test := range []struct {
		id   string
		want []string
	}{
		{"storage.table_size", []string{
			"pgcue run storage.table_size --schema <schema> --table <table>",
			"Point-in-time measurement at execution time.",
			"Use when:", "Inspect:", "Result columns:", "schema_name:", "total_size:",
			"Related checks:", "storage.largest_tables", "Compatibility:", "PostgreSQL 14+",
			"Type:        string\n    Required:    yes", "Description: Schema containing the table.",
		}},
		{"statements.top", []string{
			"Cumulative statistics since PostgreSQL last reset the relevant counters.",
			"Default:     null", "Default:     2", "Default:     20",
		}},
		{"activity.connections", []string{
			"pgcue run activity.connections", "Current state at execution time.", "Minimum:     1",
			"Use NULL for no limit.",
		}},
		{"storage.cluster_size", []string{"Parameters:\n  None\n"}},
	} {
		t.Run(test.id, func(t *testing.T) {
			app, out, _ := testApp(t, "")
			require.NoError(t, app.Run(t.Context(), []string{"describe", test.id}))
			for _, want := range test.want {
				require.Contains(t, out.String(), want)
			}
		})
	}
}

func TestDescribeDocuments(t *testing.T) {
	for _, test := range []struct {
		args []string
		want []string
	}{
		{[]string{"describe", "wal"}, []string{
			"Write-ahead log", "WAL generation and WAL retention", "Key concepts:", "Investigations:", "wal-disk-growth",
		}},
		{[]string{"topic", "wal"}, []string{"Write-ahead log", "Related checks:", "wal.statistics"}},
		{[]string{"describe", "slow-database"}, []string{
			"Slow database or requests", "Find frequently executed statements", "Run: pgcue run statements.top",
			"Run the complete investigation:\n  pgcue investigate slow-database",
		}},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			app, out, _ := testApp(t, "")
			require.NoError(t, app.Run(t.Context(), test.args))
			for _, want := range test.want {
				require.Contains(t, out.String(), want)
			}
		})
	}
}

func TestSQL(t *testing.T) {
	for _, test := range []struct {
		id      string
		headers []string
		query   string
	}{
		{"storage.table_size", []string{"-- PostgreSQL 14+"}, "SELECT @schema::text"},
		{"statements.top", []string{
			"-- pg_stat_statements 1.5+", "-- pg_stat_statements 1.8+", "-- pg_stat_statements 1.9+",
		}, "{{ extension.pg_stat_statements.schema }}"},
	} {
		t.Run(test.id, func(t *testing.T) {
			app, out, _ := testApp(t, "")
			app.Style = output.Style{Enabled: true}
			require.NoError(t, app.Run(t.Context(), []string{"sql", test.id}))
			require.NotContains(t, out.String(), "\x1b[")
			require.Contains(t, out.String(), test.query)
			previous := -1
			for _, header := range test.headers {
				position := strings.Index(out.String(), header)
				require.Greater(t, position, previous, "variant %s must appear in declaration order", header)
				previous = position
			}
		})
	}
}

func TestUnknownDocument(t *testing.T) {
	for _, test := range []struct{ command, id, want string }{
		{"describe", "missing", `unknown check, topic, or investigation "missing"`},
		{"sql", "missing", `unknown check "missing"`},
		{"topic", "wla", `unknown topic "wla"; did you mean "wal"?`},
	} {
		t.Run(test.command, func(t *testing.T) {
			app, out, _ := testApp(t, "")
			err := app.Run(t.Context(), []string{test.command, test.id})
			require.ErrorContains(t, err, test.want)
			var exitError *ExitError
			require.ErrorAs(t, err, &exitError)
			require.Equal(t, ExitInvalidInput, exitError.Code)
			require.Empty(t, out.String())
		})
	}
}
