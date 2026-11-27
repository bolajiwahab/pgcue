// Package manual renders pgcue's terminal manual: catalogue documentation and command usage.
package manual

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/output"
)

// Check writes a check's documentation.
func Check(w io.Writer, style output.Style, check catalogue.Check) error {
	var text strings.Builder
	fmt.Fprintf(
		&text,
		"%s\n\n%s\n\n%s\n  %s\n\n%s\n  %s\n\n%s\n  %s\n",
		style.Heading(check.Name),
		check.Description,
		style.Bold("Identifier:"),
		style.Cyan(check.ID),
		style.Bold("Run:"),
		CheckCommand(style, check),
		style.Bold("Result semantics:"),
		ResultSemantics(check.Result),
	)
	writeBullets(&text, style.Bold("Use when:"), check.Guidance.UseWhen)
	writeBullets(&text, style.Bold("Inspect:"), check.Guidance.Inspect)
	fmt.Fprintf(&text, "\n%s\n", style.Bold("Result columns:"))
	for _, name := range slices.Sorted(maps.Keys(check.Columns)) {
		fmt.Fprintf(&text, "  %s: %s\n", style.Green(name), check.Columns[name])
	}
	if len(check.Guidance.Related) > 0 {
		fmt.Fprintf(&text, "\n%s\n", style.Bold("Related checks:"))
		for _, related := range check.Guidance.Related {
			fmt.Fprintf(&text, "  - %s\n", style.Cyan(related))
		}
	}
	fmt.Fprintf(&text, "\n%s\n", style.Bold("Compatibility:"))
	for _, variant := range check.SQL {
		fmt.Fprintf(&text, "  - %s\n", variant.Compatibility())
	}
	writeParameters(&text, check.Parameters, style)
	_, err := io.WriteString(w, text.String())
	return err
}

func writeParameters(text *strings.Builder, parameters map[string]catalogue.Parameter, style output.Style) {
	fmt.Fprintf(text, "\n%s\n", style.Bold("Parameters:"))
	if len(parameters) == 0 {
		fmt.Fprintln(text, "  None")
		return
	}
	for _, name := range slices.Sorted(maps.Keys(parameters)) {
		parameter := parameters[name]
		required := "no"
		if parameter.Required {
			required = "yes"
		}
		fmt.Fprintf(
			text,
			"  %s\n    Type:        %s\n    Required:    %s\n",
			style.Green(name),
			parameter.Type,
			required,
		)
		if defaultValue, hasDefault := parameter.DefaultValue(); hasDefault {
			fmt.Fprintf(text, "    Default:     %s\n", FormatDefault(defaultValue))
		}
		if parameter.Minimum != nil {
			fmt.Fprintf(text, "    Minimum:     %d\n", *parameter.Minimum)
		}
		if parameter.Description != "" {
			fmt.Fprintf(text, "    Description: %s\n", parameter.Description)
		}
	}
}

// SQL writes each SQL variant of a check, preceded by its compatibility.
func SQL(w io.Writer, check catalogue.Check) error {
	var text strings.Builder
	for index, variant := range check.SQL {
		if index > 0 {
			fmt.Fprintln(&text)
		}
		fmt.Fprintf(&text, "-- %s\n%s", variant.Compatibility(), variant.Statement)
	}
	_, err := io.WriteString(w, text.String())
	return err
}

// Topic writes a topic's documentation with its investigations and checks.
func Topic(w io.Writer, style output.Style, catalog *catalogue.Catalogue, topic catalogue.Topic) error {
	var text strings.Builder
	fmt.Fprintf(
		&text,
		"\n%s\n\n%s\n\n%s\n",
		style.Heading(topic.Name),
		topic.Description,
		topic.Overview,
	)
	writeBullets(&text, style.Bold("Key concepts:"), topic.KeyConcepts)
	if investigations := catalog.InvestigationsForTopic(topic.ID); len(investigations) > 0 {
		fmt.Fprintf(&text, "\n%s\n", style.Bold("Investigations:"))
		for _, investigation := range investigations {
			fmt.Fprintf(
				&text,
				"  %s (%s)\n    Run: %s investigate %s\n\n",
				investigation.Name,
				style.Cyan(investigation.ID),
				Program,
				style.Cyan(investigation.ID),
			)
		}
	}
	fmt.Fprintf(&text, "\n%s\n", style.Bold("Related checks:"))
	for _, check := range catalog.ChecksForTopic(topic.ID) {
		fmt.Fprintf(
			&text,
			"  %s  %s\n    Describe: %s describe %s\n\n",
			style.Cyan(check.ID),
			check.Name,
			Program,
			style.Cyan(check.ID),
		)
	}
	_, err := io.WriteString(w, text.String())
	return err
}

// Investigation writes an investigation's steps. Step checks must exist in catalog.
func Investigation(
	w io.Writer,
	style output.Style,
	catalog *catalogue.Catalogue,
	investigation catalogue.Investigation,
) error {
	var text strings.Builder
	fmt.Fprintf(&text, "%s\n\n%s\n", style.Heading(investigation.Name), investigation.Description)
	for index, step := range investigation.Steps {
		check, _ := catalog.GetCheck(step.Check)
		fmt.Fprintf(
			&text,
			"\n%d. %s\n   Check: %s\n   %s\n   Run: %s\n",
			index+1,
			style.Cyan(check.Name),
			style.Cyan(check.ID),
			step.Reason,
			CheckCommand(style, check),
		)
	}
	fmt.Fprintf(
		&text,
		"\nRun the complete investigation:\n  %s investigate %s\n",
		Program,
		style.Cyan(investigation.ID),
	)
	_, err := io.WriteString(w, text.String())
	return err
}

// Bullets writes a heading followed by a bulleted list.
func Bullets(w io.Writer, heading string, values []string) error {
	var text strings.Builder
	writeBullets(&text, heading, values)
	_, err := io.WriteString(w, text.String())
	return err
}

func writeBullets(text *strings.Builder, heading string, values []string) {
	fmt.Fprintf(text, "\n%s\n", heading)
	for _, value := range values {
		fmt.Fprintf(text, "  - %s\n", value)
	}
}

// FormatDefault formats a parameter default for display.
func FormatDefault(value any) string {
	if value == nil {
		return "null"
	}
	return fmt.Sprint(value)
}

// ResultSemantics explains how to read a check's results.
func ResultSemantics(result catalogue.ResultSemantics) string { return resultSemantics[result] }

// resultSemantics describes each result semantics; catalogue validation accepts no others.
var resultSemantics = map[catalogue.ResultSemantics]string{
	catalogue.CurrentResult:    "Current state at execution time.",
	catalogue.CumulativeResult: "Cumulative statistics since PostgreSQL last reset the relevant counters.",
	catalogue.SnapshotResult:   "Point-in-time measurement at execution time.",
}
