package catalogue

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTopicValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Topic)
		want   string
	}{
		{"invalid ID", func(topic *Topic) {
			topic.ID = "Bad"
		}, `invalid topic ID "Bad"`},
		{"missing name", func(topic *Topic) {
			topic.Name = ""
		}, "name is required"},
		{"missing description", func(topic *Topic) {
			topic.Description = ""
		}, "description is required"},
		{"missing overview", func(topic *Topic) {
			topic.Overview = ""
		}, "overview is required"},
		{"missing concepts", func(topic *Topic) {
			topic.KeyConcepts = nil
		}, "key_concepts are required"},
		{"empty concept", func(topic *Topic) {
			topic.KeyConcepts = []string{" "}
		}, "key_concepts entry 1 must not be empty"},
		{"duplicate concept", func(topic *Topic) {
			topic.KeyConcepts = []string{"one", "one"}
		}, `key_concepts entry 2 repeats "one"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			topic := testTopic()
			test.mutate(&topic)

			require.EqualError(t, validateTopic(topic), test.want)
		})
	}
}

func TestCheckValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Check)
		want   string
	}{
		{"invalid topic ID", func(check *Check) {
			check.TopicID = "Bad"
		}, `invalid topic ID "Bad"`},
		{"invalid check ID", func(check *Check) {
			check.ID = "activity.Bad"
		}, `invalid check ID "activity.Bad" for topic "activity"`},
		{"missing name", func(check *Check) {
			check.Name = ""
		}, "name is required"},
		{"missing description", func(check *Check) {
			check.Description = ""
		}, "description is required"},
		{"missing result", func(check *Check) {
			check.Result = ""
		}, "result is required"},
		{"invalid result", func(check *Check) {
			check.Result = "forever"
		}, `unsupported result semantics "forever"`},
		{"missing use when", func(check *Check) {
			check.Guidance.UseWhen = nil
		}, "guidance.use_when is required"},
		{"empty use when", func(check *Check) {
			check.Guidance.UseWhen = []string{" "}
		}, "guidance.use_when entry 1 must not be empty"},
		{"missing inspect", func(check *Check) {
			check.Guidance.Inspect = nil
		}, "guidance.inspect is required"},
		{"empty inspect", func(check *Check) {
			check.Guidance.Inspect = []string{" "}
		}, "guidance.inspect entry 1 must not be empty"},
		{"empty related", func(check *Check) {
			check.Guidance.Related = []string{" "}
		}, "guidance.related entry 1 must not be empty"},
		{"self reference", func(check *Check) {
			check.Guidance.Related = []string{check.ID}
		}, `guidance.related entry 1 references the check itself "activity.connections"`},
		{"duplicate related", func(check *Check) {
			check.Guidance.Related = []string{"activity.other", "activity.other"}
		}, `guidance.related entry 2 repeats "activity.other"`},
		{"missing columns", func(check *Check) {
			check.Columns = nil
		}, "at least one column is required"},
		{"empty column name", func(check *Check) {
			check.Columns = map[string]string{" ": "description"}
		}, "column name must not be empty"},
		{"empty column description", func(check *Check) {
			check.Columns = map[string]string{"count": " "}
		}, `column "count" description must not be empty`},
		{"missing SQL", func(check *Check) {
			check.SQL = nil
		}, "sql is required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			check := testCheck()
			test.mutate(&check)

			require.EqualError(t, validateCheck(check), test.want)
		})
	}
}

func TestInvestigationValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Investigation)
		want   string
	}{
		{"invalid ID", func(investigation *Investigation) {
			investigation.ID = "Bad"
		}, `invalid investigation ID "Bad"`},
		{"missing name", func(investigation *Investigation) {
			investigation.Name = ""
		}, "name is required"},
		{"missing description", func(investigation *Investigation) {
			investigation.Description = ""
		}, "description is required"},
		{"missing topics", func(investigation *Investigation) {
			investigation.Topics = nil
		}, "topics are required"},
		{"invalid topic", func(investigation *Investigation) {
			investigation.Topics = []string{"Bad"}
		}, `invalid topic ID "Bad"`},
		{"duplicate topic", func(investigation *Investigation) {
			investigation.Topics = []string{"activity", "activity"}
		}, `duplicate topic "activity"`},
		{"missing steps", func(investigation *Investigation) {
			investigation.Steps = nil
		}, "steps are required"},
		{"missing step check", func(investigation *Investigation) {
			investigation.Steps[0].Check = ""
		}, "step 1 check is required"},
		{"missing step reason", func(investigation *Investigation) {
			investigation.Steps[0].Reason = " "
		}, "step 1 reason is required"},
		{"duplicate step", func(investigation *Investigation) {
			investigation.Steps = []InvestigationStep{
				{Check: "activity.connections", Reason: "First"},
				{Check: "activity.connections", Reason: "Second"},
			}
		}, `step 2 repeats check "activity.connections"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			investigation := testInvestigation()
			test.mutate(&investigation)

			require.EqualError(t, validateInvestigation(investigation), test.want)
		})
	}
}

func TestCatalogueCheckReferences(t *testing.T) {
	unknownTopic := testCheck()
	unknownTopic.ID = "missing.connections"
	unknownTopic.TopicID = "missing"

	unknownRelatedCheck := testCheck()
	unknownRelatedCheck.Guidance.Related = []string{"activity.missing"}

	for _, test := range []struct {
		name  string
		check Check
		want  string
	}{
		{
			"unknown topic",
			unknownTopic,
			`check "missing.connections": references unknown topic "missing"`,
		},
		{
			"unknown related check",
			unknownRelatedCheck,
			`check "activity.connections": guidance.related entry 1 references unknown check "activity.missing"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewCatalogue([]Topic{testTopic()}, []Check{test.check}, nil)
			require.EqualError(t, err, test.want)
		})
	}
}

func TestCatalogueInvestigationReferences(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Investigation, *Check)
		want   string
	}{
		{
			"ID used by a topic",
			func(investigation *Investigation, _ *Check) { investigation.ID = "empty" },
			`investigation "empty": ID is also used by a topic`,
		},
		{
			"unknown topic",
			func(investigation *Investigation, _ *Check) { investigation.Topics = []string{"missing"} },
			`investigation "investigate": topics entry 1 references unknown topic "missing"`,
		},
		{
			"unknown check",
			func(investigation *Investigation, _ *Check) { investigation.Steps[0].Check = "activity.missing" },
			`investigation "investigate": step 1 references unknown check "activity.missing"`,
		},
		{
			"undeclared topic",
			func(investigation *Investigation, _ *Check) { investigation.Topics = []string{"empty"} },
			`investigation "investigate": step 1 references check "activity.connections" from undeclared topic "activity"`,
		},
		{
			"check with a required parameter",
			func(_ *Investigation, check *Check) {
				check.Parameters = map[string]Parameter{"database": {Type: StringParameter, Required: true}}
				check.SQL[0].Statement = "SELECT count(*) AS count FROM pg_stat_activity WHERE datname = @database"
			},
			`investigation "investigate": step 1 references check "activity.connections", ` +
				`which requires parameter "database"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			investigation, check := testInvestigation(), testCheck()
			test.mutate(&investigation, &check)

			emptyTopic := testTopic()
			emptyTopic.ID = "empty"

			_, err := NewCatalogue(
				[]Topic{testTopic(), emptyTopic},
				[]Check{check},
				[]Investigation{investigation},
			)
			require.EqualError(t, err, test.want)
		})
	}
}

func TestCatalogueAllowsCyclicRelatedReferences(t *testing.T) {
	first := testCheck()
	second := testCheck()
	second.ID = "activity.second"

	first.Guidance.Related = []string{second.ID}
	second.Guidance.Related = []string{first.ID}

	_, err := NewCatalogue(
		[]Topic{testTopic()},
		[]Check{first, second},
		nil,
	)
	require.NoError(t, err)
}

func TestValidateCheckParameters(t *testing.T) {
	for _, test := range []struct {
		name       string
		parameters map[string]Parameter
		want       string
	}{
		{
			name: "valid defaults",
			parameters: map[string]Parameter{
				"text":    {Type: StringParameter, Default: "hello"},
				"count":   {Type: IntegerParameter, Default: 3},
				"enabled": {Type: BooleanParameter, Default: true},
				"delay":   {Type: DurationParameter, Default: "5m"},
				"limit":   {Type: IntegerParameter, Default: "NULL", Nullable: true},
			},
		},
		{
			name: "invalid parameter name",
			parameters: map[string]Parameter{
				"Bad": {Type: StringParameter},
			},
			want: `invalid parameter name "Bad"`,
		},
		{
			name: "unsupported parameter type",
			parameters: map[string]Parameter{
				"value": {Type: "unknown"},
			},
			want: `parameter "value" has unsupported parameter type "unknown"`,
		},
		{
			name: "invalid boolean default",
			parameters: map[string]Parameter{
				"value": {Type: BooleanParameter, Default: "yes"},
			},
			want: `invalid default value for parameter "value": must be true or false`,
		},
		{
			name: "deterministic validation order",
			parameters: map[string]Parameter{
				"Z": {Type: StringParameter},
				"A": {Type: StringParameter},
			},
			want: `invalid parameter name "A"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateCheckParameters(Check{Parameters: test.parameters})

			if test.want == "" {
				require.NoError(t, err)
				return
			}

			require.EqualError(t, err, test.want)
		})
	}
}
