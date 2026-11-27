package catalogue

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

var (
	metadataPlaceholder       = regexp.MustCompile(`\{\{([^{}]*)\}\}`)
	extensionSchemaExpression = regexp.MustCompile(`^extension\.([a-z][a-z0-9_-]*)\.schema$`)
)

// errNoCompatibleSQL indicates that no variant matches the environment.
var errNoCompatibleSQL = errors.New("no compatible SQL variant")

// SQLVariant is SQL with dependency requirements.
type SQLVariant struct {
	Requires  map[string]Version `yaml:"requires"`
	Statement string             `yaml:"statement"`
}

// VariantForEnvironment selects a compatible SQL variant.
func (check Check) VariantForEnvironment(environment map[string]string) (SQLVariant, error) {
	var compatible []SQLVariant
	for _, variant := range check.SQL {
		if variant.compatibleWith(environment) {
			compatible = append(compatible, variant)
		}
	}
	if variant, ok := mostSpecificVariant(compatible); ok {
		return variant, nil
	}

	var details strings.Builder
	for _, dependency := range postgresqlFirst(check.Dependencies()) {
		version := environment[dependency]
		if version == "" {
			version = "not installed"
		}
		fmt.Fprintf(&details, "\n%s: %s", dependency, version)
	}
	return SQLVariant{}, fmt.Errorf(
		"check %q: %w for the current environment%s",
		check.ID,
		errNoCompatibleSQL,
		details.String(),
	)
}

func (variant SQLVariant) compatibleWith(environment map[string]string) bool {
	for dependency, minimum := range variant.Requires {
		if !VersionAtLeast(dependency, environment[dependency], minimum) {
			return false
		}
	}
	return true
}

// mostSpecificVariant returns the compatible variant that no other compatible variant
// dominates. Validation rejects ambiguous variants, so there is at most one.
func mostSpecificVariant(compatible []SQLVariant) (SQLVariant, bool) {
	for _, candidate := range compatible {
		if !slices.ContainsFunc(compatible, func(other SQLVariant) bool {
			return requirementsDominate(other.Requires, candidate.Requires)
		}) {
			return candidate, true
		}
	}
	return SQLVariant{}, false
}

// Compatibility describes the variant requirements.
func (variant SQLVariant) Compatibility() string {
	dependencies := postgresqlFirst(sortedKeys(variant.Requires))
	parts := make([]string, 0, len(dependencies))

	for _, dependency := range dependencies {
		version := variant.Requires[dependency]
		if dependency == PostgreSQLDependency {
			dependency = "PostgreSQL"
		}
		parts = append(parts, fmt.Sprintf("%s %s+", dependency, version))
	}

	return strings.Join(parts, " with ")
}

func requirementsDominate(candidate, other map[string]Version) bool {
	stricter := len(candidate) > len(other)

	for dependency, requiredVersion := range other {
		version, exists := candidate[dependency]
		if !exists {
			return false
		}

		comparison, valid := compareVersions(
			dependency,
			string(version),
			string(requiredVersion),
		)
		if !valid || comparison < 0 {
			return false
		}

		if comparison > 0 {
			stricter = true
		}
	}

	return stricter
}

func referencedExtensions(sql string) ([]string, error) {
	if strings.Contains(sql, "{{{") || strings.Contains(sql, "}}}") {
		return nil, errors.New("malformed metadata placeholder")
	}

	matches := metadataPlaceholder.FindAllStringSubmatch(sql, -1)
	references := make([]string, 0, len(matches))

	for _, match := range matches {
		expression := extensionSchemaExpression.FindStringSubmatch(strings.TrimSpace(match[1]))
		if expression == nil {
			return nil, fmt.Errorf("unsupported metadata placeholder %q", match[0])
		}
		references = append(references, expression[1])
	}

	remaining := metadataPlaceholder.ReplaceAllString(sql, "")
	if strings.Contains(remaining, "{{") || strings.Contains(remaining, "}}") {
		return nil, errors.New("malformed metadata placeholder")
	}

	return references, nil
}

// ResolveExtensionSchemas replaces extension schema placeholders with the quoted schemas.
// Validation guarantees well-formed placeholders, and a variant is only selected when its
// extensions are installed, so each referenced schema is known.
func ResolveExtensionSchemas(sql string, schemas map[string]string) string {
	return metadataPlaceholder.ReplaceAllStringFunc(sql, func(placeholder string) string {
		expression := strings.TrimSpace(placeholder[2 : len(placeholder)-2])
		match := extensionSchemaExpression.FindStringSubmatch(expression)
		return pgx.Identifier{schemas[match[1]]}.Sanitize()
	})
}

func postgresqlFirst(dependencies []string) []string {
	ordered := make([]string, 0, len(dependencies))
	if slices.Contains(dependencies, PostgreSQLDependency) {
		ordered = append(ordered, PostgreSQLDependency)
	}
	for _, dependency := range dependencies {
		if dependency != PostgreSQLDependency {
			ordered = append(ordered, dependency)
		}
	}
	return ordered
}

// requirementKey returns a deterministic key for a set of dependency requirements.
// Dependencies are sorted, and numerically equivalent versions (such as 1.8
// and 1.8.0) produce the same key. Versions are validated before keys are built.
func requirementKey(requirements map[string]Version) string {
	var key strings.Builder

	for _, dependency := range sortedKeys(requirements) {
		version, _ := numericVersion(string(requirements[dependency]))
		for len(version) > 1 && version[len(version)-1] == 0 {
			version = version[:len(version)-1]
		}

		parts := make([]string, len(version))
		for index, part := range version {
			parts[index] = strconv.FormatUint(part, 10)
		}

		fmt.Fprintf(&key, "%s=%s\x00", dependency, strings.Join(parts, "."))
	}

	return key.String()
}

func mergeRequirements(left, right map[string]Version) map[string]Version {
	merged := maps.Clone(left)

	for dependency, rightVersion := range right {
		leftVersion, exists := merged[dependency]
		if !exists {
			merged[dependency] = rightVersion
			continue
		}

		comparison, _ := compareVersions(
			dependency,
			string(rightVersion),
			string(leftVersion),
		)
		if comparison > 0 {
			merged[dependency] = rightVersion
		}
	}

	return merged
}
