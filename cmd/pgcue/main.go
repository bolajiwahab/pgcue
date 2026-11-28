// Command pgcue runs PostgreSQL operational checks and guidance.
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/bolajiwahab/pgcue/internal/cli"
)

var exit = os.Exit

func main() {
	ctx, stop := processSignalContext(context.Background(), exit)
	defer stop()
	if code := exitCode(ctx, cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)); code != cli.ExitSuccess {
		exit(code)
	}
}

// exitCode reports a run that a signal interrupted with that signal's exit code.
func exitCode(ctx context.Context, code int) int {
	if code == cli.ExitInterrupted {
		if cancellation, ok := errors.AsType[*signalCancellation](context.Cause(ctx)); ok {
			return signalExitCode(cancellation.signal)
		}
	}
	return code
}

type signalCancellation struct{ signal os.Signal }

func (e *signalCancellation) Error() string { return e.signal.String() }

func signalExitCode(value os.Signal) int {
	if value == syscall.SIGTERM {
		return cli.ExitTerminated
	}
	return cli.ExitInterrupted
}

func processSignalContext(parent context.Context, terminate func(int)) (context.Context, func()) {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	ctx, stop := signalContext(parent, signals, terminate)
	return ctx, func() {
		signal.Stop(signals)
		stop()
	}
}

func signalContext(
	parent context.Context,
	signals <-chan os.Signal,
	terminate func(int),
) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	done := make(chan struct{})
	var once sync.Once
	stop := func() {
		once.Do(func() {
			close(done)
			cancel(context.Canceled)
		})
	}
	go func() {
		select {
		case value := <-signals:
			cancel(&signalCancellation{signal: value})
		case <-done:
			return
		}
		select {
		case value := <-signals:
			terminate(signalExitCode(value))
		case <-done:
		}
	}()
	return ctx, stop
}
