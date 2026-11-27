package interactive

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/output"
	"github.com/bolajiwahab/pgcue/internal/pgcue"
	"github.com/bolajiwahab/pgcue/internal/testutil"
)

func testRunner(catalog *catalogue.Catalogue, input string) (*Runner, *bytes.Buffer, *bytes.Buffer) {
	out, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &Runner{
		Catalogue: catalog,
		In:        io.NopCloser(strings.NewReader(input)),
		Out:       out,
		Err:       stderr,

		Session: &pgcue.Session{DSN: "postgres://localhost:invalid/pgcue"},
	}
	return runner, out, stderr
}

func TestInteractiveDiscoveryWithoutConnection(t *testing.T) {
	catalog, err := catalogue.LoadCatalogue()
	require.NoError(t, err)
	runner, out, _ := testRunner(catalog, "q\n")
	require.NoError(t, runner.Browse(context.Background()))
	require.Contains(t, out.String(), "Database activity")
}

func TestInteractiveErrorColorIsIndependent(t *testing.T) {
	catalog, err := catalogue.LoadCatalogue()
	require.NoError(t, err)
	runner, _, stderr := testRunner(catalog, "invalid\nq\n")
	runner.ErrStyle = output.Style{Enabled: true}
	require.NoError(t, runner.Browse(context.Background()))
	require.Contains(t, stderr.String(), "\x1b[1;31merror:")
}

func TestInteractiveReadErrors(t *testing.T) {
	for _, test := range []struct{ name, prefix string }{
		{"topic", ""}, {"check", "2\n"}, {"parameter", "2\n1\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner, _, _ := testRunner(testutil.ExampleCatalogue(t), "")
			runner.In = io.NopCloser(io.MultiReader(strings.NewReader(test.prefix), testutil.ErrorReader{}))
			require.EqualError(t, runner.Browse(t.Context()), "read failed")
		})
	}
}

func TestInteractiveWriteErrors(t *testing.T) {
	for _, test := range []struct {
		name, input string
		stderr      bool
	}{
		{"menu", "q\n", false},
		{"invalid choice", "invalid\n", true},
		{"required parameter", "2\n1\n\n", true},
		{"connection error", "2\n1\nvalue\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner, _, _ := testRunner(testutil.ExampleCatalogue(t), test.input)
			if test.stderr {
				runner.Err = testutil.FailingWriter{}
			} else {
				runner.Out = testutil.FailingWriter{}
			}
			require.EqualError(
				t,
				runner.Browse(t.Context()),
				"write failed",
			)
		})
	}
}

func TestInteractiveChoicesAndExecutionError(t *testing.T) {
	for _, input := range []string{"", "2\n", "invalid\nq\n", "2\ninvalid\nq\n", "2\n1\n\nq\n"} {
		runner, _, _ := testRunner(testutil.ExampleCatalogue(t), input)
		require.NoError(t, runner.Browse(context.Background()))
	}

	runner, _, stderr := testRunner(testutil.ExampleCatalogue(t), "2\n1\nvalue\nq\n")
	err := runner.Browse(context.Background())
	require.NoError(t, err)
	require.Contains(t, stderr.String(), "connect to PostgreSQL")
}

func TestInteractiveUsesTopicName(t *testing.T) {
	catalog, err := catalogue.NewCatalogue([]catalogue.Topic{
		{
			ID:          "activity",
			Name:        "Activity",
			Description: "Sessions.",
			Overview:    "Session overview.",
			KeyConcepts: []string{"Test concept."},
		},
	}, nil, nil)
	require.NoError(t, err)
	var out bytes.Buffer
	runner := Runner{Catalogue: catalog, Out: &out}
	reader := newPromptReader(io.NopCloser(strings.NewReader("2\n")))
	defer reader.Close()

	selected, choice, err := runner.selectTopic(
		context.Background(),
		reader,
	)
	require.NoError(t, err)
	require.Equal(t, actionNone, choice)
	require.Equal(t, "activity", selected)
	require.Contains(t, out.String(), "Activity")
}

func TestInteractiveTopicWithoutChecksIsOffline(t *testing.T) {
	topic := catalogue.Topic{
		ID:          "activity",
		Name:        "Activity",
		Description: "Sessions and transactions.",
		Overview:    "Each connection has a backend.",
		KeyConcepts: []string{"Idle transactions retain locks."},
	}
	catalog, err := catalogue.NewCatalogue([]catalogue.Topic{topic}, nil, nil)
	require.NoError(t, err)
	var out, stderr bytes.Buffer
	runner := Runner{Catalogue: catalog, In: io.NopCloser(strings.NewReader("2\nb\nq\n")), Out: &out, Err: &stderr}

	require.NoError(t, runner.Browse(context.Background()))
	for _, want := range []string{topic.Name, topic.Overview} {
		require.Contains(t, out.String(), want)
	}
	require.Equal(t, 0, stderr.Len())
}

func TestInteractiveCanReturnFromParameterAndQueryMenus(t *testing.T) {
	runner, out, _ := testRunner(testutil.ExampleCatalogue(t), "2\n1\nb\nb\nq\n")
	require.NoError(t, runner.Browse(context.Background()))
	require.Equal(t, 2, strings.Count(out.String(), "Select a check"))
	require.Equal(t, 2, strings.Count(out.String(), "Select a topic"))
}

func TestInteractiveQuitAndParameterEOF(t *testing.T) {
	for _, input := range []string{"2\nq\n", "2\n1\n"} {
		runner, _, _ := testRunner(testutil.ExampleCatalogue(t), input)
		require.NoError(t, runner.Browse(context.Background()))
	}
}

func TestInteractiveInspectionFollowsSuccessfulNonemptyResults(t *testing.T) {
	streamErr := errors.New("row stream failed")
	for _, test := range []struct {
		name        string
		rows        int
		guidance    []string
		streamErr   error
		wantInspect bool
	}{
		{name: "nonempty", rows: 1, guidance: []string{"Compare durations."}, wantInspect: true},
		{name: "empty", guidance: []string{"Compare durations."}},
		{name: "stream error", rows: 1, guidance: []string{"Compare durations."}, streamErr: streamErr},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out strings.Builder
			renderer := inspectionRenderer{guidance: test.guidance}
			err := renderer.Render(&out, []string{"duration"}, func(yield func([]any) error) error {
				for range test.rows {
					if err := yield([]any{42}); err != nil {
						return err
					}
				}
				return test.streamErr
			})
			require.ErrorIs(t, err, test.streamErr)
			require.Equal(t, test.wantInspect, strings.Contains(out.String(), "Inspect:"))
			if test.wantInspect && strings.Index(out.String(), "42") > strings.Index(out.String(), "Inspect:") {
				t.Fatalf("guidance precedes result: %q", out.String())
			}
		})
	}
}

func TestInterruptedCheckIsNotReportedAsFailed(t *testing.T) {
	runner, _, stderr := testRunner(testutil.InvestigationCatalogue(t), "")
	check, _ := runner.Catalogue.GetCheck("demo.example")
	reader := newPromptReader(io.NopCloser(strings.NewReader("")))
	defer reader.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := runner.runInteractiveCheck(ctx, reader, check, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, stderr.String())
}
