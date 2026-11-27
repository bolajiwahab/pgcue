// Package catalogue loads and searches pgcue content.
package catalogue

import (
	"maps"
	"slices"
	"strings"
)

// sortedKeys returns the keys of a map in sorted order.
func sortedKeys[V any](values map[string]V) []string {
	return slices.Sorted(maps.Keys(values))
}

// Catalogue stores checks, investigations, and topics.
type Catalogue struct {
	checks         map[string]Check
	investigations map[string]Investigation
	topics         map[string]Topic
}

// Topic explains an operational area.
type Topic struct {
	ID          string   `yaml:"-"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Overview    string   `yaml:"overview"`
	KeyConcepts []string `yaml:"key_concepts,omitempty"`
}

// Investigation defines an investigation.
type Investigation struct {
	ID          string              `yaml:"-"`
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	Topics      []string            `yaml:"topics"`
	Steps       []InvestigationStep `yaml:"steps"`
}

// InvestigationStep references one check in an investigation.
type InvestigationStep struct {
	Check  string `yaml:"check"`
	Reason string `yaml:"reason"`
}

// SearchResults groups matching catalogue content.
type SearchResults struct {
	Topics         []Topic
	Investigations []Investigation
	Checks         []Check
}

// NewCatalogue constructs and validates a catalogue. IDs are unique because the loader
// derives them from file paths.
func NewCatalogue(topics []Topic, checks []Check, investigations []Investigation) (*Catalogue, error) {
	c := &Catalogue{topics: map[string]Topic{}, checks: map[string]Check{}, investigations: map[string]Investigation{}}
	for _, topic := range topics {
		c.topics[topic.ID] = topic
	}
	for _, check := range checks {
		c.checks[check.ID] = check
	}
	for _, investigation := range investigations {
		c.investigations[investigation.ID] = investigation
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// sortedValues returns values ordered by key, keeping those accepted by keep.
func sortedValues[V any](values map[string]V, keep func(V) bool) []V {
	var result []V
	for _, key := range sortedKeys(values) {
		if value := values[key]; keep == nil || keep(value) {
			result = append(result, value)
		}
	}
	return result
}

// GetCheck returns a check by ID.
func (c *Catalogue) GetCheck(id string) (Check, bool) {
	check, exists := c.checks[id]
	return check, exists
}

// GetTopic returns a topic by ID.
func (c *Catalogue) GetTopic(id string) (Topic, bool) {
	topic, exists := c.topics[id]
	return topic, exists
}

// GetInvestigation returns an investigation by ID.
func (c *Catalogue) GetInvestigation(id string) (Investigation, bool) {
	investigation, exists := c.investigations[id]
	return investigation, exists
}

// Checks returns all checks ordered by ID.
func (c *Catalogue) Checks() []Check { return sortedValues(c.checks, nil) }

// Topics returns all topics ordered by ID.
func (c *Catalogue) Topics() []Topic { return sortedValues(c.topics, nil) }

// Investigations returns all investigations ordered by ID.
func (c *Catalogue) Investigations() []Investigation {
	return sortedValues(c.investigations, nil)
}

// CheckIDs returns all check IDs in order.
func (c *Catalogue) CheckIDs() []string { return sortedKeys(c.checks) }

// TopicIDs returns all topic IDs in order, including topics without checks.
func (c *Catalogue) TopicIDs() []string { return sortedKeys(c.topics) }

// InvestigationIDs returns all investigation IDs in order.
func (c *Catalogue) InvestigationIDs() []string { return sortedKeys(c.investigations) }

// ChecksForTopic returns checks belonging to a topic, ordered by ID.
func (c *Catalogue) ChecksForTopic(topicID string) []Check {
	return sortedValues(c.checks, func(check Check) bool { return check.TopicID == topicID })
}

// InvestigationsForTopic returns investigations explicitly associated with a topic.
func (c *Catalogue) InvestigationsForTopic(topicID string) []Investigation {
	return sortedValues(c.investigations, func(investigation Investigation) bool {
		return slices.Contains(investigation.Topics, topicID)
	})
}

// CheckIDsUsing returns the IDs of checks using a dependency, in order.
func (c *Catalogue) CheckIDsUsing(dependency string) []string {
	var ids []string
	for _, id := range sortedKeys(c.checks) {
		if slices.Contains(c.checks[id].Dependencies(), dependency) {
			ids = append(ids, id)
		}
	}
	return ids
}

// Dependencies returns dependencies used by checks.
func (c *Catalogue) Dependencies() []string {
	dependencies := map[string]struct{}{}
	for _, check := range c.checks {
		for _, dependency := range check.Dependencies() {
			dependencies[dependency] = struct{}{}
		}
	}
	return sortedKeys(dependencies)
}

// Search finds catalogue content matching every term.
func (c *Catalogue) Search(terms string) SearchResults {
	words := strings.Fields(strings.ToLower(terms))
	if len(words) == 0 {
		return SearchResults{}
	}
	matches := func(text string) bool {
		text = strings.ToLower(text)
		for _, word := range words {
			if !strings.Contains(text, word) {
				return false
			}
		}
		return true
	}
	return SearchResults{
		Topics: sortedValues(c.topics, func(topic Topic) bool { return matches(topic.searchText()) }),
		Investigations: sortedValues(c.investigations, func(investigation Investigation) bool {
			return matches(investigation.searchText())
		}),
		Checks: sortedValues(c.checks, func(check Check) bool { return matches(check.searchText()) }),
	}
}

func (topic Topic) searchText() string {
	parts := []string{topic.ID, topic.Name, topic.Description, topic.Overview}
	return strings.Join(append(parts, topic.KeyConcepts...), " ")
}

func (investigation Investigation) searchText() string {
	parts := []string{investigation.ID, investigation.Name, investigation.Description}
	parts = append(parts, investigation.Topics...)
	for _, step := range investigation.Steps {
		parts = append(parts, step.Check, step.Reason)
	}
	return strings.Join(parts, " ")
}
