package catalogue

import "strings"

// Guidance explains when and how to use a check.
type Guidance struct {
	UseWhen []string `yaml:"use_when,omitempty"`
	Inspect []string `yaml:"inspect,omitempty"`
	Related []string `yaml:"related,omitempty"`
}

// ResultSemantics describes how to interpret check results.
type ResultSemantics string

const (
	// CurrentResult describes current state.
	CurrentResult ResultSemantics = "current"
	// CumulativeResult describes counters since reset.
	CumulativeResult ResultSemantics = "cumulative"
	// SnapshotResult describes a point-in-time sample.
	SnapshotResult ResultSemantics = "snapshot"
)

// Valid reports whether the result semantics are recognized.
func (semantics ResultSemantics) Valid() bool {
	switch semantics {
	case CurrentResult, CumulativeResult, SnapshotResult:
		return true
	default:
		return false
	}
}

// Check describes an executable catalogue check.
type Check struct {
	ID          string `yaml:"-"`
	TopicID     string `yaml:"-"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	// RequiresMonitor marks checks whose output may be incomplete without monitoring privileges.
	RequiresMonitor bool                 `yaml:"requires_monitor,omitempty"`
	Guidance        Guidance             `yaml:"guidance,omitempty"`
	Result          ResultSemantics      `yaml:"result,omitempty"`
	Columns         map[string]string    `yaml:"columns,omitempty"`
	Parameters      map[string]Parameter `yaml:"parameters,omitempty"`
	SQL             []SQLVariant         `yaml:"sql"`
}

func (check Check) searchText() string {
	parts := []string{check.ID, check.TopicID, check.Name, check.Description}
	parts = append(parts, check.Guidance.UseWhen...)
	parts = append(parts, check.Guidance.Inspect...)

	for _, name := range sortedKeys(check.Columns) {
		parts = append(parts, name, check.Columns[name])
	}

	for _, name := range sortedKeys(check.Parameters) {
		parts = append(parts, name, check.Parameters[name].Description)
	}

	return strings.Join(parts, " ")
}

// Dependencies returns dependencies used by the check.
func (check Check) Dependencies() []string {
	dependencies := map[string]struct{}{}

	for _, variant := range check.SQL {
		for dependency := range variant.Requires {
			dependencies[dependency] = struct{}{}
		}
	}

	return sortedKeys(dependencies)
}
