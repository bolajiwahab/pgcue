package catalogue

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert/yaml"
	"github.com/stretchr/testify/require"
)

func TestResolveParameters(t *testing.T) {
	parameters := map[string]Parameter{
		"text_value": {Type: StringParameter, Required: true},
		"count":      {Type: IntegerParameter, Default: 3},
		"enabled":    {Type: BooleanParameter, Default: true},
		"delay":      {Type: DurationParameter, Default: "5m"},
	}

	got, err := ResolveParameters(
		parameters,
		map[string]string{"text_value": "hello", "enabled": "false"},
	)
	require.NoError(t, err)
	require.Equal(t, pgx.NamedArgs{
		"text_value": "hello",
		"count":      int64(3),
		"enabled":    false,
		"delay":      "300 seconds",
	}, got)
}

func TestParameterErrorDescribe(t *testing.T) {
	_, err := ResolveParameters(map[string]Parameter{"name": {Type: StringParameter, Required: true}}, nil)
	var parameterErr *ParameterError
	require.ErrorAs(t, err, &parameterErr)
	require.Equal(t, "name", parameterErr.Name)
	require.Equal(t, "missing required parameter --name", parameterErr.Describe("--name"))
}

func TestResolveParameterErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		parameters map[string]Parameter
		supplied   map[string]string
		want       string
	}{
		{
			"required",
			map[string]Parameter{"name": {Type: StringParameter, Required: true}},
			nil,
			`missing required parameter "name"`,
		},
		{
			"boolean",
			map[string]Parameter{"enabled": {Type: BooleanParameter}},
			map[string]string{"enabled": "yes"},
			`invalid value "yes" for "enabled": must be true or false`,
		},
		{
			"unknown",
			nil,
			map[string]string{"z": "1", "a": "2"},
			`unknown parameter "a"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ResolveParameters(test.parameters, test.supplied)
			require.EqualError(t, err, test.want)
		})
	}
}

func TestResolveZeroAndNullDefaults(t *testing.T) {
	got, err := ResolveParameters(map[string]Parameter{
		"text":     {Type: StringParameter, Default: "", defaultSet: true},
		"count":    {Type: IntegerParameter, Default: 0},
		"enabled":  {Type: BooleanParameter, Default: false},
		"database": {Type: StringParameter, defaultSet: true},
	}, nil)

	require.NoError(t, err)
	require.Equal(t, pgx.NamedArgs{
		"text":     "",
		"count":    int64(0),
		"enabled":  false,
		"database": nil,
	}, got)
}

func TestOptionalParameterCanBeOmitted(t *testing.T) {
	got, err := ResolveParameters(
		map[string]Parameter{"optional": {Type: StringParameter}},
		nil,
	)

	require.NoError(t, err)
	require.Empty(t, got)
}

func TestParseValues(t *testing.T) {
	for _, test := range []struct {
		name    string
		kind    ParameterType
		raw     string
		want    any
		wantErr string
	}{
		{"empty string", StringParameter, "", "", ""},
		{"negative integer", IntegerParameter, "-42", int64(-42), ""},
		{"ordinary integer rejects NULL", IntegerParameter, "NULL", nil, "must be a whole number"},
		{"integer maximum", IntegerParameter, "9223372036854775807", int64(9223372036854775807), ""},
		{
			"invalid integer", IntegerParameter, "1.5", nil,
			"must be a whole number",
		},
		{"false", BooleanParameter, "false", false, ""},
		{
			"case-sensitive boolean", BooleanParameter, "TRUE", nil,
			"must be true or false",
		},
		{"Go duration", DurationParameter, "1m30s", "90 seconds", ""},
		{
			"invalid duration", DurationParameter, "soon", nil,
			"expected a duration such as 30, 30s, or 2 hours",
		},
		{
			"unknown type", ParameterType("unknown"), "value", nil,
			`unsupported type "unknown"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseValue(Parameter{Type: test.kind}, test.raw)

			if test.wantErr != "" {
				require.EqualError(t, err, test.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestParsePostgreSQLIntervals(t *testing.T) {
	parameter := Parameter{Type: DurationParameter}

	for _, test := range []struct {
		value string
		valid bool
	}{
		{"1day", true},
		{"2 days", true},
		{"1week 2days", true},
		{"1 day 3 hours", true},
		{"1.5 months", true},
		{"-1year", true},
		{"1 lightyear", false},
		{"days", false},
		{"1.2.3 hours", false},
	} {
		t.Run(test.value, func(t *testing.T) {
			got, err := ParseValue(parameter, test.value)

			if !test.valid {
				require.EqualError(t, err, "expected a duration such as 30, 30s, or 2 hours")
				return
			}

			require.NoError(t, err)
			require.Equal(t, test.value, got)
		})
	}
}

func TestParameterUnmarshalYAMLInvalidFieldType(t *testing.T) {
	var parameter Parameter

	err := yaml.Unmarshal([]byte(`
type: string
required: [true]
`), &parameter)

	require.ErrorContains(t, err, "cannot unmarshal !!seq into bool")
}

func TestParameterMinimum(t *testing.T) {
	var parameter Parameter
	require.NoError(t, yaml.Unmarshal([]byte("type: integer\nminimum: 1\ndefault: 20\n"), &parameter))
	for _, value := range []string{"-1", "0", "1", "20"} {
		t.Run(value, func(t *testing.T) {
			got, err := ParseValue(parameter, value)
			if value == "-1" || value == "0" {
				require.EqualError(t, err, "must be at least 1")
				return
			}
			require.NoError(t, err)
			require.Positive(t, got)
		})
	}
	_, err := ResolveParameters(map[string]Parameter{"count": parameter}, map[string]string{"count": "0"})
	require.EqualError(t, err, `invalid value "0" for "count": must be at least 1`)
	check := testCheck()
	parameter.Type = StringParameter
	check.Parameters = map[string]Parameter{"count": parameter}
	require.EqualError(t, validateCheckParameters(check), `parameter "count": minimum is only supported for integers`)
	parameter.Type, parameter.Default = IntegerParameter, 0
	check.Parameters["count"] = parameter
	require.ErrorContains(t, validateCheckParameters(check), "invalid default value for parameter")
}

func TestResolveUnlimitedParameter(t *testing.T) {
	var parameter Parameter
	require.NoError(t, yaml.Unmarshal([]byte("type: integer\nminimum: 1\nnullable: true\ndefault: 20\n"), &parameter))
	for _, test := range []struct {
		value string
		want  any
	}{
		{"5", int64(5)},
		{"NULL", nil},
		{"nUlL", nil},
	} {
		t.Run(test.value, func(t *testing.T) {
			got, err := ResolveParameters(
				map[string]Parameter{"limit": parameter},
				map[string]string{"limit": test.value},
			)
			require.NoError(t, err)
			require.Equal(t, pgx.NamedArgs{"limit": test.want}, got)
		})
	}
}

func TestParseDuration(t *testing.T) {
	for _, test := range []struct {
		value string
		want  time.Duration
	}{
		{"30", 30 * time.Second},
		{"1.5", 1500 * time.Millisecond},
		{"1m30s", 90 * time.Second},
		{"2 hours", 2 * time.Hour},
		{"1 day 3 hours", 27 * time.Hour},
		{"1week", 7 * 24 * time.Hour},
		{"250 milliseconds", 250 * time.Millisecond},
	} {
		t.Run(test.value, func(t *testing.T) {
			got, err := ParseDuration(test.value)
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}

	// Months and years vary in length, so they are not durations.
	for _, value := range []string{"1 month", "1 year", "1 day 1 month", "soon"} {
		t.Run(value, func(t *testing.T) {
			_, err := ParseDuration(value)
			require.EqualError(t, err, "expected a duration such as 30, 30s, or 2 hours")
		})
	}
}
