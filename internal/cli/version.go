package cli

import (
	"context"
	"fmt"
	"runtime"
)

// version is the release version. make prepare-release updates it.
const version = "0.1.0"

func (app *App) printVersion(context.Context, []string, []string) error {
	_, err := fmt.Fprintf(
		app.Out,
		"%s %s, Compiler: %s %s\n",
		programName,
		version,
		runtime.Compiler,
		runtime.Version(),
	)
	return err
}
