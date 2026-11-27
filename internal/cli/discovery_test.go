package cli

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bolajiwahab/pgcue/internal/output"
	"github.com/bolajiwahab/pgcue/internal/testutil"
)

func TestFind(t *testing.T) {
	for _, test := range []struct {
		terms []string
		want  []string
	}{
		{[]string{"slow", "database"}, []string{"Investigations", "slow-database", "Run: pgcue investigate slow-database"}},
		{[]string{"crash", "recovery", "durability"}, []string{"Topics", "wal", "Read: pgcue describe wal"}},
		{[]string{"vacuum"}, []string{"Topics\n", "Investigations\n", "Checks\n"}},
		{[]string{"pgcue_nonexistent_search_term"}, []string{
			`No topics, investigations, or checks found for "pgcue_nonexistent_search_term".`,
		}},
	} {
		t.Run(strings.Join(test.terms, " "), func(t *testing.T) {
			app, out, _ := testApp(t, "")
			require.NoError(t, app.Run(t.Context(), append([]string{"find"}, test.terms...)))
			previous := -1
			for _, want := range test.want {
				position := strings.Index(out.String(), want)
				require.Greater(t, position, previous, "%q must appear in result order", want)
				previous = position
			}
			require.False(t, strings.HasPrefix(out.String(), "\n"))
		})
	}
}

func TestDiscoveryLists(t *testing.T) {
	for _, test := range []struct {
		command string
		want    []string
	}{
		{"topic", []string{"PostgreSQL topics", "activity", "wal", "pgcue describe <topic>"}},
		{"investigate", []string{
			"Investigations", "slow-database", "Investigate reports of slow database-backed operations.",
			"pgcue describe <investigation>", "pgcue investigate <investigation>",
		}},
	} {
		t.Run(test.command, func(t *testing.T) {
			app, out, _ := testApp(t, "")
			require.NoError(t, app.Run(t.Context(), []string{test.command}))
			for _, want := range test.want {
				require.Contains(t, out.String(), want)
			}
		})
	}
}

func TestListGroupsChecksByTopic(t *testing.T) {
	app, out, _ := testApp(t, "")
	app.Style = output.Style{Enabled: true}
	require.NoError(t, app.Run(t.Context(), []string{"list"}))
	previous := -1
	for _, want := range []string{
		"\x1b[1;36mDatabase activity\x1b[0m", "activity.connections", "Storage", "storage.table_size",
	} {
		position := strings.Index(out.String(), want)
		require.Greater(t, position, previous, "%q must appear in topic order", want)
		previous = position
	}
}

func TestListAvailabilityMarkers(t *testing.T) {
	app, out, _ := testApp(t, "")
	require.NoError(t, app.Run(t.Context(), []string{"list"}))
	require.NotContains(t, out.String(), "✓ available")
	out.Reset()
	require.NoError(t, app.writeCheckList(map[string]bool{"storage.table_size": true, "activity.connections": false}))
	require.Contains(t, out.String(), "✓ available   – unavailable")
	require.Contains(t, out.String(), "✓ storage.table_size")
	require.Contains(t, out.String(), "– activity.connections")
}

func TestListOptionErrors(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{dsnFlag, "local"}, "list connection options require " + availableFlag},
		{[]string{availableFlag, availableFlag}, "option " + availableFlag + " specified more than once"},
		{[]string{availableFlag, "--unknown"}, `unknown option "--unknown"`},
		{[]string{availableFlag + "=yes"}, availableFlag + " does not take a value"},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			app, out, _ := testApp(t, "")
			err := app.Run(t.Context(), append([]string{"list"}, test.args...))
			require.ErrorContains(t, err, test.want)
			var exitError *ExitError
			require.ErrorAs(t, err, &exitError)
			require.Equal(t, ExitInvalidInput, exitError.Code)
			require.Empty(t, out.String())
		})
	}
}

func TestListAvailableConnectionError(t *testing.T) {
	app, _, _ := testApp(t, "")
	err := app.Run(t.Context(), []string{"list", availableFlag, dsnFlag, invalidDSN})
	require.ErrorContains(t, err, "connect to PostgreSQL")
}

func TestListAvailablePostgreSQL(t *testing.T) {
	dsn, err := url.Parse(testutil.PostgresDSN(t))
	require.NoError(t, err)
	dsn.Path = "/pgcue" // The Compose database without optional extensions.
	app, out, stderr := testApp(t, "")
	args := []string{"list", availableFlag, dsnFlag, dsn.String()}
	require.Equal(t, ExitSuccess, Run(t.Context(), args, app.In, app.Out, app.Err), stderr.String())
	require.Empty(t, stderr.String())
	require.Contains(t, out.String(), "✓ activity.connections")
	require.Contains(t, out.String(), "– statements.top")
}
