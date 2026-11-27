package manual

import (
	"maps"
	"slices"
	"strings"

	"github.com/bolajiwahab/pgcue/internal/catalogue"
	"github.com/bolajiwahab/pgcue/internal/output"
)

// Program is the command name used in documented invocations.
const Program = "pgcue"

// FlagName converts a check parameter name to its CLI flag name, without dashes.
func FlagName(parameter string) string { return strings.ReplaceAll(parameter, "_", "-") }

// CheckCommand returns the command that runs a check with its required parameters.
func CheckCommand(style output.Style, check catalogue.Check) string {
	parts := []string{Program, "run", style.Cyan(check.ID)}
	for _, name := range slices.Sorted(maps.Keys(check.Parameters)) {
		if check.Parameters[name].Required {
			flag := FlagName(name)
			parts = append(parts, "--"+flag, "<"+flag+">")
		}
	}
	return strings.Join(parts, " ")
}
