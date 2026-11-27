package cli

import (
	"context"
	"fmt"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/manual"
)

func (app *App) lookupCheck(id string) (catalogue.Check, error) {
	check, ok := app.catalogue.GetCheck(id)
	if !ok {
		return check, inputError(unknownValueError("check", id, app.catalogue.CheckIDs(),
			fmt.Sprintf("use '%s list' to see available checks", programName)))
	}
	return check, nil
}

func (app *App) lookupTopic(id string) (catalogue.Topic, error) {
	topic, ok := app.catalogue.GetTopic(id)
	if !ok {
		return topic, inputError(unknownValueError("topic", id, app.catalogue.TopicIDs(),
			fmt.Sprintf("use '%s topic' to see available topics", programName)))
	}
	return topic, nil
}

func (app *App) lookupInvestigation(id string) (catalogue.Investigation, error) {
	investigation, ok := app.catalogue.GetInvestigation(id)
	if !ok {
		return investigation, inputError(unknownValueError("investigation", id, app.catalogue.InvestigationIDs(),
			fmt.Sprintf("use '%s investigate' to see available investigations", programName)))
	}
	return investigation, nil
}

// describe documents an investigation, topic, or check, in that order of precedence.
func (app *App) describe(_ context.Context, args, _ []string) error {
	id := args[0]
	if investigation, ok := app.catalogue.GetInvestigation(id); ok {
		return manual.Investigation(app.Out, app.Style, app.catalogue, investigation)
	}
	if topic, ok := app.catalogue.GetTopic(id); ok {
		return manual.Topic(app.Out, app.Style, app.catalogue, topic)
	}
	check, ok := app.catalogue.GetCheck(id)
	if !ok {
		return inputError(unknownValueError("check, topic, or investigation", id, documentationIDs(app), fmt.Sprintf(
			"use '%s list', '%s topic', or '%s investigate' to discover documentation",
			programName, programName, programName,
		)))
	}
	return manual.Check(app.Out, app.Style, check)
}

func (app *App) runTopicCommand(_ context.Context, args, _ []string) error {
	if len(args) == 0 {
		return app.listTopics()
	}
	topic, err := app.lookupTopic(args[0])
	if err != nil {
		return err
	}
	return manual.Topic(app.Out, app.Style, app.catalogue, topic)
}

func (app *App) printSQL(_ context.Context, args, _ []string) error {
	check, err := app.lookupCheck(args[0])
	if err != nil {
		return err
	}
	return manual.SQL(app.Out, check)
}
