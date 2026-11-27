package catalogue

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDependencyVersionComparison(t *testing.T) {
	for _, test := range []struct {
		dependency, left, right string
		want                    int
		valid                   bool
	}{
		{PostgreSQLDependency, "18", "17", 1, true},
		{PostgreSQLDependency, "17", "18", -1, true},
		{PostgreSQLDependency, "18", "18", 0, true},
		{"pg_stat_statements", "1.10", "1.9", 1, true},
		{"pg_stat_statements", "1.9", "1.9.0", 0, true},
		{"pg_stat_statements", "1.8", "1.9", -1, true},
		{"pg_stat_statements", "one", "1.9", 0, false},
		{PostgreSQLDependency, "18.1", "18", 0, false},
	} {
		t.Run(test.dependency+"/"+test.left+"/"+test.right, func(t *testing.T) {
			got, valid := compareVersions(test.dependency, test.left, test.right)
			require.Equal(t, test.want, got)
			require.Equal(t, test.valid, valid)
		})
	}
}

func TestSupportedDependencyVersions(t *testing.T) {
	for _, test := range []struct {
		dependency, version string
		supported           bool
	}{
		{PostgreSQLDependency, "14", true},
		{PostgreSQLDependency, "18", true},
		{PostgreSQLDependency, "13", false},
		{PostgreSQLDependency, "18.1", false},
		{"pg_stat_statements", "1.5", true},
		{"pg_stat_statements", "1.12", true},
		{"pg_stat_statements", "1.4", false},
		{"pg_buffercache", "1.3", true},
		{"pg_buffercache", "latest", false},
	} {
		t.Run(test.dependency+"/"+test.version, func(t *testing.T) {
			require.Equal(t, test.supported, CheckDependencyVersion(test.dependency, test.version) == nil)
		})
	}
}

func TestCheckServerVersion(t *testing.T) {
	require.NoError(t, CheckServerVersion(MinimumPostgreSQLVersion))
	require.EqualError(
		t,
		CheckServerVersion(13),
		"PostgreSQL 13 is unsupported; pgcue requires PostgreSQL 14 or later",
	)
}

func TestCheckDependencyVersion(t *testing.T) {
	require.NoError(t, CheckDependencyVersion("pg_stat_statements", "1.5"))
	require.EqualError(
		t,
		CheckDependencyVersion("pg_stat_statements", "1.4"),
		"pg_stat_statements 1.4 is unsupported; pgcue requires pg_stat_statements 1.5 or later",
	)
}
