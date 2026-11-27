package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type recordingCloser struct {
	deadline time.Time
	err      error
}

func (closer *recordingCloser) Close(ctx context.Context) error {
	closer.deadline, _ = ctx.Deadline()
	return closer.err
}

func TestCloseConnectionUsesBoundedContext(t *testing.T) {
	want := errors.New("close failed")
	closer := &recordingCloser{err: want}
	earliestDeadline := time.Now().Add(connectionCloseTimeout)
	cleanupDone := make(chan struct{})
	close(cleanupDone)

	require.ErrorIs(t, closeConnection(closer, cleanupDone), want)
	latestDeadline := time.Now().Add(connectionCloseTimeout)
	require.False(t, closer.deadline.IsZero())
	require.WithinRange(t, closer.deadline, earliestDeadline, latestDeadline)
}

func TestConnectionErrorIsWrappedAndRedacted(t *testing.T) {
	for _, test := range []struct {
		dsn, cause string
	}{
		{"host=localhost port=invalid password=secret", "invalid port"},
		{"postgresql://review_user:review_secret@%zz/db", "failed to parse as URL"},
		{`host=localhost port=invalid password='review_sec\'ret'`, "invalid port"},
	} {
		_, err := Connect(context.Background(), test.dsn)
		if err == nil || errors.Unwrap(err) == nil {
			t.Fatalf("expected wrapped connection error, got %v", err)
		}
		require.NotContains(t, err.Error(), "secret")
		require.NotContains(t, err.Error(), "review_sec")
		require.Contains(t, err.Error(), test.cause)
	}
}
