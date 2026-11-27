package catalogue

import (
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

const (
	topicYAML = `name: Activity
description: Sessions and transactions.
overview: Each connection has a backend.
key_concepts: [Idle transactions retain locks.]
`

	checkYAML = `name: Connections
description: Inspect sessions.
result: current
guidance:
  use_when: [Requests are slow.]
  inspect: [Connection counts.]
columns: {count: Session count.}
sql:
  - requires: {postgresql: '14'}
    statement: SELECT 1 AS count
`

	investigationYAML = `name: Investigate
description: Investigate requests.
topics: [activity]
steps:
  - check: activity.connections
    reason: Inspect sessions.
`
)

func testKnowledgeFS() fstest.MapFS {
	return fstest.MapFS{
		"topics/activity/topic.yaml":              {Data: []byte(topicYAML)},
		"topics/activity/checks/connections.yaml": {Data: []byte(checkYAML)},
		"investigations/investigate.yaml":         {Data: []byte(investigationYAML)},
	}
}

func TestLoadEmbeddedCatalogue(t *testing.T) {
	c, err := LoadCatalogue()
	require.NoError(t, err)
	require.NotEmpty(t, c.Checks())
}

func TestLoadCatalogueDerivesIDsFromPaths(t *testing.T) {
	content := testKnowledgeFS()
	content["embed.go"] = &fstest.MapFile{Data: []byte("not YAML")}
	content["notes.txt"] = &fstest.MapFile{Data: []byte("not YAML")}

	c, err := loadCatalogue(content)
	require.NoError(t, err)

	require.Equal(t, []string{"activity"}, c.TopicIDs())
	require.Equal(t, []string{"activity.connections"}, checkIDs(c.Checks()))
	require.Equal(t, []string{"investigate"}, investigationIDs(c.Investigations()))

	check, exists := c.GetCheck("activity.connections")
	require.True(t, exists)
	require.Equal(t, "activity", check.TopicID)
}

func TestLoadCatalogueRejectsYMLExtension(t *testing.T) {
	content := testKnowledgeFS()
	content["topics/activity/checks/sessions.yml"] = &fstest.MapFile{Data: []byte(checkYAML)}
	_, err := loadCatalogue(content)
	require.EqualError(
		t,
		err,
		`unsupported knowledge document "topics/activity/checks/sessions.yml": use the .yaml extension`,
	)
}

func TestLoadCatalogueRejectsUnsupportedPaths(t *testing.T) {
	for _, filename := range []string{
		"notes.yaml",
		"activity/topic.yaml",
		"topics/activity.yaml",
		"topics/activity/connections.yaml",
		"topics/activity/checks/nested/connections.yaml",
		"topics/nested/activity/topic.yaml",
		"investigations/nested/investigate.yaml",
	} {
		t.Run(filename, func(t *testing.T) {
			content := testKnowledgeFS()
			content[filename] = &fstest.MapFile{Data: []byte(topicYAML)}

			_, err := loadCatalogue(content)
			require.EqualError(t, err, fmt.Sprintf(
				"unsupported knowledge document %q: expected topics/<topic>/topic.yaml, topics/<topic>/checks/<check>.yaml, or investigations/<investigation>.yaml",
				filename,
			))
		})
	}
}

func TestLoadCatalogueRejectsInvalidIDs(t *testing.T) {
	for _, test := range []struct {
		filename, data, want string
	}{
		{
			"topics/Bad/topic.yaml",
			topicYAML,
			`topic "Bad": invalid topic ID "Bad"`,
		},
		{
			"topics/bad.topic/topic.yaml",
			topicYAML,
			`topic "bad.topic": invalid topic ID "bad.topic"`,
		},
		{
			"topics/Bad/checks/connections.yaml",
			checkYAML,
			`check "Bad.connections": invalid topic ID "Bad"`,
		},
		{
			"topics/activity/checks/Bad.yaml",
			checkYAML,
			`check "activity.Bad": invalid check ID "activity.Bad" for topic "activity"`,
		},
		{
			"topics/activity/checks/.yaml",
			checkYAML,
			`check "activity.": invalid check ID "activity." for topic "activity"`,
		},
		{
			"topics/activity/checks/bad.check.yaml",
			checkYAML,
			`check "activity.bad.check": invalid check ID "activity.bad.check" for topic "activity"`,
		},
		{
			"investigations/Bad.yaml",
			investigationYAML,
			`investigation "Bad": invalid investigation ID "Bad"`,
		},
		{
			"investigations/.yaml",
			investigationYAML,
			`investigation "": invalid investigation ID ""`,
		},
	} {
		t.Run(test.filename, func(t *testing.T) {
			content := testKnowledgeFS()
			content[test.filename] = &fstest.MapFile{Data: []byte(test.data)}

			_, err := loadCatalogue(content)
			require.EqualError(t, err, test.want)
		})
	}
}

func TestLoadCatalogueRejectsDerivedIdentityFields(t *testing.T) {
	for _, test := range []struct {
		filename, field, documentType, data string
	}{
		{"topics/activity/topic.yaml", "id", "Topic", topicYAML},
		{"topics/activity/topic.yaml", "ID", "Topic", topicYAML},
		{"topics/activity/checks/connections.yaml", "id", "Check", checkYAML},
		{"topics/activity/checks/connections.yaml", "topic_id", "Check", checkYAML},
		{"investigations/investigate.yaml", "id", "Investigation", investigationYAML},
	} {
		t.Run(test.filename+"/"+test.field, func(t *testing.T) {
			content := testKnowledgeFS()

			// Place the unknown field first for a stable YAML line number.
			content[test.filename].Data = []byte(test.field + ": override\n" + test.data)

			_, err := loadCatalogue(content)
			require.EqualError(t, err, fmt.Sprintf(
				"parse %q: yaml: unmarshal errors:\n  line 1: field %s not found in type catalogue.%s",
				test.filename, test.field, test.documentType,
			))
		})
	}
}

func TestDecodeYAMLRejectsInvalidDocuments(t *testing.T) {
	for _, test := range []struct {
		name, content, want string
	}{
		{
			"empty",
			"",
			`parse "topic.yaml": expected a YAML mapping`,
		},
		{
			"comment only",
			"# comment\n",
			`parse "topic.yaml": expected a YAML mapping`,
		},
		{
			"null",
			"null",
			`parse "topic.yaml": expected a YAML mapping`,
		},
		{
			"scalar",
			"plain text",
			`parse "topic.yaml": expected a YAML mapping`,
		},
		{
			"sequence",
			"[]",
			`parse "topic.yaml": expected a YAML mapping`,
		},
		{
			"multiple documents",
			"name: A\n---\nname: B",
			`parse "topic.yaml": multiple YAML documents are not supported`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var topic Topic
			err := decodeYAML("topic.yaml", []byte(test.content), &topic)
			require.EqualError(t, err, test.want)
		})
	}
}

func TestDecodeYAMLRejectsMalformedDocuments(t *testing.T) {
	for _, test := range []struct {
		name, content string
	}{
		{"invalid syntax", "name: ["},
		{"duplicate keys", "name: A\nname: B"},
		{"unknown field", "unknown: value"},
		{
			"malformed second document",
			"name: A\n---\nname: [",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var topic Topic
			err := decodeYAML("topic.yaml", []byte(test.content), &topic)
			require.ErrorContains(t, err, `parse "topic.yaml":`)
		})
	}
}

func TestParameterYAML(t *testing.T) {
	for _, test := range []struct {
		name, metadata, want string
		value                any
	}{
		{"null default", "{type: string, default: null}", "", nil},
		{"false default", "{type: boolean, default: false}", "", false},
		{"zero default", "{type: integer, default: 0}", "", 0},
		{"scalar metadata", "invalid", "parameter must be a YAML mapping", nil},
		{"unknown field", "{type: string, unknown: true}", "unknown parameter field", nil},
		{"structured default", "{type: string, default: {nested: value}}", "must be a scalar value", nil},
		{"required null default", "{type: string, required: true, default: null}", "cannot be required and have a default", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := testKnowledgeFS()
			content["topics/activity/checks/connections.yaml"].Data = []byte(
				strings.Replace(
					checkYAML,
					"SELECT 1 AS count",
					"SELECT @value AS count",
					1,
				) + "parameters:\n  value: " + test.metadata + "\n",
			)

			c, err := loadCatalogue(content)
			if test.want != "" {
				require.ErrorContains(t, err, test.want)
				return
			}

			require.NoError(t, err)

			check, exists := c.GetCheck("activity.connections")
			require.True(t, exists)

			value, set := check.Parameters["value"].DefaultValue()
			require.True(t, set)
			require.Equal(t, test.value, value)
		})
	}
}

func TestDependencyVersionYAML(t *testing.T) {
	for _, test := range []struct {
		version, want string
	}{
		{"'14'", ""},
		{"14", "version must be a string"},
		{"latest", "invalid postgresql version"},
	} {
		t.Run(test.version, func(t *testing.T) {
			content := testKnowledgeFS()
			content["topics/activity/checks/connections.yaml"].Data = []byte(
				strings.Replace(checkYAML, "'14'", test.version, 1),
			)

			_, err := loadCatalogue(content)
			if test.want == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.want)
		})
	}
}

type failingOpenFS struct {
	fs.FS
	path string
}

func (f failingOpenFS) Open(name string) (fs.File, error) {
	if name == f.path {
		return nil, fs.ErrPermission
	}
	return f.FS.Open(name)
}

func TestLoadCatalogueFilesystemErrors(t *testing.T) {
	for _, test := range []struct {
		name, path, want string
	}{
		{
			"walk error",
			".",
			`walk knowledge catalogue at ".": permission denied`,
		},
		{
			"read error",
			"topics/activity/topic.yaml",
			`read knowledge catalogue at "topics/activity/topic.yaml": permission denied`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := failingOpenFS{
				FS:   testKnowledgeFS(),
				path: test.path,
			}

			_, err := loadCatalogue(content)
			require.ErrorIs(t, err, fs.ErrPermission)
			require.EqualError(t, err, test.want)
		})
	}
}
