// Package interactive implements terminal browsing, prompts, and investigations.
package interactive

import (
	"context"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/manual"
	"github.com/bolajiwahab/pgcue/internal/output"
	"github.com/bolajiwahab/pgcue/internal/pgcue"
	"github.com/bolajiwahab/pgcue/internal/postgres"
)

// checkOutcome reports how an interactive check run ended.
type checkOutcome uint8

const (
	checkBack checkOutcome = iota
	checkQuit
	checkSucceeded
	checkFailed
)

// Runner drives terminal workflows using a caller-owned application session.
type Runner struct {
	Catalogue *catalogue.Catalogue
	In        io.ReadCloser
	Out, Err  io.Writer
	Style     output.Style // Style decorates Out.
	ErrStyle  output.Style // ErrStyle decorates Err.
	Session   *pgcue.Session
}

// Browse runs topic, check, and investigation navigation. It closes the prompt input
// on return; the caller remains responsible for closing Session.
func (runner *Runner) Browse(ctx context.Context) error {
	reader := newPromptReader(runner.In)
	defer reader.Close()
	for {
		topicID, choice, err := runner.selectTopic(ctx, reader)
		if err != nil || choice == actionQuit {
			return err
		}
		if topicID == "" {
			choice, err = runner.browseInvestigations(ctx, reader)
		} else {
			choice, err = runner.browseTopic(ctx, reader, topicID)
		}
		if err != nil || choice == actionQuit {
			return err
		}
	}
}

// selectTopic returns the chosen topic ID, or an empty ID for the investigations menu,
// which is offered first.
func (runner *Runner) selectTopic(ctx context.Context, reader *promptReader) (string, action, error) {
	topics := runner.Catalogue.Topics()
	labels := []string{"Investigations"}
	for _, topic := range topics {
		labels = append(labels, topic.Name)
	}
	index, choice, err := runner.choose(ctx, reader, "Select a topic (q to quit):", labels, false)
	if err != nil || choice != actionNone || index == 0 {
		return "", choice, err
	}
	return topics[index-1].ID, actionNone, nil
}

func (runner *Runner) browseInvestigations(ctx context.Context, reader *promptReader) (action, error) {
	investigations := runner.Catalogue.Investigations()
	labels := make([]string, len(investigations))
	for index, investigation := range investigations {
		labels[index] = investigation.Name
	}
	for {
		index, choice, err := runner.choose(
			ctx, reader, "Select an investigation (b to go back, q to quit):", labels, true,
		)
		if err != nil || choice != actionNone {
			return choice, err
		}
		choice, err = runner.runInvestigation(ctx, reader, investigations[index])
		if err != nil || choice == actionQuit {
			return choice, err
		}
	}
}

func (runner *Runner) browseTopic(ctx context.Context, reader *promptReader, topicID string) (action, error) {
	topic, _ := runner.Catalogue.GetTopic(topicID)
	if err := manual.Topic(runner.Out, runner.Style, runner.Catalogue, topic); err != nil {
		return actionNone, err
	}
	checks := runner.Catalogue.ChecksForTopic(topicID)
	labels := make([]string, len(checks))
	for i, check := range checks {
		labels[i] = check.Name
	}
	for {
		index, choice, err := runner.choose(ctx, reader, "Select a check (b to go back, q to quit):", labels, true)
		if err != nil || choice != actionNone {
			return choice, err
		}
		outcome, err := runner.runInteractiveCheck(ctx, reader, checks[index], nil)
		if err != nil {
			return actionNone, err
		}
		if outcome == checkQuit {
			return actionQuit, nil
		}
	}
}

// runInteractiveCheck prompts for parameters and runs a check. Execution failures are
// reported to the user and end in checkFailed; beforeExecute failures and output errors
// are returned.
func (runner *Runner) runInteractiveCheck(
	ctx context.Context,
	reader *promptReader,
	check catalogue.Check,
	beforeExecute func() error,
) (checkOutcome, error) {
	values, choice, err := runner.promptParameters(ctx, reader, check)
	switch {
	case err != nil, choice == actionQuit:
		return checkQuit, err
	case choice == actionBack:
		return checkBack, nil
	}
	// promptParameters accepts only values that resolve, and catalogue validation checks defaults.
	arguments, _ := catalogue.ResolveParameters(check.Parameters, values)
	if beforeExecute != nil {
		if err := beforeExecute(); err != nil {
			return checkFailed, err
		}
	}
	if err := runner.executeCheck(ctx, check, arguments); err != nil {
		if ctx.Err() != nil {
			return checkFailed, ctx.Err()
		}
		return checkFailed, runner.printError(err)
	}
	_, err = fmt.Fprintln(runner.Out)
	return checkSucceeded, err
}

// executeCheck connects before writing and stops before querying if the separator
// cannot be written. Guidance is added only after successful result consumption.
func (runner *Runner) executeCheck(ctx context.Context, check catalogue.Check, arguments pgx.NamedArgs) error {
	if err := runner.Session.Connect(ctx); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(runner.Out); err != nil {
		return err
	}
	renderer := inspectionRenderer{table: output.TableRenderer{Style: runner.Style}, guidance: check.Guidance.Inspect}
	return runner.Session.Execute(ctx, check, arguments, func(result postgres.Result) error {
		return renderer.Render(runner.Out, result.Columns, result.Rows)
	})
}

// inspectionRenderer adds interactive guidance only after successfully rendering rows.
type inspectionRenderer struct {
	table    output.TableRenderer
	guidance []string
}

func (renderer inspectionRenderer) Render(out io.Writer, columns []string, rows output.Rows) error {
	hasRows := false
	err := renderer.table.Render(out, columns, func(yield func([]any) error) error {
		return rows(func(row []any) error {
			hasRows = true
			return yield(row)
		})
	})
	if err != nil || !hasRows {
		return err
	}
	return manual.Bullets(out, renderer.table.Style.Bold("Inspect:"), renderer.guidance)
}
