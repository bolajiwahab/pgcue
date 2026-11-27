package catalogue

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"go.yaml.in/yaml/v3"
)

// ParameterType identifies a check parameter type.
type ParameterType string

const (
	// StringParameter accepts text.
	StringParameter ParameterType = "string"
	// IntegerParameter accepts whole numbers.
	IntegerParameter ParameterType = "integer"
	// BooleanParameter accepts true or false.
	BooleanParameter ParameterType = "boolean"
	// DurationParameter accepts what ParseDuration reads and PostgreSQL interval expressions,
	// including calendar units such as 1 month.
	DurationParameter ParameterType = "duration"
)

// postgresqlInterval matches supported quantity-unit interval expressions,
// such as "2 hours", "1 month", and "1 day 3 hours".
var postgresqlInterval = regexp.MustCompile(
	`(?i)^(?:[+-]?(?:\d+(?:\.\d*)?|\.\d+)\s*(?:microseconds?|milliseconds?|seconds?|minutes?|hours?|days?|weeks?|months?|years?)\s*)+$`,
)

var (
	seconds      = regexp.MustCompile(`^[+-]?(?:\d+(?:\.\d*)?|\.\d+)$`)
	intervalTerm = regexp.MustCompile(`(?i)([+-]?(?:\d+(?:\.\d*)?|\.\d+))\s*([a-z]+)`)
	// fixedIntervalUnits are the interval units with a fixed length; calendar units are not.
	fixedIntervalUnits = map[string]time.Duration{
		"microsecond": time.Microsecond,
		"millisecond": time.Millisecond,
		"second":      time.Second,
		"minute":      time.Minute,
		"hour":        time.Hour,
		"day":         24 * time.Hour,
		"week":        7 * 24 * time.Hour,
	}
	errDuration = errors.New("expected a duration such as 30, 30s, or 2 hours")
)

// ParseDuration reads a number of seconds such as 30, a Go duration such as 1m30s, or an
// interval in fixed-length units such as 2 hours.
func ParseDuration(value string) (time.Duration, error) {
	if duration, err := time.ParseDuration(value); err == nil {
		return duration, nil
	}
	var total float64
	switch {
	case seconds.MatchString(value):
		total, _ = strconv.ParseFloat(value, 64)
		total *= float64(time.Second)
	case postgresqlInterval.MatchString(value):
		for _, term := range intervalTerm.FindAllStringSubmatch(value, -1) {
			length, fixed := fixedIntervalUnits[strings.TrimSuffix(strings.ToLower(term[2]), "s")]
			if !fixed {
				return 0, errDuration
			}
			quantity, _ := strconv.ParseFloat(term[1], 64)
			total += quantity * float64(length)
		}
	default:
		return 0, errDuration
	}
	return time.Duration(total), nil
}

// Parameter describes a check argument.
type Parameter struct {
	Type        ParameterType `yaml:"type"`
	Required    bool          `yaml:"required,omitempty"`
	Default     any           `yaml:"default,omitempty"`
	Minimum     *int64        `yaml:"minimum,omitempty"`
	Nullable    bool          `yaml:"nullable,omitempty"`
	Description string        `yaml:"description,omitempty"`
	defaultSet  bool
}

// UnmarshalYAML decodes parameter metadata and tracks explicit defaults.
func (parameter *Parameter) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return errors.New("parameter must be a YAML mapping")
	}

	defaultSet := false
	for index := 0; index < len(node.Content); index += 2 {
		switch node.Content[index].Value {
		case "type", "required", "description", "minimum", "nullable":
		case "default":
			defaultSet = true
		default:
			return fmt.Errorf("unknown parameter field %q", node.Content[index].Value)
		}
	}

	type plain Parameter
	var decoded plain
	if err := node.Decode(&decoded); err != nil {
		return err
	}

	*parameter = Parameter(decoded)
	parameter.defaultSet = defaultSet
	return nil
}

// DefaultValue returns the default and whether it was set.
func (parameter Parameter) DefaultValue() (any, bool) {
	return parameter.Default, parameter.defaultSet || parameter.Default != nil
}

var (
	errUnknownParameter = errors.New("unknown parameter")
	errMissingParameter = errors.New("missing required parameter")
)

// ParameterError reports an unknown, missing, or invalid parameter value.
type ParameterError struct {
	Name string
	// Value is the rejected value; it is empty for unknown and missing parameters.
	Value string
	Err   error
}

func (e *ParameterError) Error() string { return e.Describe(strconv.Quote(e.Name)) }

// Describe formats the error, referring to the parameter by label.
func (e *ParameterError) Describe(label string) string {
	if errors.Is(e.Err, errUnknownParameter) || errors.Is(e.Err, errMissingParameter) {
		return e.Err.Error() + " " + label
	}
	return fmt.Sprintf("invalid value %q for %s: %v", e.Value, label, e.Err)
}

// ResolveParameters validates and converts supplied parameters.
func ResolveParameters(
	parameters map[string]Parameter,
	supplied map[string]string,
) (pgx.NamedArgs, error) {
	for _, name := range sortedKeys(supplied) {
		if _, exists := parameters[name]; !exists {
			return nil, &ParameterError{Name: name, Err: errUnknownParameter}
		}
	}

	result := pgx.NamedArgs{}

	for _, name := range sortedKeys(parameters) {
		parameter := parameters[name]
		raw, suppliedValue := supplied[name]

		defaultValue, hasDefault := parameter.DefaultValue()
		switch {
		case suppliedValue:
			// Explicit values take precedence over defaults, including empty values.
		case hasDefault && defaultValue == nil:
			result[name] = nil
			continue
		case hasDefault:
			raw = fmt.Sprint(defaultValue)
		case parameter.Required:
			return nil, &ParameterError{Name: name, Err: errMissingParameter}
		default:
			continue
		}

		value, err := ParseValue(parameter, raw)
		if err != nil {
			return nil, &ParameterError{Name: name, Value: raw, Err: err}
		}

		result[name] = value
	}

	return result, nil
}

// ParseValue converts a parameter value.
func ParseValue(parameter Parameter, raw string) (any, error) {
	if parameter.Nullable && strings.EqualFold(raw, "NULL") {
		return nil, nil
	}
	switch parameter.Type {
	case StringParameter:
		return raw, nil
	case IntegerParameter:
		return parseInteger(parameter, raw)
	case BooleanParameter:
		if raw != "true" && raw != "false" {
			return nil, errors.New("must be true or false")
		}
		return raw == "true", nil
	case DurationParameter:
		// Interval expressions keep PostgreSQL's calendar semantics, such as for 1 month.
		if postgresqlInterval.MatchString(raw) {
			return raw, nil
		}
		duration, err := ParseDuration(raw)
		if err != nil {
			return nil, err
		}
		return strconv.FormatFloat(duration.Seconds(), 'f', -1, 64) + " seconds", nil
	default:
		return nil, fmt.Errorf("unsupported type %q", parameter.Type)
	}
}

func parseInteger(parameter Parameter, raw string) (any, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	switch {
	case err != nil:
		return nil, errors.New("must be a whole number")
	case parameter.Minimum != nil && value < *parameter.Minimum:
		return nil, fmt.Errorf("must be at least %d", *parameter.Minimum)
	}
	return value, nil
}
