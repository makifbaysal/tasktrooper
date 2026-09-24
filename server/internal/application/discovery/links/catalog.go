package links

import (
	_ "embed"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

//go:embed catalog.yaml
var catalogYAML []byte

type catalogMatch struct {
	NPM  []string `yaml:"npm,omitempty"`
	Go   []string `yaml:"go,omitempty"`
	Py   []string `yaml:"py,omitempty"`
	Rust []string `yaml:"rust,omitempty"`
	Ruby []string `yaml:"ruby,omitempty"`
	PHP  []string `yaml:"php,omitempty"`
	JVM  []string `yaml:"jvm,omitempty"`
}

type catalogResource struct {
	Kind   domain.ResourceKind `yaml:"kind"`
	Vendor string              `yaml:"vendor"`
	Name   string              `yaml:"name"`
}

type catalogEntry struct {
	ID       string              `yaml:"id"`
	Match    catalogMatch        `yaml:"match"`
	Resource catalogResource     `yaml:"resource"`
	Protocol domain.LinkProtocol `yaml:"protocol"`
	Scope    string              `yaml:"scope"`
}

// ecosystem is one manifest kind the catalog matches dependency names from;
// "go" is always a prefix match (module paths nest), every other ecosystem
// is exact unless the pattern itself ends in "*".
type ecosystem string

const (
	ecoNPM  ecosystem = "npm"
	ecoGo   ecosystem = "go"
	ecoPy   ecosystem = "py"
	ecoRust ecosystem = "rust"
	ecoRuby ecosystem = "ruby"
	ecoPHP  ecosystem = "php"
	ecoJVM  ecosystem = "jvm"
)

func (e catalogEntry) patternsFor(eco ecosystem) []string {
	switch eco {
	case ecoNPM:
		return e.Match.NPM
	case ecoGo:
		return e.Match.Go
	case ecoPy:
		return e.Match.Py
	case ecoRust:
		return e.Match.Rust
	case ecoRuby:
		return e.Match.Ruby
	case ecoPHP:
		return e.Match.PHP
	case ecoJVM:
		return e.Match.JVM
	}
	return nil
}

type depCatalog struct {
	entries []catalogEntry
	byID    map[string]catalogEntry
}

func loadCatalog(raw []byte) (depCatalog, error) {
	var entries []catalogEntry
	if err := yaml.Unmarshal(raw, &entries); err != nil {
		return depCatalog{}, fmt.Errorf("parse catalog.yaml: %w", err)
	}
	byID := make(map[string]catalogEntry, len(entries))
	for _, e := range entries {
		byID[e.ID] = e
	}
	return depCatalog{entries: entries, byID: byID}, nil
}

var catalog = mustLoadCatalog()

func mustLoadCatalog() depCatalog {
	c, err := loadCatalog(catalogYAML)
	if err != nil {
		panic("links: " + err.Error())
	}
	return c
}

func (c depCatalog) byIDOrZero(id string) (catalogEntry, bool) {
	e, ok := c.byID[id]
	return e, ok
}

// match finds the catalog entry whose pattern for eco best matches name; an
// exact pattern always outranks a "*"-prefix pattern, and among prefixes the
// longest one wins.
func (c depCatalog) match(eco ecosystem, name string) (catalogEntry, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return catalogEntry{}, false
	}
	var best catalogEntry
	bestScore := -1
	found := false
	for _, e := range c.entries {
		for _, pattern := range e.patternsFor(eco) {
			score, ok := matchScore(eco, pattern, name)
			if !ok {
				continue
			}
			if score > bestScore {
				best, bestScore, found = e, score, true
			}
		}
	}
	return best, found
}

func matchScore(eco ecosystem, pattern, name string) (int, bool) {
	if eco == ecoGo {
		if strings.HasPrefix(name, pattern) {
			return len(pattern), true
		}
		return 0, false
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		if strings.HasPrefix(name, prefix) {
			return len(prefix), true
		}
		return 0, false
	}
	if name == pattern {
		return 1 << 20, true
	}
	return 0, false
}
