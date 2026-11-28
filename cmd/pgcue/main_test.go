package main

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bolajiwahab/pgcue/internal/cli"
)

func TestSignalContext(t *testing.T) {
	signals := make(chan os.Signal, 2)
	exits := make(chan int, 1)
	ctx, stop := signalContext(context.Background(), signals, func(code int) { exits <- code })
	defer stop()

	signals <- os.Interrupt
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("first signal did not cancel the context")
	}
	var cancellation *signalCancellation
	require.ErrorAs(t, context.Cause(ctx), &cancellation)
	require.Equal(t, os.Interrupt.String(), cancellation.Error())

	signals <- syscall.SIGTERM
	select {
	case code := <-exits:
		require.Equal(t, cli.ExitTerminated, code)
	case <-time.After(time.Second):
		t.Fatal("second signal did not terminate immediately")
	}
}

func TestStoppedSignalContext(t *testing.T) {
	exits := make(chan int, 1)
	ctx, stop := signalContext(
		context.Background(),
		make(chan os.Signal),
		func(code int) { exits <- code },
	)
	stop()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	select {
	case code := <-exits:
		t.Fatalf("unexpected exit code %d", code)
	default:
	}
	require.Equal(t, cli.ExitInterrupted, signalExitCode(os.Interrupt))
}

func TestSignalContextStoppedAfterFirstSignal(t *testing.T) {
	signals := make(chan os.Signal, 1)
	exits := make(chan int, 1)
	ctx, stop := signalContext(context.Background(), signals, func(code int) { exits <- code })
	signals <- os.Interrupt
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("signal did not cancel context")
	}
	stop()
	select {
	case code := <-exits:
		t.Fatalf("unexpected exit code %d", code)
	default:
	}
}

func TestTerminatedRunExitsWithSignalCode(t *testing.T) {
	signals := make(chan os.Signal, 1)
	ctx, stop := signalContext(context.Background(), signals, func(int) {})
	defer stop()
	signals <- syscall.SIGTERM
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("signal did not cancel the context")
	}
	require.Equal(t, cli.ExitTerminated, exitCode(ctx, cli.ExitInterrupted))
	require.Equal(t, cli.ExitFailure, exitCode(ctx, cli.ExitFailure), "a run that failed on its own keeps its code")
}

func TestMainUsesRunExitCode(t *testing.T) {
	originalArgs, originalStderr, originalExit := os.Args, os.Stderr, exit
	read, write, err := os.Pipe()
	require.NoError(t, err)
	os.Args = []string{"pgcue", "unknown"}
	os.Stderr = write
	code := 0
	exit = func(value int) { code = value }
	t.Cleanup(func() {
		os.Args, os.Stderr, exit = originalArgs, originalStderr, originalExit
		_ = read.Close()
		_ = write.Close()
	})

	main()
	require.Equal(t, cli.ExitInvalidInput, code)
}
