package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/output"
	"github.com/bolajiwahab/pgcue/internal/testutil"
)

// outcomesCatalogue has a check for each way a run can end: demo.succeeds; demo.unavailable,
// which needs pg_buffercache, an extension the test database does not install; demo.fails;
// and demo.slow, which runs until interrupted. The investigation demo-outcomes runs the
// first three in that order, and demo-slow runs demo.slow.
func outcomesCatalogue(t *testing.T) *catalogue.Catalogue {
	t.Helper()
	check := func(name, statement string, requires map[string]catalogue.Version) catalogue.Check {
		requires[catalogue.PostgreSQLDependency] = "14"
		return catalogue.Check{
			ID: "demo." + name, TopicID: "demo", Name: name, Description: "Test check.",
			Result:   catalogue.CurrentResult,
			Guidance: catalogue.Guidance{UseWhen: []string{"Testing."}, Inspect: []string{"The value."}},
			Columns:  map[string]string{"value": "Test value."},
			SQL:      []catalogue.SQLVariant{{Requires: requires, Statement: statement}},
		}
	}
	investigation := func(id string, checks ...string) catalogue.Investigation {
		steps := make([]catalogue.InvestigationStep, len(checks))
		for i, check := range checks {
			steps[i] = catalogue.InvestigationStep{Check: check, Reason: "Testing."}
		}
		return catalogue.Investigation{
			ID: id, Name: id, Description: "Test investigation.", Topics: []string{"demo"}, Steps: steps,
		}
	}
	catalog, err := catalogue.NewCatalogue(
		[]catalogue.Topic{{
			ID: "demo", Name: "Demo", Description: "Test topic.", Overview: "Test overview.",
			KeyConcepts: []string{"Test concept."},
		}},
		[]catalogue.Check{
			check("succeeds", "SELECT 1 AS value", map[string]catalogue.Version{}),
			check("unavailable", "SELECT 1 AS value", map[string]catalogue.Version{"pg_buffercache": "1.3"}),
			check("fails", "SELECT 1/0 AS value", map[string]catalogue.Version{}),
			check("slow", "SELECT pg_sleep(10) AS value", map[string]catalogue.Version{}),
		},
		[]catalogue.Investigation{
			investigation("demo-outcomes", "demo.succeeds", "demo.unavailable", "demo.fails"),
			investigation("demo-slow", "demo.slow"),
		},
	)
	require.NoError(t, err)
	return catalog
}

func TestRunConnectionsPostgreSQL(t *testing.T) {
	app, out, stderr := testApp(t, "")
	args := []string{
		"run", "activity.connections", dsnFlag, testutil.PostgresDSN(t),
		"--limit", "NULL", outputFlag, "json",
	}
	require.Equal(t, ExitSuccess, Run(t.Context(), args, app.In, app.Out, app.Err), stderr.String())
	require.Empty(t, stderr.String())
	var document struct {
		Rows []struct {
			Database    string `json:"database_name"`
			Application string `json:"application_name"`
			Connections int64  `json:"connections"`
		} `json:"rows"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &document))
	for _, row := range document.Rows {
		if row.Database == "pgcue_extensions" && row.Application == programName {
			require.GreaterOrEqual(t, row.Connections, int64(1))
			return
		}
	}
	t.Fatal("results must include the CLI connection")
}

func TestRunPostgreSQLCompatibility(t *testing.T) {
	dsn := testutil.PostgresDSN(t)
	// Tuple counters changed in 16; WAL counters changed in 18. The remaining
	// checks exercise representative system views and relation-size queries.
	for _, id := range []string{
		"tables.tuple_activity", "wal.statistics", "io.background_writer",
		"storage.largest_tables", "storage.largest_indexes", "indexes.unused", "indexes.duplicate", "locks.blocked",
	} {
		t.Run(id, func(t *testing.T) {
			app, out, stderr := testApp(t, "")
			args := []string{"run", id, dsnFlag, dsn, outputFlag, "json"}
			require.Equal(t, ExitSuccess, Run(t.Context(), args, app.In, app.Out, app.Err), stderr.String())
			require.Empty(t, stderr.String())
			var document struct {
				Columns []string `json:"columns"`
			}
			require.NoError(t, json.Unmarshal(out.Bytes(), &document))
			require.Greater(t, len(document.Columns), 1, "JSON must include columns, even without result rows")
		})
	}
}

func TestRunMonitoringWarningsPostgreSQL(t *testing.T) {
	dsn, err := url.Parse(testutil.PostgresDSN(t))
	require.NoError(t, err)
	dsn.User = url.UserPassword("pgcue_reader", "pgcue_reader")
	for _, id := range []string{"activity.connections", "locks.blocked", "statements.top"} {
		t.Run(id, func(t *testing.T) {
			app, out, stderr := testApp(t, "")
			args := []string{"run", id, dsnFlag, dsn.String(), outputFlag, "json"}
			require.Equal(t, ExitSuccess, Run(t.Context(), args, app.In, app.Out, app.Err), stderr.String())
			require.Contains(t, stderr.String(), "warning:")
			require.Contains(t, stderr.String(), "Results may be incomplete")
			var document struct {
				Rows []json.RawMessage `json:"rows"`
			}
			require.NoError(t, json.Unmarshal(out.Bytes(), &document))
			require.NotNil(t, document.Rows, "results must include rows")
		})
	}
}

// renderCounter cancels watching once it has received the table header renders times.
type renderCounter struct {
	renders int
	cancel  context.CancelFunc
}

func (w *renderCounter) Write(p []byte) (int, error) {
	if w.renders += bytes.Count(p, []byte("database_name")); w.renders >= 2 {
		w.cancel()
	}
	return len(p), nil
}

func TestRunWatchRepeatsUntilInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	app, _, stderr := testApp(t, "")
	out := &renderCounter{cancel: cancel}
	args := []string{
		"run", "activity.connections", dsnFlag, testutil.PostgresDSN(t), watchFlag, "10ms",
	}
	require.Equal(t, ExitInterrupted, Run(ctx, args, app.In, out, app.Err), stderr.String())
	require.Empty(t, stderr.String())
	require.GreaterOrEqual(t, out.renders, 2, "watch must render repeatedly until interrupted")
}

func TestRunWatchStopsAfterCountPostgreSQL(t *testing.T) {
	app, out, stderr := testApp(t, "")
	app.catalogue = outcomesCatalogue(t)
	args := []string{"run", "demo.succeeds", dsnFlag, testutil.PostgresDSN(t), watchFlag, "1ms", countFlag, "2"}
	require.NoError(t, app.Run(t.Context(), args))
	require.Empty(t, stderr.String())
	// A blank line sets each run apart from the one before.
	require.Equal(t, "value\n─────\n1    \n\nvalue\n─────\n1    \n", out.String())
}

func TestRunWatchReportsFailedRuns(t *testing.T) {
	for _, test := range []struct {
		count int
		want  string
	}{
		{1, "1 of 1 run failed"},
		{3, "3 of 3 runs failed"},
	} {
		t.Run(test.want, func(t *testing.T) {
			app, _, stderr := testApp(t, "")
			args := []string{
				"run", "activity.connections", dsnFlag, invalidDSN,
				watchFlag, "1ms", countFlag, strconv.Itoa(test.count),
			}
			require.EqualError(t, app.Run(t.Context(), args), test.want)
			// Watching reports each failed run and carries on.
			require.Equal(t, test.count, strings.Count(stderr.String(), "error: connect to PostgreSQL"))
		})
	}
}

func TestRunInvestigationPostgreSQL(t *testing.T) {
	dsn := testutil.PostgresDSN(t)
	const want = "investigation completed with 1 failed check"

	t.Run("table", func(t *testing.T) {
		app, out, stderr := testApp(t, "")
		app.catalogue = outcomesCatalogue(t)
		require.EqualError(t, app.Run(t.Context(), []string{"run", "demo-outcomes", dsnFlag, dsn}), want)
		require.Equal(t,
			"Check: demo.succeeds\n\nvalue\n─────\n1    \n"+
				"\nCheck: demo.unavailable\n\n"+
				"\nCheck: demo.fails\n\n",
			out.String(),
		)
		// Every step runs; an unavailable step is a warning, and only a failed step is an error.
		require.Contains(t, stderr.String(), `warning: check "demo.unavailable": no compatible SQL variant`)
		require.Contains(t, stderr.String(), "pg_buffercache: not installed")
		require.Regexp(t, `\nerror: .*demo\.fails.*division by zero`, stderr.String())
	})

	t.Run("json", func(t *testing.T) {
		app, out, stderr := testApp(t, "")
		app.catalogue = outcomesCatalogue(t)
		args := []string{"run", "demo-outcomes", dsnFlag, dsn, outputFlag, "json"}
		require.EqualError(t, app.Run(t.Context(), args), want)
		require.Empty(t, stderr.String(), "JSON output reports each step's problem in the document")
		var document struct {
			Investigation string `json:"investigation"`
			Results       []struct {
				Check  string           `json:"check"`
				Status string           `json:"status"`
				Rows   []map[string]any `json:"rows"`
				Error  string           `json:"error"`
			} `json:"results"`
		}
		require.NoError(t, json.Unmarshal(out.Bytes(), &document))
		require.Equal(t, "demo-outcomes", document.Investigation)
		statuses := make([][2]string, len(document.Results))
		for i, result := range document.Results {
			statuses[i] = [2]string{result.Check, result.Status}
		}
		require.Equal(t, [][2]string{
			{"demo.succeeds", output.Succeeded},
			{"demo.unavailable", output.Unavailable},
			{"demo.fails", output.Failed},
		}, statuses)
		require.Equal(t, []map[string]any{{"value": float64(1)}}, document.Results[0].Rows)
		require.Contains(t, document.Results[1].Error, "pg_buffercache: not installed")
		require.Contains(t, document.Results[2].Error, "division by zero")
	})
}

func TestRunInvestigationConnectionFailure(t *testing.T) {
	args := []string{"run", "slow-database", dsnFlag, invalidDSN}

	t.Run("table", func(t *testing.T) {
		app, out, _ := testApp(t, "")
		require.ErrorContains(t, app.Run(t.Context(), args), "connect to PostgreSQL")
		require.Empty(t, out.String(), "no step runs without a connection")
	})

	t.Run("json", func(t *testing.T) {
		app, out, _ := testApp(t, "")
		err := app.Run(t.Context(), slices.Concat(args, []string{outputFlag, "json"}))
		require.ErrorContains(t, err, "connect to PostgreSQL")
		// The run still writes its document, with the error instead of results.
		var document map[string]any
		require.NoError(t, json.Unmarshal(out.Bytes(), &document))
		require.Equal(t, "slow-database", document["investigation"])
		require.Equal(t, err.Error(), document["error"])
		require.NotContains(t, document, "results")
	})
}

func TestRunInterruptedDuringQueryReportsNothing(t *testing.T) {
	dsn := testutil.PostgresDSN(t)
	for _, args := range [][]string{
		{"run", "demo.slow", outputFlag, "json"},
		{"run", "demo.slow", watchFlag, "1s"},
		{"run", "demo-slow", outputFlag, "json"},
	} {
		t.Run(strings.Join(args[1:], " "), func(t *testing.T) {
			app, out, stderr := testApp(t, "")
			app.catalogue = outcomesCatalogue(t)
			ctx, cancel := context.WithCancel(t.Context())
			// Interrupt while demo.slow's query runs.
			time.AfterFunc(200*time.Millisecond, cancel)
			require.ErrorIs(t, app.Run(ctx, slices.Concat(args, []string{dsnFlag, dsn})), context.Canceled)
			require.Empty(t, out.String(), "an interrupted run writes no result")
			require.Empty(t, stderr.String(), "an interrupted run is not reported as failed")
		})
	}
}
