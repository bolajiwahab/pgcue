package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/output"
	"github.com/bolajiwahab/pgcue/internal/pgcue"
)

func (app *App) find(_ context.Context, terms, _ []string) error {
	search := strings.Join(terms, " ")
	result := app.catalogue.Search(search)
	style := app.Style
	var text strings.Builder
	if len(result.Topics) > 0 {
		writeSearchHeading(&text, style.Heading("Topics"))
		for _, topic := range result.Topics {
			fmt.Fprintf(
				&text,
				"  %s\n    %s\n    Read: %s describe %s\n",
				style.Cyan(topic.ID),
				topic.Description,
				programName,
				style.Cyan(topic.ID),
			)
		}
	}
	if len(result.Investigations) > 0 {
		writeSearchHeading(&text, style.Heading("Investigations"))
		for _, investigation := range result.Investigations {
			fmt.Fprintf(
				&text,
				"  %s\n    %s\n    Run: %s investigate %s\n",
				style.Cyan(investigation.ID),
				investigation.Description,
				programName,
				style.Cyan(investigation.ID),
			)
		}
	}
	if len(result.Checks) > 0 {
		writeSearchHeading(&text, style.Heading("Checks"))
		for _, check := range result.Checks {
			fmt.Fprintf(&text, "  %s %s\n", style.Cyan(fmt.Sprintf("%-32s", check.ID)), check.Name)
		}
	}
	if text.Len() == 0 {
		fmt.Fprintf(&text, "No topics, investigations, or checks found for %q.\n", search)
	}
	_, err := io.WriteString(app.Out, text.String())
	return err
}

func writeSearchHeading(text *strings.Builder, heading string) {
	if text.Len() > 0 {
		fmt.Fprintln(text)
	}
	fmt.Fprintln(text, heading)
}

type indexEntry struct{ id, name, description string }

// writeIndex lists entries with aligned IDs, followed by footer.
func (app *App) writeIndex(heading string, entries []indexEntry, footer string) error {
	style := app.Style
	var text strings.Builder
	fmt.Fprintln(&text, style.Heading(heading))
	width := 0
	for _, entry := range entries {
		width = max(width, len(entry.id))
	}
	for _, entry := range entries {
		fmt.Fprintf(
			&text,
			"\n  %s  %s\n  %s  %s\n",
			style.Cyan(fmt.Sprintf("%-*s", width, entry.id)),
			entry.name,
			strings.Repeat(" ", width),
			entry.description,
		)
	}
	fmt.Fprintf(&text, "\n%s\n", footer)
	_, err := io.WriteString(app.Out, text.String())
	return err
}

func (app *App) listTopics() error {
	var entries []indexEntry
	for _, topic := range app.catalogue.Topics() {
		entries = append(entries, indexEntry{topic.ID, topic.Name, topic.Description})
	}
	return app.writeIndex(
		"PostgreSQL topics",
		entries,
		fmt.Sprintf("Read a topic with '%s describe <topic>'.", programName),
	)
}

func (app *App) listInvestigations() error {
	var entries []indexEntry
	for _, investigation := range app.catalogue.Investigations() {
		entries = append(entries, indexEntry{investigation.ID, investigation.Name, investigation.Description})
	}
	return app.writeIndex("Investigations", entries, fmt.Sprintf(
		"Inspect an investigation with '%s describe <investigation>' or run it with '%s investigate <investigation>'.",
		programName,
		programName,
	))
}

func listOptions(_ *App, args []string) []string {
	if slices.Contains(args, availableFlag) {
		return connectionFlags
	}
	return []string{availableFlag}
}

func (app *App) runListCommand(ctx context.Context, _, options []string) error {
	if len(options) == 0 {
		return app.writeCheckList(nil)
	}
	parsed, _, err := parseOptions(options, listFlags, optionsHint)
	if err != nil {
		return inputError(err)
	}
	if !parsed.available {
		return inputError(fmt.Errorf("list connection options require %s", availableFlag))
	}
	return app.withSession(parsed.dsn, parsed.timeout, func(session *pgcue.Session) error {
		availability, err := session.Availability(ctx, app.catalogue)
		if err != nil {
			return err
		}
		return app.writeCheckList(availability)
	})
}

func (app *App) writeCheckList(availability map[string]bool) error {
	style := app.Style
	var text strings.Builder
	if availability != nil {
		fmt.Fprintf(&text, "%s available   %s unavailable\n\n", style.Green("✓"), style.Yellow("–"))
	}
	for index, topic := range app.catalogue.Topics() {
		if index > 0 {
			fmt.Fprintln(&text)
		}
		fmt.Fprintf(&text, "%s\n%s\n\n", style.Heading(topic.Name), topic.Description)
		writeCheckRows(&text, app.catalogue.ChecksForTopic(topic.ID), availability, style)
	}
	_, err := io.WriteString(app.Out, text.String())
	return err
}

func writeCheckRows(text *strings.Builder, checks []catalogue.Check, availability map[string]bool, style output.Style) {
	width := 0
	for _, check := range checks {
		width = max(width, len(check.ID))
	}
	for _, check := range checks {
		status := ""
		if available, checked := availability[check.ID]; checked {
			if available {
				status = style.Green("✓") + " "
			} else {
				status = style.Yellow("–") + " "
			}
		}
		fmt.Fprintf(text, "  %s%s  %s\n", status, style.Cyan(fmt.Sprintf("%-*s", width, check.ID)), check.Name)
	}
}

func documentationIDs(app *App) []string {
	return slices.Concat(app.catalogue.CheckIDs(), app.catalogue.TopicIDs(), app.catalogue.InvestigationIDs())
}
