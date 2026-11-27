package catalogue

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

const pgStatStatementsDependency = "pg_stat_statements"

// testVariant requires the given PostgreSQL and pg_stat_statements versions, omitting
// empty ones. Its statement names those requirements, such as "16/1.9" or "16/".
func testVariant(postgres, statements string) SQLVariant {
	requires := map[string]Version{}
	if postgres != "" {
		requires[PostgreSQLDependency] = Version(postgres)
	}
	if statements != "" {
		requires[pgStatStatementsDependency] = Version(statements)
	}
	return SQLVariant{Requires: requires, Statement: postgres + "/" + statements}
}

// testEnvironment reports the installed PostgreSQL and pg_stat_statements versions, omitting empty ones.
func testEnvironment(postgres, statements string) map[string]string {
	environment := map[string]string{}
	if postgres != "" {
		environment[PostgreSQLDependency] = postgres
	}
	if statements != "" {
		environment[pgStatStatementsDependency] = statements
	}
	return environment
}

func TestSQLSelection(t *testing.T) {
	type requirement struct{ postgres, statements string }
	type selection struct{ postgres, statements, want string }
	for _, test := range []struct {
		name       string
		variants   []requirement
		selections []selection
	}{
		{
			"both dependencies",
			[]requirement{{"18", "1.9"}, {"14", "1.8"}, {"14", "1.5"}},
			[]selection{
				{"18", "1.12", "18/1.9"},
				{"18", "1.9", "18/1.9"},
				{"18", "1.8", "14/1.8"},
				{"18", "1.5", "14/1.5"},
				{"17", "1.9", "14/1.8"},
				{"14", "1.8", "14/1.8"},
				{"14", "1.5", "14/1.5"},
			},
		},
		{
			"PostgreSQL only",
			[]requirement{{"14", ""}, {"16", ""}, {"18", ""}},
			[]selection{{"14", "", "14/"}, {"17", "", "16/"}, {"18", "", "18/"}, {"19", "", "18/"}},
		},
		{
			"pg_stat_statements only",
			[]requirement{{"", "1.9"}, {"", "1.5"}, {"", "1.8"}},
			[]selection{
				{"", "1.5", "/1.5"}, {"", "1.7", "/1.5"}, {"", "1.8", "/1.8"}, {"", "1.9", "/1.9"}, {"", "1.12", "/1.9"},
			},
		},
		{
			"combined minimums",
			[]requirement{{"16", "1.9"}, {"14", "1.5"}, {"16", "1.5"}, {"14", "1.9"}},
			[]selection{
				{"15", "1.5", "14/1.5"},
				{"15", "1.9", "14/1.9"},
				{"16", "1.5", "16/1.5"},
				{"16", "1.9", "16/1.9"},
				{"18", "1.12", "16/1.9"},
			},
		},
		{
			"missing extension falls back to core",
			[]requirement{{"16", "1.9"}, {"16", ""}},
			[]selection{{"16", "", "16/"}, {"16", "1.9", "16/1.9"}},
		},
	} {
		// Declaration order must not affect selection.
		for _, order := range []string{"declared", "reversed"} {
			var check Check
			for _, variant := range test.variants {
				check.SQL = append(check.SQL, testVariant(variant.postgres, variant.statements))
			}
			if order == "reversed" {
				slices.Reverse(check.SQL)
			}
			for _, selection := range test.selections {
				name := test.name + "/" + order + "/" + selection.postgres + "/" + selection.statements
				t.Run(name, func(t *testing.T) {
					variant, err := check.VariantForEnvironment(
						testEnvironment(selection.postgres, selection.statements),
					)
					require.NoError(t, err)
					require.Equal(t, selection.want, variant.Statement)
				})
			}
		}
	}
}

func TestNoCompatibleSQL(t *testing.T) {
	check := Check{ID: "test.versioned", SQL: []SQLVariant{testVariant("14", "1.5")}}
	const prefix = "check \"test.versioned\": no compatible SQL variant for the current environment\n"
	for _, test := range []struct {
		name        string
		environment map[string]string
		want        string
	}{
		{"unsupported PostgreSQL", testEnvironment("13", "1.9"), "postgresql: 13\npg_stat_statements: 1.9"},
		{"missing extension", testEnvironment("18", ""), "postgresql: 18\npg_stat_statements: not installed"},
		{"unsupported extension", testEnvironment("18", "1.2"), "postgresql: 18\npg_stat_statements: 1.2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := check.VariantForEnvironment(test.environment)
			require.ErrorIs(t, err, errNoCompatibleSQL)
			require.EqualError(t, err, prefix+test.want)
		})
	}
}

func TestSQLVariantValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Check)
		want   string
	}{
		{
			"missing dependencies",
			func(check *Check) { check.SQL[0].Requires = nil },
			"SQL variant 1 requires at least one dependency",
		},
		{
			"empty statement",
			func(check *Check) { check.SQL[0].Statement = " " },
			"SQL variant 1 statement is required",
		},
		{
			"unsupported dependency",
			func(check *Check) { check.SQL[0].Requires = map[string]Version{"z": "1", "a": "1"} },
			`SQL variant 1: has unsupported dependency "a"`,
		},
		{
			"invalid version",
			func(check *Check) { check.SQL[0] = testVariant("latest", "latest") },
			`SQL variant 1: has invalid pg_stat_statements version "latest"`,
		},
		{
			"below supported minimum",
			func(check *Check) { check.SQL[0] = testVariant("13", "1.2") },
			"SQL variant 1: requires pg_stat_statements 1.2, below the supported minimum 1.5",
		},
		{
			"missing minimum variant",
			func(check *Check) { check.SQL[0] = testVariant("16", "1.9") },
			"no SQL variant declaring minimum pg_stat_statements version 1.5",
		},
		{
			"undeclared SQL parameter",
			func(check *Check) { check.SQL[0].Statement = "SELECT @z, @a" },
			`SQL variant 1: SQL parameters: SQL references undeclared parameter "z"`,
		},
		{
			"unused SQL parameter",
			func(check *Check) {
				check.Parameters = map[string]Parameter{"z": {Type: StringParameter}, "a": {Type: StringParameter}}
			},
			`SQL variant 1: SQL parameters: parameter "a" is declared but not referenced in SQL`,
		},
		{
			"undeclared extension",
			func(check *Check) {
				check.SQL[0].Statement = "SELECT * FROM {{ extension.pg_stat_statements.schema }}.pg_stat_statements"
			},
			`SQL variant 1: extension "pg_stat_statements" is referenced but not declared in requires`,
		},
		{
			"PostgreSQL as extension",
			func(check *Check) { check.SQL[0].Statement = "SELECT {{ extension.postgresql.schema }}" },
			"SQL variant 1: PostgreSQL cannot be referenced as an extension",
		},
		{
			"invalid metadata placeholder",
			func(check *Check) { check.SQL[0].Statement = "SELECT {{ extension.pg_stat_statements.version }}" },
			`SQL variant 1: unsupported metadata placeholder "{{ extension.pg_stat_statements.version }}"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			check := testCheck()
			test.mutate(&check)
			require.EqualError(t, validateCheckVariants(check), test.want)
		})
	}
}

func TestMalformedMetadataPlaceholders(t *testing.T) {
	for _, statement := range []string{
		"SELECT * FROM {{{ extension.pg_stat_statements.schema }}}.pg_stat_statements",
		"SELECT * FROM {{ extension.pg_stat_statements.schema.pg_stat_statements",
	} {
		t.Run(statement, func(t *testing.T) {
			check := testCheck()
			check.SQL[0].Statement = statement
			require.EqualError(t, validateCheckVariants(check), "SQL variant 1: malformed metadata placeholder")
		})
	}
}

func TestSQLVariantCompatibilityClosure(t *testing.T) {
	check := testCheck()
	check.SQL = nil

	for _, bufferVersion := range []Version{"1.3", "1.4"} {
		for _, extensionVersion := range []Version{"1.5", "1.9"} {
			for _, postgresVersion := range []Version{"14", "16"} {
				check.SQL = append(check.SQL, SQLVariant{
					Requires: map[string]Version{
						PostgreSQLDependency:       postgresVersion,
						pgStatStatementsDependency: extensionVersion,
						"pg_buffercache":           bufferVersion,
					},
					Statement: "SELECT 1",
				})
			}
		}
	}

	for _, test := range []struct {
		name string
		omit int
	}{
		{"complete", -1},
		{"missing pairwise intersection", 3},
		{"missing triple intersection", 7},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := check
			candidate.SQL = slices.Clone(check.SQL)

			if test.omit >= 0 {
				candidate.SQL = slices.Delete(
					candidate.SQL, test.omit, test.omit+1,
				)
			}

			err := validateCheckVariants(candidate)
			if test.omit == -1 {
				require.NoError(t, err)
				return
			}

			require.ErrorContains(t, err, "ambiguous compatibility requirements")
		})
	}
}

func TestEquivalentCompatibilityCasesAreDuplicates(t *testing.T) {
	for _, version := range []Version{"1.5", "1.5.0", "01.005.0"} {
		t.Run(string(version), func(t *testing.T) {
			check := testCheck()
			check.SQL = []SQLVariant{
				{
					Requires:  map[string]Version{pgStatStatementsDependency: "1.5"},
					Statement: "SELECT 1",
				},
				{
					Requires:  map[string]Version{pgStatStatementsDependency: version},
					Statement: "SELECT 2",
				},
			}

			require.ErrorContains(t,
				validateCheckVariants(check),
				"SQL variant 2 duplicates",
			)
		})
	}
}

func TestEquivalentMinimumVersionDeclarationIsPreserved(t *testing.T) {
	check := testCheck()
	check.SQL = []SQLVariant{{
		Requires: map[string]Version{
			pgStatStatementsDependency: "1.5.0",
		},
		Statement: "SELECT 1",
	}}

	require.NoError(t, validateCheckVariants(check))
	require.Equal(t,
		Version("1.5.0"),
		check.SQL[0].Requires[pgStatStatementsDependency],
	)
}

func TestNamedSQLParameterValidationUsesPGXLexing(t *testing.T) {
	parameters := map[string]Parameter{
		"value": {Type: StringParameter},
	}

	for _, sql := range []string{
		`SELECT @value, @value`,
		`SELECT '@quoted', "@identifier", E'\\@escaped', @value -- @line
/* @outer /* @inner */ @outer */`,
		`SELECT @value::text, '@missing', '@_missing'`,
	} {
		require.NoError(t, validateSQLParameters(parameters, sql))
	}

	// Placeholder validation is not SQL syntax validation.
	require.NoError(t, validateSQLParameters(nil, "not valid SQL"))
}

func TestResolveExtensionSchemas(t *testing.T) {
	sql := "SELECT @minimum_calls FROM {{ extension.pg_stat_statements.schema }}.stats JOIN {{ extension.pg_buffercache.schema }}.cache USING (id)"

	got := ResolveExtensionSchemas(sql, map[string]string{
		pgStatStatementsDependency: "monitoring",
		"pg_buffercache":           `odd" schema`,
	})
	require.Equal(t,
		`SELECT @minimum_calls FROM "monitoring".stats JOIN "odd"" schema".cache USING (id)`,
		got,
	)
}

func TestDependenciesAndCompatibility(t *testing.T) {
	check := Check{SQL: []SQLVariant{testVariant("18", "1.12")}}

	require.Equal(t,
		[]string{pgStatStatementsDependency, PostgreSQLDependency},
		check.Dependencies(),
	)
	require.Equal(t,
		"PostgreSQL 18+ with pg_stat_statements 1.12+",
		check.SQL[0].Compatibility(),
	)
}

func TestRequirementKey(t *testing.T) {
	require.Equal(t,
		"pg_stat_statements=1.5\x00",
		requirementKey(map[string]Version{
			"pg_stat_statements": "01.005.0",
		}),
	)
}

func TestMergeRequirements(t *testing.T) {
	left := map[string]Version{
		PostgreSQLDependency: "16",
		"pg_stat_statements": "1.9",
	}
	right := map[string]Version{
		PostgreSQLDependency: "14",
		"pg_stat_statements": "1.10",
		"pg_buffercache":     "1.3",
	}

	require.Equal(t, map[string]Version{
		PostgreSQLDependency: "16",
		"pg_stat_statements": "1.10",
		"pg_buffercache":     "1.3",
	}, mergeRequirements(left, right))
}
