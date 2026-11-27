package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/output"
	"github.com/bolajiwahab/pgcue/internal/pgcue"
	"github.com/bolajiwahab/pgcue/internal/postgres"
)

// runStep is a check that run executes, with its resolved arguments.
type runStep struct {
	check     catalogue.Check
	arguments pgx.NamedArgs
}

// runTarget is what run executes each time: one check, or every step of an investigation.
type runTarget struct {
	investigation string
	steps         []runStep
}

func (app *App) runCommand(ctx context.Context, args, options []string) error {
	target, parsed, err := app.resolveRunTarget(args[0], options)
	if err != nil {
		return err
	}
	return app.withSession(parsed.dsn, parsed.timeout, func(session *pgcue.Session) error {
		if parsed.watch > 0 {
			return app.watch(ctx, session, target, parsed)
		}
		return app.runOnce(ctx, session, target, parsed.format, false)
	})
}

// resolveRunTarget runs an investigation's steps with their defaults, since investigations
// never use checks with required parameters.
func (app *App) resolveRunTarget(id string, options []string) (runTarget, runOptions, error) {
	if investigation, ok := app.catalogue.GetInvestigation(id); ok {
		parsed, _, err := parseOptions(options, runFlags, optionsHint)
		if err != nil {
			return runTarget{}, parsed, inputError(err)
		}
		target := runTarget{investigation: investigation.ID}
		for _, step := range investigation.Steps {
			check, _ := app.catalogue.GetCheck(step.Check)
			// Catalogue validation checks defaults, so they always resolve.
			arguments, _ := catalogue.ResolveParameters(check.Parameters, nil)
			target.steps = append(target.steps, runStep{check: check, arguments: arguments})
		}
		return target, parsed, nil
	}
	check, ok := app.catalogue.GetCheck(id)
	if !ok {
		return runTarget{}, runOptions{}, inputError(unknownValueError("check or investigation", id,
			slices.Concat(app.catalogue.CheckIDs(), app.catalogue.InvestigationIDs()),
			fmt.Sprintf("use '%s list' or '%s investigate' to see what can run", programName, programName)))
	}
	parsed, values, err := parseRunOptions(check, options)
	if err != nil {
		return runTarget{}, parsed, inputError(err)
	}
	arguments, err := catalogue.ResolveParameters(check.Parameters, values)
	if err != nil {
		return runTarget{}, parsed, inputError(flagError(err))
	}
	return runTarget{steps: []runStep{{check: check, arguments: arguments}}}, parsed, nil
}

// runOnce runs the target and writes its output. JSON output is one timestamped document per
// run, failures included, so a consumer sees every run. When separate is set, table output
// starts with a blank line to set it apart from an earlier run's.
func (app *App) runOnce(
	ctx context.Context,
	session *pgcue.Session,
	target runTarget,
	format string,
	separate bool,
) error {
	capturedAt := time.Now().Truncate(time.Microsecond)
	if target.investigation != "" {
		return app.runInvestigation(ctx, session, target, format, capturedAt, separate)
	}
	step := target.steps[0]
	if format == tableFormat {
		return app.renderStep(ctx, session, step, "", separate)
	}
	collected, err := collectStep(ctx, session, step)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	result := stepResult(step.check.ID, collected, err)
	return app.writeJSON(output.CheckDocument{CapturedAt: capturedAt, Result: result}, err)
}

// runInvestigation runs every step even when some fail, then reports the failures.
// A check this server cannot run is reported as unavailable, not as a failure.
func (app *App) runInvestigation(
	ctx context.Context,
	session *pgcue.Session,
	target runTarget,
	format string,
	capturedAt time.Time,
	separate bool,
) error {
	document := output.InvestigationDocument{CapturedAt: capturedAt, Investigation: target.investigation}
	if err := checkServer(ctx, session); err != nil {
		if format == jsonFormat && ctx.Err() == nil {
			document.Error = err.Error()
			return app.writeJSON(document, err)
		}
		return err
	}
	failed := 0
	for index, step := range target.steps {
		collected, err := app.runStep(ctx, session, step, format, separate || index > 0)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		result := stepResult(step.check.ID, collected, err)
		if result.Status == output.Failed {
			failed++
		}
		if err := app.reportStep(result, format); err != nil {
			return err
		}
		document.Results = append(document.Results, result)
	}
	var err error
	if failed > 0 {
		err = pgcue.InvestigationFailed(failed, false)
	}
	if format == jsonFormat {
		return app.writeJSON(document, err)
	}
	return err
}

// checkServer connects and confirms pgcue supports the server, since no step can run otherwise.
func checkServer(ctx context.Context, session *pgcue.Session) error {
	server, err := session.Target(ctx)
	if err != nil {
		return err
	}
	return catalogue.CheckServerVersion(server.ServerMajorVersion)
}

// stepResult records how a step ended. A check this server cannot run is unavailable,
// not failed.
func stepResult(check string, result output.Result, err error) output.Result {
	if err == nil {
		result.Check, result.Status = check, output.Succeeded
		return result
	}
	if _, ok := errors.AsType[*pgcue.UnavailableError](err); ok {
		return output.Result{Check: check, Status: output.Unavailable, Error: err.Error()}
	}
	return output.Result{Check: check, Status: output.Failed, Error: err.Error()}
}

// reportStep reports an unavailable or failed step of a table run as it happens; JSON output
// carries it in the document instead.
func (app *App) reportStep(result output.Result, format string) error {
	if format != tableFormat {
		return nil
	}
	switch result.Status {
	case output.Unavailable:
		return app.printWarning(result.Error)
	case output.Failed:
		return app.printError(result.Error)
	default:
		return nil
	}
}

// runStep runs one step of an investigation, rendering a table under the check's ID or
// collecting the result for JSON.
func (app *App) runStep(
	ctx context.Context,
	session *pgcue.Session,
	step runStep,
	format string,
	separate bool,
) (output.Result, error) {
	if format == jsonFormat {
		return collectStep(ctx, session, step)
	}
	heading := "Check: " + app.Style.Cyan(step.check.ID) + "\n\n"
	return output.Result{}, app.renderStep(ctx, session, step, heading, separate)
}

// renderStep writes heading, after a blank line when separate is set, then runs the step and
// renders its result as a table. The heading comes first, so the step's error or warning
// appears under it.
func (app *App) renderStep(
	ctx context.Context,
	session *pgcue.Session,
	step runStep,
	heading string,
	separate bool,
) error {
	if separate {
		heading = "\n" + heading
	}
	if _, err := io.WriteString(app.Out, heading); err != nil {
		return err
	}
	return session.Execute(ctx, step.check, step.arguments, func(result postgres.Result) error {
		return output.TableRenderer{Style: app.Style}.Render(app.Out, result.Columns, result.Rows)
	})
}

// collectStep runs the step and collects its result for JSON output.
func collectStep(ctx context.Context, session *pgcue.Session, step runStep) (output.Result, error) {
	var collected output.Result
	err := session.Execute(ctx, step.check, step.arguments, func(result postgres.Result) (err error) {
		collected, err = output.ReadResult(step.check.ID, result.Columns, result.Rows)
		return err
	})
	return collected, err
}

// writeJSON writes a run's document, then returns runErr, so a failed run still sets the
// exit status.
func (app *App) writeJSON(document any, runErr error) error {
	if err := output.WriteJSON(app.Out, document); err != nil {
		return err
	}
	return runErr
}

// watch reruns the target at parsed.watch intervals until interrupted or after parsed.count
// runs. A failed run is reported and watching continues; the failures set the exit status.
func (app *App) watch(ctx context.Context, session *pgcue.Session, target runTarget, parsed runOptions) error {
	failed := 0
	for run := 1; ; run++ {
		succeeded, err := app.watchOnce(ctx, session, target, parsed.format, run > 1)
		if err != nil {
			return err
		}
		if !succeeded {
			failed++
		}
		if run == parsed.count {
			break
		}
		if err := sleep(ctx, parsed.watch); err != nil {
			return err
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d %s failed", failed, parsed.count, plural(parsed.count, "run", "runs"))
	}
	return nil
}

// watchOnce runs the target once and reports a failed run rather than returning it. It
// returns an error only when interrupted.
func (app *App) watchOnce(
	ctx context.Context,
	session *pgcue.Session,
	target runTarget,
	format string,
	separate bool,
) (bool, error) {
	runErr := app.runOnce(ctx, session, target, format, separate)
	if runErr == nil {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	return false, app.printError(runErr.Error())
}

// sleep waits for interval unless ctx ends first.
func sleep(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func plural(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}
