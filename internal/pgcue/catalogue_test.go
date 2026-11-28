package pgcue

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/postgres"
	"github.com/bolajiwahab/pgcue/internal/testutil"
)

// testedPostgreSQLVersions are the PostgreSQL versions in the CI test matrix.
var testedPostgreSQLVersions = []int{14, 15, 16, 17, 18}

// requiredValues supplies the parameters that have no defaults.
var requiredValues = map[string]string{"schema": "public", "table": "pgcue_check"}

// TestEverySQLVariantRunsInCI fails when a variant would never be selected on a tested
// PostgreSQL version, so TestCatalogueOnPostgreSQL would never run it.
func TestEverySQLVariantRunsInCI(t *testing.T) {
	catalog, err := catalogue.LoadCatalogue()
	require.NoError(t, err)
	for _, check := range catalog.Checks() {
		for _, variant := range check.SQL {
			require.True(t,
				slices.ContainsFunc(testedPostgreSQLVersions, func(major int) bool {
					return selects(check, variant, major)
				}),
				"%s: no tested PostgreSQL version selects the %s variant", check.ID, variant.Compatibility(),
			)
		}
	}
}

// TestCatalogueOnPostgreSQL runs every check with the server's default extension
// versions, then each variant this server selects with its minimum extension versions.
// Extensions are installed in a non-public schema to exercise schema placeholders.
func TestCatalogueOnPostgreSQL(t *testing.T) {
	catalog, err := catalogue.LoadCatalogue()
	require.NoError(t, err)
	server := testutil.ConnectPostgres(t)
	var major int
	require.NoError(t, server.QueryRow(t.Context(),
		"SELECT pg_catalog.current_setting('server_version_num')::integer / 10000").Scan(&major))

	t.Run("default extension versions", func(t *testing.T) { testDefaultExtensionVersions(t, catalog) })
	t.Run("minimum extension versions", func(t *testing.T) {
		testMinimumExtensionVersions(t, catalog, server, major)
	})
}

func testDefaultExtensionVersions(t *testing.T, catalog *catalogue.Catalogue) {
	extensions := map[string]string{}
	for _, dependency := range catalog.Dependencies() {
		if dependency != catalogue.PostgreSQLDependency {
			extensions[dependency] = ""
		}
	}
	session := catalogueSession(t, extensions)
	for _, check := range catalog.Checks() {
		t.Run(check.ID, func(t *testing.T) { runCheck(t, session, check) })
	}
}

// testMinimumExtensionVersions shares a database between variants that need the same
// extension versions.
func testMinimumExtensionVersions(t *testing.T, catalog *catalogue.Catalogue, server *pgx.Conn, major int) {
	sessions := map[string]*Session{}
	for _, check := range catalog.Checks() {
		for _, variant := range check.SQL {
			extensions := minimumExtensions(variant)
			key := fmt.Sprint(extensions)
			skip := skipReason(t, server, check, variant, major)
			if _, exists := sessions[key]; !exists && skip == "" {
				sessions[key] = catalogueSession(t, extensions)
			}
			t.Run(check.ID+"/"+variant.Compatibility(), func(t *testing.T) {
				if skip != "" {
					t.Skip(skip)
				}
				runCheck(t, sessions[key], check)
			})
		}
	}
}

// skipReason explains why this server cannot run variant, or returns an empty string.
func skipReason(t *testing.T, server *pgx.Conn, check catalogue.Check, variant catalogue.SQLVariant, major int) string {
	t.Helper()
	if !selects(check, variant, major) {
		return fmt.Sprintf("PostgreSQL %d selects another variant", major)
	}
	extensions := minimumExtensions(variant)
	for _, name := range slices.Sorted(maps.Keys(extensions)) {
		var available bool
		require.NoError(t, server.QueryRow(t.Context(),
			"SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_available_extension_versions WHERE name = $1 AND version = $2)",
			name, extensions[name]).Scan(&available))
		if !available {
			return fmt.Sprintf("PostgreSQL %d does not provide %s %s", major, name, extensions[name])
		}
	}
	return ""
}

// selects reports whether a PostgreSQL major version selects variant when only the
// variant's extensions are installed, at their minimum versions.
func selects(check catalogue.Check, variant catalogue.SQLVariant, major int) bool {
	environment := minimumExtensions(variant)
	environment[catalogue.PostgreSQLDependency] = strconv.Itoa(major)
	selected, err := check.VariantForEnvironment(environment)
	return err == nil && maps.Equal(selected.Requires, variant.Requires)
}

func minimumExtensions(variant catalogue.SQLVariant) map[string]string {
	extensions := map[string]string{}
	for name, version := range variant.Requires {
		if name != catalogue.PostgreSQLDependency {
			extensions[name] = string(version)
		}
	}
	return extensions
}

// catalogueSession opens a session on a new database with the given extensions, where an
// empty version installs the server's default.
func catalogueSession(t *testing.T, extensions map[string]string) *Session {
	t.Helper()
	setup := testutil.CreatePostgresDatabase(t)
	statements := []string{
		"CREATE SCHEMA monitoring",
		"CREATE TABLE public.pgcue_check (id bigint PRIMARY KEY, value text)",
	}
	for _, name := range slices.Sorted(maps.Keys(extensions)) {
		statement := "CREATE EXTENSION " + pgx.Identifier{name}.Sanitize() + " WITH SCHEMA monitoring"
		if version := extensions[name]; version != "" {
			statement += " VERSION " + pgx.Identifier{version}.Sanitize()
		}
		statements = append(statements, statement)
	}
	for _, statement := range statements {
		_, err := setup.Exec(t.Context(), statement)
		require.NoError(t, err, statement)
	}
	session := &Session{DSN: setup.Config().ConnString()}
	t.Cleanup(func() { require.NoError(t, session.Close()) })
	return session
}

func runCheck(t *testing.T, session *Session, check catalogue.Check) {
	t.Helper()
	supplied := map[string]string{}
	for name, parameter := range check.Parameters {
		if parameter.Required {
			value, ok := requiredValues[name]
			require.True(t, ok, "add a test value for required parameter %q", name)
			supplied[name] = value
		}
	}
	arguments, err := catalogue.ResolveParameters(check.Parameters, supplied)
	require.NoError(t, err)
	require.NoError(t, session.Execute(t.Context(), check, arguments, func(result postgres.Result) error {
		require.ElementsMatch(t, slices.Collect(maps.Keys(check.Columns)), result.Columns,
			"returned columns must match the documented columns")
		return result.Rows(func([]any) error { return nil })
	}))
}
