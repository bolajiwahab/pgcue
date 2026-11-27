package catalogue

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/bolajiwahab/pgcue/knowledge"
)

// LoadCatalogue loads and validates the embedded knowledge catalogue.
func LoadCatalogue() (*Catalogue, error) { return loadCatalogue(knowledge.FS) }

type catalogueDocuments struct {
	topics         []Topic
	checks         []Check
	investigations []Investigation
}

func loadCatalogue(knowledgeFS fs.FS) (*Catalogue, error) {
	var documents catalogueDocuments
	err := fs.WalkDir(knowledgeFS, ".", func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk knowledge catalogue at %q: %w", filename, walkErr)
		}

		switch {
		case entry.IsDir():
			return nil
		case strings.HasSuffix(filename, ".yml"):
			return fmt.Errorf("unsupported knowledge document %q: use the .yaml extension", filename)
		case !strings.HasSuffix(filename, ".yaml"):
			return nil // Such as embed.go.
		}

		content, err := fs.ReadFile(knowledgeFS, filename)
		if err != nil {
			return fmt.Errorf("read knowledge catalogue at %q: %w", filename, err)
		}

		return documents.add(filename, content)
	})
	if err != nil {
		return nil, err
	}
	return NewCatalogue(documents.topics, documents.checks, documents.investigations)
}

func (documents *catalogueDocuments) add(filename string, content []byte) error {
	parts := strings.Split(filename, "/")
	switch {
	case len(parts) == 2 && parts[0] == "investigations":
		investigation, err := parseInvestigation(filename, strings.TrimSuffix(parts[1], ".yaml"), content)
		if err != nil {
			return err
		}
		documents.investigations = append(documents.investigations, investigation)
	case len(parts) == 4 && parts[0] == "topics" && parts[2] == "checks":
		check, err := parseCheck(filename, parts[1], strings.TrimSuffix(parts[3], ".yaml"), content)
		if err != nil {
			return err
		}
		documents.checks = append(documents.checks, check)
	case len(parts) == 3 && parts[0] == "topics" && parts[2] == "topic.yaml":
		topic, err := parseTopic(filename, parts[1], content)
		if err != nil {
			return err
		}
		documents.topics = append(documents.topics, topic)
	default:
		return fmt.Errorf(
			"unsupported knowledge document %q: expected topics/<topic>/topic.yaml, topics/<topic>/checks/<check>.yaml, or investigations/<investigation>.yaml",
			filename,
		)
	}
	return nil
}

func decodeYAML(filename string, content []byte, target any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(content))

	var document yaml.Node
	if err := decoder.Decode(&document); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("parse %q: %w", filename, err)
	}

	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("parse %q: expected a YAML mapping", filename)
	}

	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("parse %q: %w", filename, err)
		}
		return fmt.Errorf("parse %q: multiple YAML documents are not supported", filename)
	}

	decoder = yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)

	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("parse %q: %w", filename, err)
	}

	return nil
}

func parseTopic(filename, topicID string, content []byte) (Topic, error) {
	topic := Topic{ID: topicID}
	if err := decodeYAML(filename, content, &topic); err != nil {
		return Topic{}, err
	}
	return topic, nil
}

func parseInvestigation(filename, investigationID string, content []byte) (Investigation, error) {
	investigation := Investigation{ID: investigationID}
	if err := decodeYAML(filename, content, &investigation); err != nil {
		return Investigation{}, err
	}
	return investigation, nil
}

func parseCheck(filename, topicID, checkID string, content []byte) (Check, error) {
	check := Check{ID: topicID + "." + checkID, TopicID: topicID}
	if err := decodeYAML(filename, content, &check); err != nil {
		return Check{}, err
	}
	return check, nil
}
