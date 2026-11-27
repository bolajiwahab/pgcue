package output

import (
	"fmt"
	"io"
)

// WriteError writes message to w as a labelled error.
func WriteError(w io.Writer, style Style, message string) error {
	_, err := fmt.Fprintf(w, "%s %s\n", style.Red("error:"), message)
	return err
}

// WriteWarning writes message to w as a labelled warning.
func WriteWarning(w io.Writer, style Style, message string) error {
	_, err := fmt.Fprintf(w, "%s %s\n", style.Yellow("warning:"), message)
	return err
}
