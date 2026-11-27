package interactive

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/pgcue"
	"github.com/bolajiwahab/pgcue/internal/testutil"
)

func TestInvestigationDescribePreservesProgressAcrossTopics(t *testing.T) {
	runner, out, stderr := testRunner(testutil.InvestigationCatalogue(t), "s\nd\nd\ns\n")
	first, _ := runner.Catalogue.GetCheck("demo.example")
	second := first
	second.ID = "storage.size"
	second.TopicID = "storage"
	second.Name = "Storage size"
	investigation, _ := runner.Catalogue.GetInvestigation("slow-requests")
	investigation.Topics = append(investigation.Topics, second.TopicID)
	investigation.Steps = append(
		investigation.Steps,
		catalogue.InvestigationStep{Check: second.ID, Reason: "Inspect storage growth."},
	)
	topics := append(
		runner.Catalogue.Topics(),
		catalogue.Topic{
			ID:          "storage",
			Name:        "Storage",
			Description: "Test topic.",
			Overview:    "Test overview.",
			KeyConcepts: []string{"Test concept."},
		},
	)
	c, err := catalogue.NewCatalogue(topics, []catalogue.Check{first, second}, []catalogue.Investigation{investigation})
	require.NoError(t, err)
	runner.Catalogue = c
	require.NoError(t, runner.Investigate(t.Context(), investigation))
	for _, want := range []string{"Step 1 of 2: Example\nCheck: demo.example", "Step 2 of 2: Storage size\nCheck: storage.size", "Skipped:   2", "Succeeded: 0", "Failed:    0"} {
		require.Contains(t, out.String(), want)
	}
	require.Equal(t, 3, strings.Count(out.String(), "Step 2 of 2:"))
	require.Equal(t, 0, stderr.Len())
}

func TestInvestigationDescribeDoesNotCollectParametersOrConnect(t *testing.T) {
	runner, out, _ := testRunner(testutil.ExampleCatalogue(t), "")
	check, _ := runner.Catalogue.GetCheck("demo.example")
	investigation := catalogue.Investigation{
		Steps: []catalogue.InvestigationStep{{Check: check.ID, Reason: "Inspect sessions."}},
	}
	reader := newPromptReader(io.NopCloser(strings.NewReader("d\nd\n")))
	defer reader.Close()
	executions := 0
	for range 2 {
		result, err := runner.runInvestigationStep(
			context.Background(),
			reader,
			investigation,
			0,
			func() error { executions++; return nil },
		)
		require.NoError(t, err)
		require.Equal(t, investigationStay, result.navigation)
		require.Equal(t, investigationPending, result.outcome)
		require.Equal(t, 0, executions)
	}
	require.NotContains(t, out.String(), "? Name")
	require.Equal(t, 2, strings.Count(out.String(), "Identifier:\n  demo.example"))
}

func TestInvestigationMenuReturnsToTopics(t *testing.T) {
	for _, input := range []string{"1\nb\nq\n", "1\n1\ns\nb\nq\n"} {
		runner, out, _ := testRunner(testutil.InvestigationCatalogue(t), input)
		require.NoError(t, runner.Browse(context.Background()))
		require.Equal(t, 2, strings.Count(out.String(), "Select a topic"))
	}
}

func TestStoppedInvestigationCountsRemainingStepsAsSkipped(t *testing.T) {
	outcomes := []investigationStepOutcome{
		investigationSucceeded,
		investigationSkipped,
		investigationFailed,
		investigationPending,
		investigationPending,
	}
	var out strings.Builder
	runner := Runner{Out: &out}
	require.EqualError(t, runner.finishInvestigation(outcomes, false), "investigation stopped with 1 failed check")
	for _, want := range []string{"Investigation stopped.", "Succeeded: 1", "Skipped:   3", "Failed:    1"} {
		require.Contains(t, out.String(), want)
	}
}

func TestInvestigationTargetEndsWithSingleNewline(t *testing.T) {
	var out bytes.Buffer
	runner := Runner{Out: &out}
	require.NoError(t, runner.writeInvestigationTarget(pgcue.Target{
		Address:            "db.example:5432/app",
		ServerMajorVersion: 17,
		ServerRole:         "replica",
		Database:           "app",
		User:               "reader",
		ReadOnly:           true,
	}))
	require.True(t, strings.HasSuffix(out.String(), "  Read-only:   yes\n"))
	require.Contains(t, out.String(), "Server role: replica\n")
}

func TestInteractiveInvestigations(t *testing.T) {
	runner, out, _ := testRunner(testutil.InvestigationCatalogue(t), "1\n1\nb\nb\nq\n")
	require.NoError(t, runner.Browse(context.Background()))
	for _, want := range []string{"Investigations", "Select an investigation", "Slow requests", "Step 1 of 1", "Check: demo.example", "Find slow database work."} {
		require.Contains(t, out.String(), want)
	}
}

func TestInvestigationQuit(t *testing.T) {
	for _, input := range []string{"1\nq\n", "1\n1\nq\n"} {
		runner, _, stderr := testRunner(testutil.InvestigationCatalogue(t), input)
		require.NoError(t, runner.Browse(t.Context()))
		require.Empty(t, stderr.String())
	}
	for _, input := range []string{"q\n", ""} {
		runner, _, stderr := testRunner(testutil.InvestigationCatalogue(t), input)
		investigation, _ := runner.Catalogue.GetInvestigation("slow-requests")
		require.NoError(t, runner.Investigate(t.Context(), investigation))
		require.Empty(t, stderr.String())
	}
}

func TestInvestigationConnectionError(t *testing.T) {
	runner, out, _ := testRunner(testutil.InvestigationCatalogue(t), "r\n")
	investigation, _ := runner.Catalogue.GetInvestigation("slow-requests")
	require.ErrorContains(t, runner.Investigate(t.Context(), investigation), "connect to PostgreSQL")
	require.NotContains(t, out.String(), "Inspect:")
}

func TestInvestigationRunNavigationWithoutExecution(t *testing.T) {
	runner, out, stderr := testRunner(testutil.InvestigationCatalogue(t), "b\ninvalid\ns\n")
	investigation, _ := runner.Catalogue.GetInvestigation("slow-requests")
	require.NoError(t, runner.Investigate(t.Context(), investigation))
	for _, want := range []string{"Step 1 of 1", "Run this check?", "Investigation complete.", "Succeeded: 0", "Skipped:   1", "Failed:    0"} {
		require.Contains(t, out.String(), want)
	}
	for _, want := range []string{"already at the first step", "choose r, d, s, b, or q"} {
		require.Contains(t, stderr.String(), want)
	}
}

func TestInvestigationBackRevisitsPreviousStep(t *testing.T) {
	runner, out, _ := testRunner(testutil.InvestigationCatalogue(t), "")
	check, _ := runner.Catalogue.GetCheck("demo.example")
	investigation, _ := runner.Catalogue.GetInvestigation("slow-requests")
	second := check
	second.ID = "demo.second"
	investigation.Steps = append(
		investigation.Steps,
		catalogue.InvestigationStep{Check: second.ID, Reason: "Inspect another signal."},
	)
	catalog, err := catalogue.NewCatalogue(
		runner.Catalogue.Topics(),
		[]catalogue.Check{check, second},
		[]catalogue.Investigation{investigation},
	)
	require.NoError(t, err)
	runner.Catalogue = catalog
	runner.In = io.NopCloser(strings.NewReader("s\nb\ns\ns\n"))
	require.NoError(t, runner.Investigate(t.Context(), investigation))
	require.Equal(t, 2, strings.Count(out.String(), "Step 1 of 2"))
}

func TestInvestigationWriteErrors(t *testing.T) {
	for _, test := range []struct {
		name, input, marker string
		stderr              bool
	}{
		{"step", "s\n", "Step 1 of 1", false},
		{"prompt", "s\n", "Run this check?", false},
		{"description", "d\n", "Identifier:", false},
		{"summary", "s\n", "Investigation complete.", false},
		{"invalid choice", "invalid\n", "", true},
		{"back from first step", "b\n", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner, _, _ := testRunner(testutil.InvestigationCatalogue(t), test.input)
			if test.stderr {
				runner.Err = testutil.FailingWriter{}
			} else {
				runner.Out = testutil.RejectingWriter{Needle: test.marker}
			}
			investigation, _ := runner.Catalogue.GetInvestigation("slow-requests")
			require.EqualError(t, runner.Investigate(t.Context(), investigation), "write failed")
		})
	}
}

func TestInvestigationCanceledBeforeExecution(t *testing.T) {
	runner, _, _ := testRunner(testutil.InvestigationCatalogue(t), "")
	// Input that never arrives, so the prompt can only end by cancellation.
	input, _ := io.Pipe()
	runner.In = input
	investigation, _ := runner.Catalogue.GetInvestigation("slow-requests")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, runner.Investigate(ctx, investigation), context.Canceled)
}

func TestInvestigationQuitDuringParameters(t *testing.T) {
	runner, _, _ := testRunner(testutil.ExampleCatalogue(t), "r\nq\n")
	check, ok := runner.Catalogue.GetCheck("demo.example")
	require.True(t, ok)
	// Investigation steps use checks without required parameters.
	check.Parameters = map[string]catalogue.Parameter{"name": {Type: catalogue.StringParameter, Default: "demo"}}
	investigation := catalogue.Investigation{
		Topics: []string{
			"demo",
		},
		ID:          "parameter-investigation",
		Name:        "Parameter investigation",
		Description: "Prompts.",
		Steps:       []catalogue.InvestigationStep{{Check: "demo.example", Reason: "Prompt."}},
	}
	catalog, err := catalogue.NewCatalogue(
		runner.Catalogue.Topics(),
		[]catalogue.Check{check},
		[]catalogue.Investigation{investigation},
	)
	require.NoError(t, err)
	runner.Catalogue = catalog
	require.NoError(t, runner.Investigate(t.Context(), investigation))
}

func TestInvestigationFailureStatus(t *testing.T) {
	for _, test := range []struct {
		outcomes []investigationStepOutcome
		complete bool
		want     string
	}{
		{[]investigationStepOutcome{investigationFailed}, true, "investigation completed with 1 failed check"},
		{[]investigationStepOutcome{investigationFailed, investigationFailed}, false, "investigation stopped with 2 failed checks"},
	} {
		var out bytes.Buffer
		runner := Runner{Out: &out}
		require.EqualError(t, runner.finishInvestigation(
			test.outcomes,
			test.complete,
		), test.want)
		require.Contains(t, out.String(), fmt.Sprintf("Failed:    %d", len(test.outcomes)))
	}
	require.NoError(t, (&Runner{Out: io.Discard}).finishInvestigation(
		[]investigationStepOutcome{investigationSkipped},
		true,
	))
	require.EqualError(t, (&Runner{Out: testutil.FailingWriter{}}).finishInvestigation(
		[]investigationStepOutcome{investigationFailed},
		true,
	), "write failed")
}

func TestInvestigationParameterPromptNavigation(t *testing.T) {
	example := testutil.ExampleCatalogue(t)
	topic, _ := example.GetTopic("demo")
	check, _ := example.GetCheck("demo.example")
	// Investigation steps use checks without required parameters.
	check.Parameters = map[string]catalogue.Parameter{"name": {Type: catalogue.StringParameter, Default: "demo"}}
	investigation := catalogue.Investigation{
		ID: "prompted", Name: "Prompted", Description: "Steps with parameters.", Topics: []string{"demo"},
		Steps: []catalogue.InvestigationStep{{Check: check.ID, Reason: "Prompt for a parameter."}},
	}
	catalog, err := catalogue.NewCatalogue(
		[]catalogue.Topic{topic}, []catalogue.Check{check}, []catalogue.Investigation{investigation},
	)
	require.NoError(t, err)
	// Back from the parameter prompt stays on the step; quit from it stops the investigation.
	runner, out, stderr := testRunner(catalog, "r\nb\nr\nq\n")
	require.NoError(t, runner.Investigate(t.Context(), investigation))
	require.Equal(t, 2, strings.Count(out.String(), "Step 1 of 1"))
	require.Contains(t, out.String(), "Investigation stopped.")
	require.Empty(t, stderr.String())
}

func TestInvestigationCountsFailedStepsPostgreSQL(t *testing.T) {
	runner, out, stderr := testRunner(testutil.InvestigationCatalogue(t), "r\nr\n")
	runner.Session = &pgcue.Session{DSN: testutil.PostgresDSN(t)}
	t.Cleanup(func() { require.NoError(t, runner.Session.Close()) })
	succeeds, _ := runner.Catalogue.GetCheck("demo.example")
	fails := succeeds
	fails.ID = "demo.fails"
	fails.SQL = []catalogue.SQLVariant{{Requires: succeeds.SQL[0].Requires, Statement: "SELECT 1/0"}}
	investigation, _ := runner.Catalogue.GetInvestigation("slow-requests")
	investigation.Steps = []catalogue.InvestigationStep{
		{Check: fails.ID, Reason: "Divides by zero."},
		{Check: succeeds.ID, Reason: "Succeeds."},
	}
	catalog, err := catalogue.NewCatalogue(
		runner.Catalogue.Topics(), []catalogue.Check{succeeds, fails}, []catalogue.Investigation{investigation},
	)
	require.NoError(t, err)
	runner.Catalogue = catalog

	require.EqualError(t, runner.Investigate(t.Context(), investigation), "investigation completed with 1 failed check")
	require.Contains(t, stderr.String(), "division by zero")
	// The connection is described before the first step only.
	require.Equal(t, 1, strings.Count(out.String(), "Connected"))
	for _, want := range []string{"Succeeded: 1", "Skipped:   0", "Failed:    1"} {
		require.Contains(t, out.String(), want)
	}
}
