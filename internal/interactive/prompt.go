package interactive

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/manual"
	"github.com/bolajiwahab/pgcue/internal/output"
)

// promptReader reads input lines in the background, so a prompt returns as soon as its
// context is cancelled; closing a terminal does not interrupt a blocked read.
type promptReader struct {
	lines  chan promptLine
	done   chan struct{}
	closer io.Closer
}

type promptLine struct {
	value string
	err   error
}

func newPromptReader(input io.ReadCloser) *promptReader {
	reader := &promptReader{lines: make(chan promptLine), done: make(chan struct{}), closer: input}
	go reader.readLines(bufio.NewReader(input))
	return reader
}

func (r *promptReader) readLines(input *bufio.Reader) {
	defer close(r.lines)
	for {
		value, err := input.ReadString('\n')
		if errors.Is(err, io.EOF) && value != "" {
			err = nil
		}
		select {
		case r.lines <- promptLine{value: strings.TrimSpace(value), err: err}:
		case <-r.done:
			return
		}
		if err != nil {
			return
		}
	}
}

func (r *promptReader) Read(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case line, ok := <-r.lines:
		if !ok {
			return "", io.EOF
		}
		return line.value, line.err
	}
}

// Close stops the background reader and closes the input.
func (r *promptReader) Close() {
	close(r.done)
	_ = r.closer.Close()
}

// action is a navigation request entered at a prompt instead of a value.
type action uint8

const (
	actionNone action = iota
	actionBack
	actionQuit
)

// parseAction recognizes b (when allowed) and q. End of input quits.
func parseAction(value string, allowBack bool) action {
	switch {
	case value == "q":
		return actionQuit
	case value == "b" && allowBack:
		return actionBack
	default:
		return actionNone
	}
}

func (runner *Runner) choose(
	ctx context.Context,
	reader *promptReader,
	heading string,
	labels []string,
	allowBack bool,
) (int, action, error) {
	for {
		if err := writeMenu(runner.Out, runner.Style.Bold(heading), labels); err != nil {
			return 0, actionNone, err
		}
		value, err := prompt(ctx, reader, runner.Out, runner.Style.Cyan("> "))
		if errors.Is(err, io.EOF) {
			return 0, actionQuit, nil
		}
		if err != nil {
			return 0, actionNone, err
		}
		if choice := parseAction(value, allowBack); choice != actionNone {
			return 0, choice, nil
		}
		index, err := selection(value, len(labels))
		if err == nil {
			return index, actionNone, nil
		}
		if err := runner.printError(err); err != nil {
			return 0, actionNone, err
		}
	}
}

func (runner *Runner) promptParameters(
	ctx context.Context,
	reader *promptReader,
	check catalogue.Check,
) (map[string]string, action, error) {
	values := map[string]string{}
	for _, name := range slices.Sorted(maps.Keys(check.Parameters)) {
		value, choice, err := runner.promptParameter(ctx, reader, name, check.Parameters[name])
		if err != nil || choice != actionNone {
			return nil, choice, err
		}
		if value != "" {
			values[name] = value
		}
	}
	return values, actionNone, nil
}

func (runner *Runner) promptParameter(
	ctx context.Context,
	reader *promptReader,
	name string,
	parameter catalogue.Parameter,
) (string, action, error) {
	displayName := title(strings.ReplaceAll(name, "_", " "))
	label := displayName
	if defaultValue, hasDefault := parameter.DefaultValue(); hasDefault {
		label += " [" + manual.FormatDefault(defaultValue) + "]"
	}
	for {
		value, err := prompt(ctx, reader, runner.Out, runner.Style.Cyan("? ")+label+" (b to go back, q to quit): ")
		if errors.Is(err, io.EOF) {
			return "", actionQuit, nil
		}
		if err != nil {
			return "", actionNone, err
		}
		if choice := parseAction(value, true); choice != actionNone {
			return "", choice, nil
		}
		err = validateParameterInput(parameter, displayName, value)
		if err == nil {
			return value, actionNone, nil
		}
		if err := runner.printError(err); err != nil {
			return "", actionNone, err
		}
	}
}

// validateParameterInput accepts an empty value only for optional parameters,
// which then use their defaults.
func validateParameterInput(parameter catalogue.Parameter, displayName, value string) error {
	if value == "" {
		if parameter.Required {
			return fmt.Errorf("%s is required", displayName)
		}
		return nil
	}
	if _, err := catalogue.ParseValue(parameter, value); err != nil {
		return fmt.Errorf("%s: %w", displayName, err)
	}
	return nil
}

func writeMenu(w io.Writer, heading string, choices []string) error {
	var text strings.Builder
	fmt.Fprintln(&text, heading)
	for i, choice := range choices {
		fmt.Fprintf(&text, "  %d) %s\n", i+1, choice)
	}
	_, err := io.WriteString(w, text.String())
	return err
}

func (runner *Runner) printError(err error) error {
	return output.WriteError(runner.Err, runner.ErrStyle, err.Error())
}

func prompt(ctx context.Context, reader *promptReader, w io.Writer, label string) (string, error) {
	if _, err := fmt.Fprint(w, label); err != nil {
		return "", err
	}
	return reader.Read(ctx)
}

func selection(value string, count int) (int, error) {
	number, err := strconv.Atoi(value)
	if err != nil || number < 1 || number > count {
		return 0, fmt.Errorf("choose a number from 1 to %d", count)
	}
	return number - 1, nil
}

// title capitalizes a parameter's display name, which validation starts with a lowercase letter.
func title(value string) string {
	return strings.ToUpper(value[:1]) + value[1:]
}
