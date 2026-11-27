package output

import (
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

const escape = "\x1b["

// Style applies terminal colors.
type Style struct {
	Enabled bool
}

// Auto enables color for an interactive terminal.
func Auto(w io.Writer) Style {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		return Style{}
	}
	if strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return Style{}
	}

	file, ok := w.(*os.File)
	if !ok {
		return Style{}
	}

	return Style{Enabled: term.IsTerminal(int(file.Fd()))}
}

// Bold applies bold styling.
func (s Style) Bold(value string) string { return s.wrap("1", value) }

// Dim applies dim styling.
func (s Style) Dim(value string) string { return s.wrap("2", value) }

// Red applies red styling.
func (s Style) Red(value string) string { return s.wrap("1;31", value) }

// Green applies green styling.
func (s Style) Green(value string) string { return s.wrap("32", value) }

// Cyan applies cyan styling.
func (s Style) Cyan(value string) string { return s.wrap("36", value) }

// Heading applies heading styling.
func (s Style) Heading(value string) string { return s.wrap("1;36", value) }

// Yellow applies yellow styling.
func (s Style) Yellow(value string) string { return s.wrap("33", value) }

func (s Style) wrap(code, value string) string {
	if !s.Enabled {
		return value
	}
	return escape + code + "m" + value + escape + "0m"
}
