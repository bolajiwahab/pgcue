// Package cli implements pgcue commands and terminal presentation.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/manual"
	"github.com/bolajiwahab/pgcue/internal/output"
	"github.com/bolajiwahab/pgcue/internal/pgcue"
)

const (
	programName     = manual.Program
	connectionUsage = "[" + dsnFlag + " <dsn>] [" + timeoutFlag + " <duration>]"

	// ExitSuccess indicates successful execution.
	ExitSuccess = 0
	// ExitFailure indicates an execution failure.
	ExitFailure = 1
	// ExitInvalidInput indicates invalid CLI input.
	ExitInvalidInput = 2
	// ExitInterrupted indicates an interrupt signal.
	ExitInterrupted = 130
	// ExitTerminated indicates a termination signal.
	ExitTerminated = 143
)

// commandHandler receives the command's leading positional arguments and the
// option arguments that follow them.
type commandHandler func(app *App, ctx context.Context, positionals, options []string) error

// commandOptions returns the options a command supports, given its arguments so far.
type commandOptions func(app *App, args []string) []string

type builtInCommand struct {
	usage       string
	description string
	// minArgs and maxArgs bound the positional arguments; maxArgs < 0 means unbounded.
	minArgs, maxArgs int
	// options is nil for commands that accept no options.
	options           commandOptions
	complete          commandCompleter
	handler           commandHandler
	requiresCatalogue bool
}

func commandRegistry() map[string]builtInCommand {
	commands := offlineCommands()
	maps.Copy(commands, databaseCommands())
	return commands
}

// offlineCommands work without a database connection.
func offlineCommands() map[string]builtInCommand {
	return map[string]builtInCommand{
		"completion": {
			usage:       "completion <bash|zsh|fish>",
			description: "Generate shell completion scripts.",
			minArgs:     1, maxArgs: 1,
			complete: completeShell,
			handler:  (*App).printCompletion,
		},
		"describe": {
			usage:       "describe <id>",
			description: "Show documentation for a check, topic, or investigation.",
			minArgs:     1, maxArgs: 1,
			complete: completeDescribe,
			handler:  (*App).describe, requiresCatalogue: true,
		},
		"find": {
			usage:       "find <terms...>",
			description: "Search checks, topics, and investigations.",
			minArgs:     1, maxArgs: -1,
			handler: (*App).find, requiresCatalogue: true,
		},
		"help": {
			usage:       "help",
			description: "Show this help message.",
			handler:     (*App).printHelp,
		},
		"sql": {
			usage:       "sql <check>",
			description: "Show the SQL variants defined for a check.",
			minArgs:     1, maxArgs: 1,
			complete: completeCheck,
			handler:  (*App).printSQL, requiresCatalogue: true,
		},
		"topic": {
			usage:       "topic [topic]",
			description: "List topics or display information about a topic.",
			maxArgs:     1,
			complete:    completeTopic,
			handler:     (*App).runTopicCommand, requiresCatalogue: true,
		},
		"version": {
			usage:       "version",
			description: "Show the installed pgcue version.",
			handler:     (*App).printVersion,
		},
	}
}

// databaseCommands connect to PostgreSQL, apart from listing without --available.
func databaseCommands() map[string]builtInCommand {
	return map[string]builtInCommand{
		"doctor": {
			usage:             "doctor " + connectionUsage,
			description:       "Inspect PostgreSQL connectivity, permissions, and extensions.",
			options:           fixedOptions(connectionFlags),
			handler:           (*App).doctor,
			requiresCatalogue: true,
		},
		"investigate": {
			usage:             "investigate [id] " + connectionUsage,
			description:       "List investigations or work through one step by step.",
			maxArgs:           1,
			options:           investigateOptions,
			complete:          completeInvestigation,
			handler:           (*App).runInvestigationCommand,
			requiresCatalogue: true,
		},
		"list": {
			usage:             "list [" + availableFlag + " " + connectionUsage + "]",
			description:       "List checks, optionally showing compatibility with the connected server.",
			options:           listOptions,
			handler:           (*App).runListCommand,
			requiresCatalogue: true,
		},
		"run": {
			usage: "run <check|investigation> " + connectionUsage + " [" + outputFlag + " <format>] [" +
				watchFlag + " <duration> [" + countFlag + " <n>]] [check parameters]",
			description:       "Run a check, or every step of an investigation, without prompts.",
			minArgs:           1,
			maxArgs:           1,
			options:           runCommandOptions,
			complete:          completeRunnable,
			handler:           (*App).runCommand,
			requiresCatalogue: true,
		},
	}
}

func fixedOptions(flags []string) commandOptions {
	return func(*App, []string) []string { return flags }
}

// ExitError carries a process exit code.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

// Run composes the CLI with terminal styling, presents errors, and returns its exit code.
func Run(ctx context.Context, args []string, in io.ReadCloser, out, stderr io.Writer) int {
	app := App{In: in, Out: out, Err: stderr, Style: output.Auto(out), ErrStyle: output.Auto(stderr)}

	if err := app.Run(ctx, args); err != nil {
		if errors.Is(err, context.Canceled) {
			return ExitInterrupted
		}

		_ = app.printError(err.Error())

		if exitError, ok := errors.AsType[*ExitError](err); ok {
			return exitError.Code
		}
		return ExitFailure
	}

	return ExitSuccess
}

// App runs the pgcue CLI.
type App struct {
	catalogue *catalogue.Catalogue
	In        io.ReadCloser
	Out       io.Writer
	Err       io.Writer
	Style     output.Style // Style decorates Out.
	ErrStyle  output.Style // ErrStyle decorates Err.
}

func (app *App) loadCatalogue() error {
	if app.catalogue != nil {
		return nil
	}

	catalog, err := catalogue.LoadCatalogue()
	if err != nil {
		return fmt.Errorf("load embedded catalogue: %w", err)
	}

	app.catalogue = catalog
	return nil
}

// withSession runs fn with a database session that is closed afterwards.
func (app *App) withSession(dsn string, timeout time.Duration, fn func(*pgcue.Session) error) (err error) {
	session := &pgcue.Session{DSN: dsn, Timeout: timeout, Warn: app.warn}
	defer func() { err = errors.Join(err, session.Close()) }()
	return fn(session)
}

func (app *App) printError(message string) error {
	return output.WriteError(app.Err, app.ErrStyle, message)
}

func (app *App) printWarning(message string) error {
	return output.WriteWarning(app.Err, app.ErrStyle, message)
}

// warn reports limited visibility before a check runs.
func (app *App) warn(message string) error {
	return app.printWarning(fmt.Sprintf("%s Use '%s doctor' to inspect permissions.", message, programName))
}

// Run executes pgcue, starting the interactive browser when no command is provided.
func (app *App) Run(ctx context.Context, args []string) error {
	if len(args) == 1 {
		switch args[0] {
		case helpFlag, shortHelpFlag:
			args = []string{"help"}
		case versionFlag:
			args = []string{"version"}
		}
	}

	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return app.browse(ctx, args)
	}

	return app.runBuiltIn(ctx, args[0], args[1:])
}

func (app *App) runBuiltIn(ctx context.Context, name string, args []string) error {
	if name == "__complete" {
		if err := app.loadCatalogue(); err != nil {
			return err
		}
		return app.printCompletionCandidates(args)
	}

	commands := commandRegistry()
	command, ok := commands[name]
	if !ok {
		return inputError(unknownValueError("command", name, slices.Sorted(maps.Keys(commands)), ""))
	}

	positionals, options := splitArguments(args)
	if err := command.validateArguments(name, positionals, options); err != nil {
		return inputError(err)
	}

	if command.requiresCatalogue {
		if err := app.loadCatalogue(); err != nil {
			return err
		}
	}

	return command.handler(app, ctx, positionals, options)
}

// splitArguments separates leading positional arguments from the options after them.
func splitArguments(args []string) (positionals, options []string) {
	index := slices.IndexFunc(args, func(arg string) bool { return strings.HasPrefix(arg, "-") })
	if index < 0 {
		return args, nil
	}
	return args[:index], args[index:]
}

func (command builtInCommand) validateArguments(name string, positionals, options []string) error {
	if command.options == nil && len(options) > 0 {
		return fmt.Errorf("%s does not accept option %q", name, options[0])
	}
	switch {
	case len(positionals) < command.minArgs:
		return commandUsage(name)
	case command.maxArgs < 0 || len(positionals) <= command.maxArgs:
		return nil
	case command.options != nil:
		return fmt.Errorf("unexpected argument %q", positionals[command.maxArgs])
	case command.maxArgs == 0:
		return fmt.Errorf("%s accepts no arguments", name)
	default:
		return commandUsage(name)
	}
}

func commandUsage(name string) error {
	return fmt.Errorf("usage: %s %s", programName, commandRegistry()[name].usage)
}

func commandUsageError(name string) error { return inputError(commandUsage(name)) }

func inputError(err error) error {
	return &ExitError{Code: ExitInvalidInput, Err: err}
}

func (app *App) printHelp(context.Context, []string, []string) error {
	style := app.Style
	var text strings.Builder

	fmt.Fprintf(
		&text,
		"%s - PostgreSQL operational checks and guidance, ready to run.\n\n",
		style.Heading(programName),
	)

	fmt.Fprintf(
		&text,
		"%s\n  %s %s\n      Start the interactive browser.\n\n",
		style.Heading("Usage:"),
		programName,
		connectionUsage,
	)

	commands := commandRegistry()
	for _, name := range slices.Sorted(maps.Keys(commands)) {
		command := commands[name]
		fmt.Fprintf(
			&text,
			"  %s %s\n      %s\n\n",
			programName,
			command.usage,
			command.description,
		)
	}

	fmt.Fprintf(&text, "%s\n  Options follow their command and check/investigation ID.\n", style.Heading("Options:"))
	for _, option := range [][2]string{
		{dsnFlag, "PostgreSQL connection string (otherwise pgx environment defaults)."},
		{timeoutFlag, "Limit for each PostgreSQL operation (default: 30s; 0 disables)."},
		{outputFlag, "table (default) or json (run only)."},
		{watchFlag, "Rerun at this interval until interrupted (run only)."},
		{countFlag, "Stop --watch after this many runs."},
		{availableFlag, "Mark the checks the connected server can run (list only)."},
		{helpFlag + ", " + shortHelpFlag, "Show help."},
		{versionFlag, "Show version."},
	} {
		fmt.Fprintf(&text, "  %-13s%s\n", option[0], option[1])
	}
	fmt.Fprintln(&text, "  Durations are seconds (30), Go durations (1m30s), or intervals (2 hours).")
	fmt.Fprintf(&text, "\n%s\n  %s    Default PostgreSQL operation timeout; overridden by %s\n",
		style.Heading("Environment:"), timeoutEnv, timeoutFlag,
	)

	_, err := io.WriteString(app.Out, text.String())
	return err
}
