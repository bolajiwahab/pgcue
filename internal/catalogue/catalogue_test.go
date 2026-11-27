package catalogue

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func testTopic() Topic {
	return Topic{
		ID:          "activity",
		Name:        "Activity",
		Description: "Sessions and transactions.",
		Overview:    "Each connection has a backend.",
		KeyConcepts: []string{"Idle transactions retain locks."},
	}
}

func testCheck() Check {
	return Check{
		ID:          "activity.connections",
		TopicID:     "activity",
		Name:        "Connections",
		Description: "Inspect sessions.",
		Result:      CurrentResult,
		Guidance: Guidance{
			UseWhen: []string{"Requests are slow."},
			Inspect: []string{"Connection counts."},
		},
		Columns: map[string]string{"count": "Session count."},
		SQL: []SQLVariant{{
			Requires: map[string]Version{
				PostgreSQLDependency: "14",
			},
			Statement: "SELECT 1 AS count",
		}},
	}
}

func testInvestigation() Investigation {
	return Investigation{
		ID:          "investigate",
		Name:        "Investigate",
		Description: "Investigate requests.",
		Topics:      []string{"activity"},
		Steps: []InvestigationStep{{
			Check:  "activity.connections",
			Reason: "Inspect sessions.",
		}},
	}
}

func testCatalogue(t *testing.T) *Catalogue {
	t.Helper()

	topicWithoutChecks := Topic{
		ID:          "empty",
		Name:        "Empty Topic",
		Description: "Topic without checks.",
		Overview:    "No operational checks are available for this topic.",
		KeyConcepts: []string{"Catalogue organization."},
	}

	metricsTopic := Topic{
		ID:          "metrics",
		Name:        "Metrics",
		Description: "Aggregated measurements.",
		Overview:    "Checks report measurements.",
		KeyConcepts: []string{"Independent observations."},
	}

	parameterizedCheck := Check{
		ID:          "activity.parameterized",
		TopicID:     "activity",
		Name:        "Parameterized check",
		Description: "Inspect values using a parameter.",
		Result:      CurrentResult,
		Guidance: Guidance{
			UseWhen: []string{"Requests are slow."},
			Inspect: []string{"Result values."},
		},
		Columns: map[string]string{
			"count": "Result count.",
		},
		Parameters: map[string]Parameter{
			"minimum_count": {
				Type:        IntegerParameter,
				Description: "Selection threshold.",
			},
		},
		SQL: []SQLVariant{{
			Requires: map[string]Version{
				PostgreSQLDependency: "14",
			},
			Statement: "SELECT @minimum_count AS count",
		}},
	}

	extensionCheck := Check{
		ID:          "metrics.extension",
		TopicID:     "metrics",
		Name:        "Extension check",
		Description: "Check requiring an extension.",
		Result:      CurrentResult,
		Guidance: Guidance{
			UseWhen: []string{"Extension metrics are needed."},
			Inspect: []string{"Extension values."},
		},
		Columns: map[string]string{
			"value": "Reported value.",
		},
		SQL: []SQLVariant{{
			Requires: map[string]Version{
				"pg_buffercache": "1.3",
			},
			Statement: "SELECT 1 AS value",
		}},
	}

	singleTopicInvestigation := testInvestigation()

	multiTopicInvestigation := testInvestigation()
	multiTopicInvestigation.ID = "multitopic"
	multiTopicInvestigation.Topics = []string{"activity", "empty"}

	c, err := NewCatalogue(
		[]Topic{metricsTopic, topicWithoutChecks, testTopic()},
		[]Check{extensionCheck, parameterizedCheck, testCheck()},
		[]Investigation{multiTopicInvestigation, singleTopicInvestigation},
	)
	require.NoError(t, err)

	return c
}

func checkIDs(checks []Check) []string {
	var ids []string
	for _, check := range checks {
		ids = append(ids, check.ID)
	}
	return ids
}

func topicIDs(topics []Topic) []string {
	var ids []string
	for _, topic := range topics {
		ids = append(ids, topic.ID)
	}
	return ids
}

func investigationIDs(investigations []Investigation) []string {
	var ids []string
	for _, investigation := range investigations {
		ids = append(ids, investigation.ID)
	}
	return ids
}

func TestCatalogueListsAreOrdered(t *testing.T) {
	c := testCatalogue(t)

	require.Equal(t,
		[]string{"activity", "empty", "metrics"},
		topicIDs(c.Topics()),
	)
	require.Equal(t,
		[]string{"activity", "empty", "metrics"},
		c.TopicIDs(),
	)
	require.Equal(t,
		[]string{"activity.connections", "activity.parameterized", "metrics.extension"},
		checkIDs(c.Checks()),
	)
	require.Equal(t,
		[]string{"investigate", "multitopic"},
		investigationIDs(c.Investigations()),
	)
	require.Equal(t,
		[]string{"pg_buffercache", "postgresql"},
		c.Dependencies(),
	)
}

func TestCatalogueFilters(t *testing.T) {
	c := testCatalogue(t)

	for _, test := range []struct {
		name      string
		got, want []string
	}{
		{
			"topic",
			checkIDs(c.ChecksForTopic("activity")),
			[]string{"activity.connections", "activity.parameterized"},
		},
		{
			"metrics topic",
			checkIDs(c.ChecksForTopic("metrics")),
			[]string{"metrics.extension"},
		},
		{
			"PostgreSQL",
			c.CheckIDsUsing(PostgreSQLDependency),
			[]string{"activity.connections", "activity.parameterized"},
		},
		{
			"extension",
			c.CheckIDsUsing("pg_buffercache"),
			[]string{"metrics.extension"},
		},
		{
			"topic investigations",
			investigationIDs(c.InvestigationsForTopic("activity")),
			[]string{"investigate", "multitopic"},
		},
		{
			"explicit association without checks",
			investigationIDs(c.InvestigationsForTopic("empty")),
			[]string{"multitopic"},
		},
		{
			"empty topic",
			checkIDs(c.ChecksForTopic("empty")),
			nil,
		},
		{
			"unknown topic",
			checkIDs(c.ChecksForTopic("missing")),
			nil,
		},
		{
			"unknown dependency",
			c.CheckIDsUsing("missing"),
			nil,
		},
		{
			"unknown investigation topic",
			investigationIDs(c.InvestigationsForTopic("missing")),
			nil,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, test.got)
		})
	}
}

func TestCatalogueSearch(t *testing.T) {
	c := testCatalogue(t)

	for _, test := range []struct {
		terms                          string
		topics, checks, investigations []string
	}{
		{
			"SESSION COUNT",
			nil,
			[]string{"activity.connections"},
			nil,
		},
		{
			"idle locks",
			[]string{"activity"},
			nil,
			nil,
		},
		{
			"investigate requests",
			nil,
			nil,
			[]string{"investigate", "multitopic"},
		},
		{
			"minimum_count selection",
			nil,
			[]string{"activity.parameterized"},
			nil,
		},
		{
			"requests slow",
			nil,
			[]string{"activity.connections", "activity.parameterized"},
			nil,
		},
		{
			"reported value",
			nil,
			[]string{"metrics.extension"},
			nil,
		},
		{
			"activity.connections",
			nil,
			[]string{"activity.connections"},
			[]string{"investigate", "multitopic"},
		},
		{
			"empty",
			[]string{"empty"},
			nil,
			[]string{"multitopic"},
		},
		{
			"sessions missing",
			nil,
			nil,
			nil,
		},
		{
			" \t",
			nil,
			nil,
			nil,
		},
	} {
		t.Run(test.terms, func(t *testing.T) {
			got := c.Search(test.terms)

			require.Equal(t, test.topics, topicIDs(got.Topics))
			require.Equal(t, test.checks, checkIDs(got.Checks))
			require.Equal(t, test.investigations, investigationIDs(got.Investigations))
		})
	}
}

func TestCatalogueLookup(t *testing.T) {
	c := testCatalogue(t)

	t.Run("topic", func(t *testing.T) {
		got, exists := c.GetTopic("activity")
		require.True(t, exists)
		require.Equal(t, testTopic(), got)
	})

	t.Run("check", func(t *testing.T) {
		got, exists := c.GetCheck("activity.connections")
		require.True(t, exists)
		require.Equal(t, testCheck(), got)
	})

	t.Run("investigation", func(t *testing.T) {
		got, exists := c.GetInvestigation("investigate")
		require.True(t, exists)
		require.Equal(t, testInvestigation(), got)
	})
}

func TestCatalogueUnknownIDs(t *testing.T) {
	c := testCatalogue(t)

	check, exists := c.GetCheck("missing")
	require.False(t, exists)
	require.Equal(t, Check{}, check)

	topic, exists := c.GetTopic("missing")
	require.False(t, exists)
	require.Equal(t, Topic{}, topic)

	investigation, exists := c.GetInvestigation("missing")
	require.False(t, exists)
	require.Equal(t, Investigation{}, investigation)
}
