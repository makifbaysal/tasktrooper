package signals

import (
	"bytes"
	"fmt"
	"io/fs"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const goldenFile = "golden.yaml"

type goldenDoc struct {
	Golden map[string][]string `yaml:"golden"`
}

// loadGolden reads the per-role golden signal lists and checks each against
// the signal catalog. A golden signal must say which way is better: deploy
// impact judges a release by them.
func loadGolden(fsys fs.FS, signals []domain.Signal) (map[domain.ComponentRole][]string, error) {
	raw, err := fs.ReadFile(fsys, goldenFile)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", goldenFile, err)
	}
	var doc goldenDoc
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%s: %w", goldenFile, err)
	}
	known := make(map[string]domain.Signal, len(signals))
	for _, s := range signals {
		known[s.ID] = s
	}
	out := make(map[domain.ComponentRole][]string, len(doc.Golden))
	for role, ids := range doc.Golden {
		r := domain.ComponentRole(role)
		if !domain.ValidComponentRole(r) {
			return nil, fmt.Errorf("%s: unknown role %q", goldenFile, role)
		}
		for i, id := range ids {
			sig, ok := known[id]
			if !ok {
				return nil, fmt.Errorf("%s: %s lists %q, which is not in the signal catalog", goldenFile, role, id)
			}
			if sig.Better == "" {
				return nil, fmt.Errorf("%s: %s lists %q, which has no better direction in the signal catalog", goldenFile, role, id)
			}
			if slices.Contains(ids[:i], id) {
				return nil, fmt.Errorf("%s: %s lists %q twice", goldenFile, role, id)
			}
		}
		out[r] = slices.Clone(ids)
	}
	return out, nil
}
