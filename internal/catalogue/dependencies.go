package catalogue

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Version is a dependency version.
type Version string

const (
	// PostgreSQLDependency identifies the PostgreSQL server.
	PostgreSQLDependency string = "postgresql"
	// MinimumPostgreSQLVersion is the oldest supported major version.
	MinimumPostgreSQLVersion int = 14
)

var minimumSupportedVersions = map[string]Version{
	PostgreSQLDependency: Version(strconv.Itoa(MinimumPostgreSQLVersion)),
	"pg_buffercache":     "1.3",
	"pg_stat_statements": "1.5",
}

// UnmarshalYAML decodes a dependency version from a YAML string.
func (version *Version) UnmarshalYAML(node *yaml.Node) error {
	if node.Tag != "!!str" {
		return errors.New("version must be a string")
	}
	*version = Version(node.Value)
	return nil
}

func compareVersions(dependency, left, right string) (int, bool) {
	// Only major versions are supported for PostgreSQL, so reject any version with a dot.
	if dependency == PostgreSQLDependency &&
		(strings.Contains(left, ".") || strings.Contains(right, ".")) {
		return 0, false
	}
	leftParts, leftValid := numericVersion(left)
	rightParts, rightValid := numericVersion(right)
	if !leftValid || !rightValid {
		return 0, false
	}
	for index := 0; index < max(len(leftParts), len(rightParts)); index++ {
		var leftPart, rightPart uint64
		if index < len(leftParts) {
			leftPart = leftParts[index]
		}
		if index < len(rightParts) {
			rightPart = rightParts[index]
		}
		if leftPart < rightPart {
			return -1, true
		}
		if leftPart > rightPart {
			return 1, true
		}
	}
	return 0, true
}

// MinimumDependencyVersion returns a dependency's supported minimum version.
func MinimumDependencyVersion(dependency string) (Version, bool) {
	minimum, supported := minimumSupportedVersions[dependency]
	return minimum, supported
}

// CheckServerVersion reports whether pgcue supports a PostgreSQL major version.
func CheckServerVersion(major int) error {
	if major < MinimumPostgreSQLVersion {
		return fmt.Errorf(
			"PostgreSQL %d is unsupported; pgcue requires PostgreSQL %d or later",
			major, MinimumPostgreSQLVersion,
		)
	}
	return nil
}

// CheckDependencyVersion reports whether pgcue supports an installed dependency version.
func CheckDependencyVersion(dependency, actual string) error {
	minimum, _ := MinimumDependencyVersion(dependency)
	if VersionAtLeast(dependency, actual, minimum) {
		return nil
	}
	return fmt.Errorf("%s %s is unsupported; pgcue requires %s %s or later", dependency, actual, dependency, minimum)
}

// VersionAtLeast reports whether actual meets minimum.
func VersionAtLeast(dependency, actual string, minimum Version) bool {
	comparison, valid := compareVersions(dependency, actual, string(minimum))
	return valid && comparison >= 0
}

func numericVersion(version string) ([]uint64, bool) {
	if version == "" {
		return nil, false
	}

	parts := strings.Split(version, ".")
	result := make([]uint64, len(parts))
	for index, part := range parts {
		value, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return nil, false
		}
		result[index] = value
	}
	return result, true
}
