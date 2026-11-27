package interactive

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/testutil"
)

func TestInteractiveDefaultLabel(t *testing.T) {
	var out bytes.Buffer
	runner := Runner{Out: &out}
	reader := newPromptReader(io.NopCloser(strings.NewReader("\n")))
	defer reader.Close()
	check := catalogue.Check{
		Parameters: map[string]catalogue.Parameter{"count": {Type: catalogue.IntegerParameter, Default: 3}},
	}
	_, action, err := runner.promptParameters(t.Context(), reader, check)
	require.NoError(t, err)
	require.Equal(t, actionNone, action)
	require.Contains(t, out.String(), "Count [3]")
}

func TestNullDefaultLabel(t *testing.T) {
	var database catalogue.Parameter
	require.NoError(t, yaml.Unmarshal([]byte("type: string\ndefault: null\n"), &database))
	check := catalogue.Check{
		ID: "demo.defaults",
		Parameters: map[string]catalogue.Parameter{
			"database": database, "limit": {Type: catalogue.IntegerParameter, Default: 20},
			"minimum_calls": {Type: catalogue.IntegerParameter, Default: 2},
		},
	}

	var out bytes.Buffer
	runner := Runner{Out: &out}
	reader := newPromptReader(io.NopCloser(strings.NewReader("\n\n\n")))
	defer reader.Close()
	values, action, err := runner.promptParameters(t.Context(), reader, check)
	require.NoError(t, err)
	require.Equal(t, actionNone, action)
	require.Empty(t, values)
	for _, label := range []string{"Database [null]", "Limit [20]", "Minimum calls [2]"} {
		require.Contains(t, out.String(), label)
	}
}

func TestInteractiveParameterValidationRetries(t *testing.T) {
	check := catalogue.Check{Parameters: map[string]catalogue.Parameter{
		"count": {Type: catalogue.IntegerParameter, Required: true},
	}}
	var out, stderr bytes.Buffer
	runner := Runner{Out: &out, Err: &stderr}
	reader := newPromptReader(io.NopCloser(strings.NewReader("\nnot-a-number\n3\n")))
	defer reader.Close()
	values, action, err := runner.promptParameters(
		context.Background(),
		reader,
		check,
	)
	require.NoError(t, err)
	require.Equal(t, actionNone, action)
	require.Equal(t, "3", values["count"])
	for _, want := range []string{"Count is required", "Count: must be a whole number"} {
		require.Contains(t, stderr.String(), want)
	}
	require.Equal(t, 3, strings.Count(out.String(), "Count"))
}

func TestParameterValidationWriteErrors(t *testing.T) {
	for _, input := range []string{"\n", "not-a-number\n"} {
		t.Run(input, func(t *testing.T) {
			runner := Runner{Out: io.Discard, Err: testutil.FailingWriter{}}
			reader := newPromptReader(io.NopCloser(strings.NewReader(input)))
			defer reader.Close()
			check := catalogue.Check{
				Parameters: map[string]catalogue.Parameter{"count": {Type: catalogue.IntegerParameter, Required: true}},
			}
			_, _, err := runner.promptParameters(t.Context(), reader, check)
			require.EqualError(t, err, "write failed")
		})
	}
}

func TestPromptAcceptsFinalUnterminatedValue(t *testing.T) {
	var out bytes.Buffer
	reader := newPromptReader(io.NopCloser(strings.NewReader("value")))
	defer reader.Close()
	value, err := prompt(context.Background(), reader, &out, "> ")
	require.NoError(t, err)
	require.Equal(t, "value", value)
	require.Equal(t, "> ", out.String())
}

func TestPromptHonorsCancellation(t *testing.T) {
	input := &blockingReadCloser{started: make(chan struct{}), closed: make(chan struct{})}
	reader := newPromptReader(input)
	defer reader.Close()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := prompt(ctx, reader, io.Discard, "> ")
		result <- err
	}()
	select {
	case <-input.started:
	case <-time.After(time.Second):
		t.Fatal("prompt did not begin reading")
	}
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("prompt did not stop after cancellation")
	}
}

type blockingReadCloser struct {
	started chan struct{}
	closed  chan struct{}
}

func (reader *blockingReadCloser) Read([]byte) (int, error) {
	close(reader.started)
	<-reader.closed
	return 0, io.ErrClosedPipe
}

func (reader *blockingReadCloser) Close() error {
	close(reader.closed)
	return nil
}

func TestPromptKeepsReportingEndOfInput(t *testing.T) {
	reader := newPromptReader(io.NopCloser(strings.NewReader("")))
	defer reader.Close()
	for range 2 {
		_, err := reader.Read(t.Context())
		require.ErrorIs(t, err, io.EOF)
	}
}
