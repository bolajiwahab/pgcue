package interactive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/manual"
	"github.com/bolajiwahab/pgcue/internal/pgcue"
)

type investigationStepOutcome uint8

const (
	investigationPending investigationStepOutcome = iota
	investigationSucceeded
	investigationSkipped
	investigationFailed
)

type investigationNavigation uint8

const (
	investigationStay investigationNavigation = iota
	investigationNext
	investigationBack
	investigationQuit
)

type investigationStepResult struct {
	navigation investigationNavigation
	outcome    investigationStepOutcome
}

// Investigate runs a resolved investigation. It closes the prompt input on return;
// the caller remains responsible for closing Session.
func (runner *Runner) Investigate(ctx context.Context, investigation catalogue.Investigation) error {
	reader := newPromptReader(runner.In)
	defer reader.Close()
	_, err := runner.runInvestigation(ctx, reader, investigation)
	return err
}

// runInvestigation returns actionQuit when the user stops before the final step.
func (runner *Runner) runInvestigation(
	ctx context.Context,
	reader *promptReader,
	investigation catalogue.Investigation,
) (action, error) {
	if _, err := fmt.Fprintf(runner.Out, "%s\n", runner.Style.Heading(investigation.Name)); err != nil {
		return actionNone, err
	}
	outcomes := make([]investigationStepOutcome, len(investigation.Steps))
	writeTarget := runner.investigationTargetWriter(ctx)

	for index := 0; index < len(investigation.Steps); {
		result, err := runner.runInvestigationStep(ctx, reader, investigation, index, writeTarget)
		if err != nil {
			return actionNone, err
		}
		switch result.navigation {
		case investigationNext:
			outcomes[index] = result.outcome
			index++
		case investigationBack:
			index--
			outcomes[index] = investigationPending
		case investigationQuit:
			return actionQuit, runner.finishInvestigation(outcomes, false)
		case investigationStay:
		}
	}
	return actionNone, runner.finishInvestigation(outcomes, true)
}

func (runner *Runner) investigationTargetWriter(ctx context.Context) func() error {
	shown := false
	return func() error {
		if shown {
			return nil
		}
		target, err := runner.Session.Target(ctx)
		if err != nil {
			return err
		}
		if err := runner.writeInvestigationTarget(target); err != nil {
			return err
		}
		shown = true
		return nil
	}
}

func (runner *Runner) runInvestigationStep(
	ctx context.Context,
	reader *promptReader,
	investigation catalogue.Investigation,
	index int,
	beforeExecute func() error,
) (investigationStepResult, error) {
	style := runner.Style
	step := investigation.Steps[index]
	check, _ := runner.Catalogue.GetCheck(step.Check)
	if _, err := fmt.Fprintf(
		runner.Out,
		"\n%s\nCheck: %s\n\n%s\n",
		style.Bold(fmt.Sprintf("Step %d of %d: %s", index+1, len(investigation.Steps), check.Name)),
		style.Cyan(check.ID),
		step.Reason,
	); err != nil {
		return investigationStepResult{}, err
	}
	choice, err := prompt(
		ctx,
		reader,
		runner.Out,
		style.Cyan("Run this check? [r]un, [d]escribe, [s]kip, [b]ack, [q]uit (r): "),
	)
	if errors.Is(err, io.EOF) || choice == "q" {
		return investigationStepResult{navigation: investigationQuit}, nil
	}
	if err != nil {
		return investigationStepResult{}, err
	}
	return runner.handleStepChoice(ctx, reader, check, choice, index, beforeExecute)
}

func (runner *Runner) handleStepChoice(
	ctx context.Context,
	reader *promptReader,
	check catalogue.Check,
	choice string,
	index int,
	beforeExecute func() error,
) (investigationStepResult, error) {
	stay := investigationStepResult{navigation: investigationStay}
	switch choice {
	case "", "r":
		outcome, err := runner.runInteractiveCheck(ctx, reader, check, beforeExecute)
		if err != nil {
			return investigationStepResult{}, err
		}
		return stepResultFor(outcome), nil
	case "d":
		if _, err := fmt.Fprintln(runner.Out); err != nil {
			return investigationStepResult{}, err
		}
		return stay, manual.Check(runner.Out, runner.Style, check)
	case "s":
		return investigationStepResult{navigation: investigationNext, outcome: investigationSkipped}, nil
	case "b":
		if index > 0 {
			return investigationStepResult{navigation: investigationBack}, nil
		}
		return stay, runner.printError(errors.New("already at the first step"))
	default:
		return stay, runner.printError(errors.New("choose r, d, s, b, or q"))
	}
}

func stepResultFor(outcome checkOutcome) investigationStepResult {
	switch outcome {
	case checkSucceeded:
		return investigationStepResult{navigation: investigationNext, outcome: investigationSucceeded}
	case checkFailed:
		return investigationStepResult{navigation: investigationNext, outcome: investigationFailed}
	case checkQuit:
		return investigationStepResult{navigation: investigationQuit}
	default:
		return investigationStepResult{navigation: investigationStay}
	}
}

func (runner *Runner) writeInvestigationTarget(target pgcue.Target) error {
	style := runner.Style
	readOnly := style.Yellow("no")
	if target.ReadOnly {
		readOnly = style.Green("yes")
	}
	var text strings.Builder
	fmt.Fprintf(
		&text,
		"\n%s\n  Target:      %s\n  PostgreSQL:  %s\n  Server role: %s\n"+
			"  Database:    %s\n  User:        %s\n  Read-only:   %s\n",
		style.Heading("Connected"),
		style.Cyan(target.Address),
		style.Cyan(fmt.Sprint(target.ServerMajorVersion)),
		style.Cyan(target.ServerRole),
		style.Cyan(target.Database),
		style.Cyan(target.User),
		readOnly,
	)
	_, err := io.WriteString(runner.Out, text.String())
	return err
}

func (runner *Runner) finishInvestigation(outcomes []investigationStepOutcome, complete bool) error {
	succeeded, skipped, failed := 0, 0, 0
	for _, outcome := range outcomes {
		switch outcome {
		case investigationSucceeded:
			succeeded++
		case investigationSkipped:
			skipped++
		case investigationFailed:
			failed++
		case investigationPending:
			if !complete {
				skipped++
			}
		}
	}
	summary := "stopped"
	if complete {
		summary = "complete"
	}
	if _, err := fmt.Fprintf(
		runner.Out,
		"\nInvestigation %s.\n\n  Succeeded: %d\n  Skipped:   %d\n  Failed:    %d\n",
		summary,
		succeeded,
		skipped,
		failed,
	); err != nil {
		return err
	}
	if failed == 0 {
		return nil
	}
	return pgcue.InvestigationFailed(failed, !complete)
}
