package catalogue

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

var (
	parameterName     = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	contentID         = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	namedSQLParameter = regexp.MustCompile(`@[a-zA-Z_][a-zA-Z0-9_]*`)
)

func validID(id string) bool { return contentID.MatchString(id) }

func blank(value string) bool { return strings.TrimSpace(value) == "" }

// validateList requires non-blank entries and, when distinct is set, no repeated entries.
func validateList(field string, values []string, distinct bool) error {
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		if blank(value) {
			return fmt.Errorf("%s entry %d must not be empty", field, index+1)
		}
		if _, repeated := seen[value]; repeated && distinct {
			return fmt.Errorf("%s entry %d repeats %q", field, index+1, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateTopic(topic Topic) error {
	switch {
	case !validID(topic.ID):
		return fmt.Errorf("invalid topic ID %q", topic.ID)
	case blank(topic.Name):
		return errors.New("name is required")
	case blank(topic.Description):
		return errors.New("description is required")
	case len(topic.KeyConcepts) == 0:
		return errors.New("key_concepts are required")
	case blank(topic.Overview):
		return errors.New("overview is required")
	}
	return validateList("key_concepts", topic.KeyConcepts, true)
}

func validateInvestigation(investigation Investigation) error {
	switch {
	case !validID(investigation.ID):
		return fmt.Errorf("invalid investigation ID %q", investigation.ID)
	case blank(investigation.Name):
		return errors.New("name is required")
	case blank(investigation.Description):
		return errors.New("description is required")
	case len(investigation.Topics) == 0:
		return errors.New("topics are required")
	}
	if err := validateTopicIDs(investigation.Topics); err != nil {
		return err
	}
	if len(investigation.Steps) == 0 {
		return errors.New("steps are required")
	}
	return validateSteps(investigation.Steps)
}

func validateTopicIDs(topicIDs []string) error {
	seen := make(map[string]struct{}, len(topicIDs))
	for _, topicID := range topicIDs {
		if !validID(topicID) {
			return fmt.Errorf("invalid topic ID %q", topicID)
		}
		if _, exists := seen[topicID]; exists {
			return fmt.Errorf("duplicate topic %q", topicID)
		}
		seen[topicID] = struct{}{}
	}
	return nil
}

func validateSteps(steps []InvestigationStep) error {
	seen := make(map[string]struct{}, len(steps))
	for index, step := range steps {
		switch {
		case blank(step.Check):
			return fmt.Errorf("step %d check is required", index+1)
		case blank(step.Reason):
			return fmt.Errorf("step %d reason is required", index+1)
		}
		if _, exists := seen[step.Check]; exists {
			return fmt.Errorf("step %d repeats check %q", index+1, step.Check)
		}
		seen[step.Check] = struct{}{}
	}
	return nil
}

func validateCheck(check Check) error {
	for _, validate := range []func(Check) error{
		validateCheckMetadata,
		validateCheckDocumentation,
		validateCheckParameters,
		validateCheckColumns,
		validateCheckVariants,
	} {
		if err := validate(check); err != nil {
			return err
		}
	}
	return nil
}

func validateCheckMetadata(check Check) error {
	switch {
	case !validID(check.TopicID):
		return fmt.Errorf("invalid topic ID %q", check.TopicID)
	case !validID(strings.TrimPrefix(check.ID, check.TopicID+".")):
		return fmt.Errorf("invalid check ID %q for topic %q", check.ID, check.TopicID)
	case blank(check.Name):
		return errors.New("name is required")
	case blank(check.Description):
		return errors.New("description is required")
	}
	return nil
}

func validateCheckDocumentation(check Check) error {
	switch {
	case check.Result == "":
		return errors.New("result is required")
	case !check.Result.Valid():
		return fmt.Errorf("unsupported result semantics %q", check.Result)
	}

	for _, field := range []struct {
		name   string
		values []string
	}{
		{"guidance.use_when", check.Guidance.UseWhen},
		{"guidance.inspect", check.Guidance.Inspect},
	} {
		if len(field.values) == 0 {
			return fmt.Errorf("%s is required", field.name)
		}
		if err := validateList(field.name, field.values, false); err != nil {
			return err
		}
	}

	if err := validateList("guidance.related", check.Guidance.Related, true); err != nil {
		return err
	}
	if index := slices.Index(check.Guidance.Related, check.ID); index >= 0 {
		return fmt.Errorf("guidance.related entry %d references the check itself %q", index+1, check.ID)
	}
	return nil
}

func validateCheckParameters(check Check) error {
	for _, name := range sortedKeys(check.Parameters) {
		if err := validateParameter(name, check.Parameters[name]); err != nil {
			return err
		}
	}
	return nil
}

func validateParameter(name string, parameter Parameter) error {
	if !parameterName.MatchString(name) {
		return fmt.Errorf("invalid parameter name %q", name)
	}
	if parameter.Minimum != nil && parameter.Type != IntegerParameter {
		return fmt.Errorf("parameter %q: minimum is only supported for integers", name)
	}
	switch parameter.Type {
	case StringParameter, IntegerParameter, BooleanParameter, DurationParameter:
	default:
		return fmt.Errorf("parameter %q has unsupported parameter type %q", name, parameter.Type)
	}

	defaultValue, hasDefault := parameter.DefaultValue()
	if parameter.Required && hasDefault {
		return fmt.Errorf("parameter %q cannot be required and have a default value", name)
	}
	if !hasDefault || defaultValue == nil {
		return nil
	}
	switch defaultValue.(type) {
	case string, bool, int, int64, uint64, float64:
	default:
		return fmt.Errorf("invalid default value for parameter %q: must be a scalar value", name)
	}
	if _, err := ParseValue(parameter, fmt.Sprint(defaultValue)); err != nil {
		return fmt.Errorf("invalid default value for parameter %q: %w", name, err)
	}
	return nil
}

func validateCheckColumns(check Check) error {
	if len(check.Columns) == 0 {
		return errors.New("at least one column is required")
	}
	for _, name := range sortedKeys(check.Columns) {
		if blank(name) {
			return errors.New("column name must not be empty")
		}
		if blank(check.Columns[name]) {
			return fmt.Errorf("column %q description must not be empty", name)
		}
	}
	return nil
}

func validateCheckVariants(check Check) error {
	if len(check.SQL) == 0 {
		return errors.New("sql is required")
	}

	cases := map[string]struct{}{}
	declaresMinimum := map[string]bool{}

	for index, variant := range check.SQL {
		switch {
		case len(variant.Requires) == 0:
			return fmt.Errorf("SQL variant %d requires at least one dependency", index+1)
		case blank(variant.Statement):
			return fmt.Errorf("SQL variant %d statement is required", index+1)
		}
		if err := recordMinimumDeclarations(variant.Requires, declaresMinimum); err != nil {
			return fmt.Errorf("SQL variant %d: %w", index+1, err)
		}

		key := requirementKey(variant.Requires)
		if _, duplicate := cases[key]; duplicate {
			return fmt.Errorf("SQL variant %d duplicates an earlier dependency case", index+1)
		}
		cases[key] = struct{}{}

		if err := validateVariantStatement(check.Parameters, variant); err != nil {
			return fmt.Errorf("SQL variant %d: %w", index+1, err)
		}
	}

	for _, dependency := range sortedKeys(declaresMinimum) {
		if !declaresMinimum[dependency] {
			minimum, _ := MinimumDependencyVersion(dependency)
			return fmt.Errorf("no SQL variant declaring minimum %s version %s", dependency, minimum)
		}
	}

	return validateVariantOverlaps(check, cases)
}

// recordMinimumDeclarations validates requirements and notes which dependencies are
// required at exactly their supported minimum version.
func recordMinimumDeclarations(requires map[string]Version, declaresMinimum map[string]bool) error {
	for _, dependency := range sortedKeys(requires) {
		isMinimum, err := validateDependency(dependency, requires[dependency])
		if err != nil {
			return err
		}
		declaresMinimum[dependency] = declaresMinimum[dependency] || isMinimum
	}
	return nil
}

func validateDependency(dependency string, version Version) (bool, error) {
	minimum, supported := MinimumDependencyVersion(dependency)
	if !supported {
		return false, fmt.Errorf("has unsupported dependency %q", dependency)
	}

	comparison, valid := compareVersions(dependency, string(version), string(minimum))
	if !valid {
		return false, fmt.Errorf("has invalid %s version %q", dependency, version)
	}
	if comparison < 0 {
		return false, fmt.Errorf("requires %s %s, below the supported minimum %s", dependency, version, minimum)
	}

	return comparison == 0, nil
}

func validateVariantStatement(parameters map[string]Parameter, variant SQLVariant) error {
	extensions, err := referencedExtensions(variant.Statement)
	if err != nil {
		return err
	}

	for _, extension := range extensions {
		if extension == PostgreSQLDependency {
			return errors.New("PostgreSQL cannot be referenced as an extension")
		}
		if _, declared := variant.Requires[extension]; !declared {
			return fmt.Errorf("extension %q is referenced but not declared in requires", extension)
		}
	}

	if err := validateSQLParameters(parameters, variant.Statement); err != nil {
		return fmt.Errorf("SQL parameters: %w", err)
	}

	return nil
}

func validateVariantOverlaps(check Check, variantKeys map[string]struct{}) error {
	for leftIndex, left := range check.SQL {
		for rightIndex := leftIndex + 1; rightIndex < len(check.SQL); rightIndex++ {
			right := check.SQL[rightIndex]

			if requirementsDominate(left.Requires, right.Requires) ||
				requirementsDominate(right.Requires, left.Requires) {
				continue
			}

			combinedRequirements := mergeRequirements(left.Requires, right.Requires)
			if _, exists := variantKeys[requirementKey(combinedRequirements)]; !exists {
				return fmt.Errorf(
					"SQL variants %d and %d have ambiguous compatibility requirements",
					leftIndex+1,
					rightIndex+1,
				)
			}
		}
	}

	return nil
}

func (c *Catalogue) validate() error {
	for _, id := range sortedKeys(c.topics) {
		if err := validateTopic(c.topics[id]); err != nil {
			return fmt.Errorf("topic %q: %w", id, err)
		}
	}

	for _, id := range sortedKeys(c.checks) {
		if err := validateCheck(c.checks[id]); err != nil {
			return fmt.Errorf("check %q: %w", id, err)
		}
	}

	for _, id := range sortedKeys(c.investigations) {
		if err := validateInvestigation(c.investigations[id]); err != nil {
			return fmt.Errorf("investigation %q: %w", id, err)
		}
	}

	return c.validateReferences()
}

func (c *Catalogue) validateReferences() error {
	for _, id := range sortedKeys(c.checks) {
		if err := c.validateCheckReferences(c.checks[id]); err != nil {
			return fmt.Errorf("check %q: %w", id, err)
		}
	}
	for _, id := range sortedKeys(c.investigations) {
		if err := c.validateInvestigationReferences(c.investigations[id]); err != nil {
			return fmt.Errorf("investigation %q: %w", id, err)
		}
	}
	return nil
}

func (c *Catalogue) validateCheckReferences(check Check) error {
	if _, exists := c.topics[check.TopicID]; !exists {
		return fmt.Errorf("references unknown topic %q", check.TopicID)
	}
	for index, related := range check.Guidance.Related {
		if _, exists := c.checks[related]; !exists {
			return fmt.Errorf("guidance.related entry %d references unknown check %q", index+1, related)
		}
	}
	return nil
}

func (c *Catalogue) validateInvestigationReferences(investigation Investigation) error {
	// Topics and investigations both use bare IDs, so commands such as describe could not
	// tell them apart. Check IDs always contain a dot and cannot collide.
	if _, exists := c.topics[investigation.ID]; exists {
		return errors.New("ID is also used by a topic")
	}
	for index, topicID := range investigation.Topics {
		if _, exists := c.topics[topicID]; !exists {
			return fmt.Errorf("topics entry %d references unknown topic %q", index+1, topicID)
		}
	}
	for index, step := range investigation.Steps {
		check, exists := c.checks[step.Check]
		if !exists {
			return fmt.Errorf("step %d references unknown check %q", index+1, step.Check)
		}
		if !slices.Contains(investigation.Topics, check.TopicID) {
			return fmt.Errorf(
				"step %d references check %q from undeclared topic %q",
				index+1, step.Check, check.TopicID,
			)
		}
		// Investigations look at the whole system, so every step runs with its defaults.
		for _, name := range sortedKeys(check.Parameters) {
			if check.Parameters[name].Required {
				return fmt.Errorf("step %d references check %q, which requires parameter %q", index+1, step.Check, name)
			}
		}
	}
	return nil
}

func validateSQLParameters(parameters map[string]Parameter, sql string) error {
	candidates := pgx.NamedArgs{}
	for _, match := range namedSQLParameter.FindAllString(sql, -1) {
		name := match[1:]
		candidates[name] = name
	}

	// Use pgx to ignore parameter-like text inside SQL strings and comments.
	_, arguments, _ := candidates.RewriteQuery(context.Background(), nil, sql, nil)

	used := make(map[string]struct{}, len(arguments))
	for _, argument := range arguments {
		name := argument.(string)
		if _, declared := parameters[name]; !declared {
			return fmt.Errorf("SQL references undeclared parameter %q", name)
		}
		used[name] = struct{}{}
	}

	for _, name := range sortedKeys(parameters) {
		if _, exists := used[name]; !exists {
			return fmt.Errorf("parameter %q is declared but not referenced in SQL", name)
		}
	}

	return nil
}
