package cli

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/manual"
)

const (
	timeoutEnv          = "pgcue_TIMEOUT"
	timeoutFlag         = "--timeout"
	outputFlag          = "--output"
	dsnFlag             = "--dsn"
	watchFlag           = "--watch"
	availableFlag       = "--available"
	helpFlag            = "--help"
	shortHelpFlag       = "-h"
	versionFlag         = "--version"
	countFlag           = "--count"
	defaultQueryTimeout = 30 * time.Second

	tableFormat = "table"
	jsonFormat  = "json"
)

var (
	connectionFlags = []string{dsnFlag, timeoutFlag}
	listFlags       = []string{availableFlag, dsnFlag, timeoutFlag}
	runFlags        = []string{countFlag, dsnFlag, outputFlag, timeoutFlag, watchFlag}
	outputFormats   = []string{jsonFormat, tableFormat}
	// switchFlags take no value.
	switchFlags = []string{availableFlag}
)

// optionsHint is the generic suggestion for unknown options.
var optionsHint = fmt.Sprintf("use '%s help' to see each command's options", programName)

type runOptions struct {
	dsn       string
	format    string
	timeout   time.Duration
	watch     time.Duration
	count     int
	available bool
}

// optionValue reads one named option, preserving empty values supplied with '='.
func optionValue(args []string, index *int) (string, string, error) {
	name, value, inline := strings.Cut(args[*index], "=")
	if !inline {
		if *index+1 >= len(args) || strings.HasPrefix(args[*index+1], "--") {
			return "", "", fmt.Errorf("%s requires a value", name)
		}
		(*index)++
		value = args[*index]
	}
	return name, value, nil
}

func queryTimeout(value string, explicit bool) (time.Duration, error) {
	if explicit {
		return parseTimeout(value, timeoutFlag)
	}

	if value := os.Getenv(timeoutEnv); value != "" {
		return parseTimeout(value, timeoutEnv)
	}

	return defaultQueryTimeout, nil
}

func parseConnectionOptions(args []string) (string, time.Duration, error) {
	options, _, err := parseOptions(args, connectionFlags, optionsHint)
	return options.dsn, options.timeout, err
}

func runCommandOptions(app *App, args []string) []string {
	if len(args) == 0 {
		return nil
	}
	if check, ok := app.catalogue.GetCheck(args[0]); ok {
		return runOptionFlags(check)
	}
	if _, ok := app.catalogue.GetInvestigation(args[0]); ok {
		return runFlags
	}
	return nil
}

func investigateOptions(app *App, args []string) []string {
	if len(args) > 0 {
		if _, ok := app.catalogue.GetInvestigation(args[0]); ok {
			return connectionFlags
		}
	}
	return nil
}

func runOptionFlags(check catalogue.Check) []string {
	flags := slices.Clone(runFlags)
	for name := range check.Parameters {
		flags = append(flags, "--"+manual.FlagName(name))
	}
	return flags
}

func parseRunOptions(check catalogue.Check, args []string) (runOptions, map[string]string, error) {
	return parseOptions(args, runOptionFlags(check),
		fmt.Sprintf("use '%s describe %s' to see its parameters", programName, check.ID))
}

// parseOptions handles shared flag syntax, duplicates, and operation options.
// Check parameters are returned as raw values for catalogue resolution.
// hint follows unknown option errors when no similar option exists.
func parseOptions(args, supported []string, hint string) (runOptions, map[string]string, error) {
	options := runOptions{format: tableFormat}
	parameters := make(map[string]string)
	seen := make(map[string]bool)
	var timeoutValue string

	for i := 0; i < len(args); i++ {
		name, value, err := readOption(args, &i, supported, hint)
		if err != nil {
			return options, nil, err
		}
		if seen[name] {
			return options, nil, fmt.Errorf("option %s specified more than once", name)
		}
		seen[name] = true

		switch name {
		case availableFlag:
			options.available = true
		case dsnFlag:
			options.dsn = value
		case outputFlag:
			options.format, err = parseFormat(value)
		case timeoutFlag:
			timeoutValue = value
		case watchFlag:
			options.watch, err = parseWatch(value)
		case countFlag:
			options.count, err = parseCount(value)
		default:
			parameters[parameterName(name)] = value
		}
		if err != nil {
			return options, nil, err
		}
	}
	if seen[countFlag] && !seen[watchFlag] {
		return options, nil, fmt.Errorf("%s requires %s", countFlag, watchFlag)
	}

	timeout, err := queryTimeout(timeoutValue, seen[timeoutFlag])
	if err != nil {
		return options, nil, err
	}

	options.timeout = timeout
	return options, parameters, nil
}

// readOption reads the supported option at args[*index], with its value unless it is a switch.
func readOption(args []string, index *int, supported []string, hint string) (string, string, error) {
	name, _, inline := strings.Cut(args[*index], "=")
	switch {
	case !strings.HasPrefix(name, "--"):
		return "", "", fmt.Errorf("unexpected argument %q", name)
	case !slices.Contains(supported, name):
		return "", "", unknownValueError("option", name, supported, hint)
	case slices.Contains(switchFlags, name) && inline:
		return "", "", fmt.Errorf("%s does not take a value", name)
	case slices.Contains(switchFlags, name):
		return name, "", nil
	}
	return optionValue(args, index)
}

func parseFormat(value string) (string, error) {
	if !slices.Contains(outputFormats, value) {
		return "", fmt.Errorf("invalid output format %q (expected table or json)", value)
	}
	return value, nil
}

func parseCount(value string) (int, error) {
	count, err := strconv.Atoi(value)
	if err != nil || count < 1 {
		return 0, fmt.Errorf("%s must be a positive whole number", countFlag)
	}
	return count, nil
}

func parseWatch(value string) (time.Duration, error) {
	interval, err := catalogue.ParseDuration(value)
	if err != nil || interval <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", watchFlag)
	}
	return interval, nil
}

func parseTimeout(value, name string) (time.Duration, error) {
	if value == "0" {
		return 0, nil
	}

	timeout, err := catalogue.ParseDuration(value)
	if err != nil || timeout <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration or 0", name)
	}
	return timeout, nil
}

// flagError refers to the parameter in a catalogue.ResolveParameters error, always a
// *catalogue.ParameterError, by its CLI flag.
func flagError(err error) error {
	parameterErr, _ := errors.AsType[*catalogue.ParameterError](err)
	return errors.New(parameterErr.Describe("--" + manual.FlagName(parameterErr.Name)))
}

// parameterName converts a check parameter flag back to the parameter's name; see manual.FlagName.
func parameterName(flag string) string {
	return strings.ReplaceAll(strings.TrimPrefix(flag, "--"), "-", "_")
}
