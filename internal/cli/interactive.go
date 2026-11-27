package cli

import (
	"context"
	"fmt"
	"slices"

	"github.com/bolajiwahab/pgcue/internal/interactive"
	"github.com/bolajiwahab/pgcue/internal/pgcue"
)

func (app *App) runInvestigationCommand(ctx context.Context, args, options []string) error {
	if len(args) == 0 {
		if len(options) > 0 {
			return commandUsageError("investigate")
		}
		return app.listInvestigations()
	}
	dsn, timeout, err := parseConnectionOptions(options)
	if err != nil {
		return inputError(err)
	}
	investigation, err := app.lookupInvestigation(args[0])
	if err != nil {
		return err
	}
	return app.withSession(dsn, timeout, func(session *pgcue.Session) error {
		return app.workflowRunner(session).Investigate(ctx, investigation)
	})
}

// browse starts the interactive browser, which accepts connection options only.
func (app *App) browse(ctx context.Context, options []string) error {
	dsn, timeout, err := parseConnectionOptions(options)
	if err != nil {
		commands := commandRegistry()
		if slices.ContainsFunc(options, func(arg string) bool { _, ok := commands[arg]; return ok }) {
			err = fmt.Errorf("options must follow their command; use '%s help'", programName)
		}
		return inputError(err)
	}
	if err := app.loadCatalogue(); err != nil {
		return err
	}
	return app.withSession(dsn, timeout, func(session *pgcue.Session) error {
		return app.workflowRunner(session).Browse(ctx)
	})
}

func (app *App) workflowRunner(session *pgcue.Session) *interactive.Runner {
	return &interactive.Runner{
		Catalogue: app.catalogue, In: app.In, Out: app.Out, Err: app.Err, Style: app.Style,
		ErrStyle: app.ErrStyle, Session: session,
	}
}
