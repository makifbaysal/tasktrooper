package datasource

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	signalsFile = "signals.yaml"
	recipesGlob = "recipes/*.yaml"
)

var (
	signalIDPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
	recipeIDPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	paramPattern     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	metricPattern    = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
	placeholderRegex = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)
)

// Catalog is the fixed signal list plus the recipes shipped beside it.
type Catalog struct {
	signals     []domain.Signal
	signalIndex map[string]domain.Signal
	recipes     []domain.Recipe
	kinds       []domain.DataSourceKind
}

type signalsDoc struct {
	Signals []struct {
		ID          string   `yaml:"id"`
		Kind        string   `yaml:"kind"`
		Unit        string   `yaml:"unit"`
		Roles       []string `yaml:"roles"`
		Aggregation string   `yaml:"aggregation"`
	} `yaml:"signals"`
}

type recipeDoc struct {
	ID         string `yaml:"id"`
	SourceKind string `yaml:"source_kind"`
	AppliesTo  struct {
		Technology      string `yaml:"technology"`
		MessagingSystem string `yaml:"messaging_system"`
	} `yaml:"applies_to"`
	Detect struct {
		Metric string `yaml:"metric"`
	} `yaml:"detect"`
	Params  []string          `yaml:"params"`
	Signals map[string]string `yaml:"signals"`
}

// LoadCatalog reads signals.yaml and recipes/*.yaml from fsys and validates
// every recipe against the signals and kinds. A recipe's file name must be its
// id, so a duplicate id cannot hide in a second file.
func LoadCatalog(fsys fs.FS, kinds []domain.DataSourceKind) (*Catalog, error) {
	c := &Catalog{signalIndex: map[string]domain.Signal{}, kinds: kinds}
	if err := c.loadSignals(fsys); err != nil {
		return nil, err
	}
	files, err := fs.Glob(fsys, recipesGlob)
	if err != nil {
		return nil, fmt.Errorf("list recipes: %w", err)
	}
	sort.Strings(files)
	for _, name := range files {
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		r, err := decodeRecipe(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if want := strings.TrimSuffix(path.Base(name), ".yaml"); r.ID != want {
			return nil, fmt.Errorf("%s: id %q must match the file name %q", name, r.ID, want)
		}
		r.Origin = domain.RecipeOriginCatalog
		if err := c.ValidateRecipe(r); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		c.recipes = append(c.recipes, r)
	}
	return c, nil
}

func (c *Catalog) loadSignals(fsys fs.FS) error {
	raw, err := fs.ReadFile(fsys, signalsFile)
	if err != nil {
		return fmt.Errorf("read %s: %w", signalsFile, err)
	}
	var doc signalsDoc
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return fmt.Errorf("%s: %w", signalsFile, err)
	}
	for _, s := range doc.Signals {
		sig := domain.Signal{
			ID:          s.ID,
			Kind:        domain.SignalKind(s.Kind),
			Unit:        s.Unit,
			Aggregation: domain.SignalAggregation(s.Aggregation),
		}
		switch {
		case !signalIDPattern.MatchString(s.ID):
			return fmt.Errorf("%s: signal id %q is not dotted lowercase", signalsFile, s.ID)
		case c.signalIndex[s.ID].ID != "":
			return fmt.Errorf("%s: signal %q is listed twice", signalsFile, s.ID)
		case !domain.ValidSignalKind(sig.Kind):
			return fmt.Errorf("%s: signal %q has unknown kind %q", signalsFile, s.ID, s.Kind)
		case !domain.ValidSignalAggregation(sig.Aggregation):
			return fmt.Errorf("%s: signal %q has unknown aggregation %q", signalsFile, s.ID, s.Aggregation)
		case strings.TrimSpace(s.Unit) == "":
			return fmt.Errorf("%s: signal %q has no unit", signalsFile, s.ID)
		case len(s.Roles) == 0:
			return fmt.Errorf("%s: signal %q has no roles", signalsFile, s.ID)
		}
		for _, role := range s.Roles {
			if !domain.ValidComponentRole(domain.ComponentRole(role)) {
				return fmt.Errorf("%s: signal %q has unknown role %q", signalsFile, s.ID, role)
			}
			sig.Roles = append(sig.Roles, domain.ComponentRole(role))
		}
		c.signals = append(c.signals, sig)
		c.signalIndex[sig.ID] = sig
	}
	return nil
}

func decodeRecipe(raw []byte) (domain.Recipe, error) {
	var doc recipeDoc
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return domain.Recipe{}, err
	}
	signals := make(map[string]string, len(doc.Signals))
	for k, v := range doc.Signals {
		signals[k] = strings.TrimSpace(v)
	}
	return domain.Recipe{
		ID:         doc.ID,
		SourceKind: domain.DataSourceKind(doc.SourceKind),
		AppliesTo: domain.RecipeAppliesTo{
			Technology:      doc.AppliesTo.Technology,
			MessagingSystem: doc.AppliesTo.MessagingSystem,
		},
		Detect:  domain.RecipeDetect{Metric: doc.Detect.Metric},
		Params:  doc.Params,
		Signals: signals,
	}, nil
}

func (c *Catalog) Signals() []domain.Signal { return slices.Clone(c.signals) }

func (c *Catalog) Recipes() []domain.Recipe { return slices.Clone(c.recipes) }

// ValidateRecipe checks a recipe against the signal catalog and the
// registered kinds; every problem is reported, not just the first.
func (c *Catalog) ValidateRecipe(r domain.Recipe) error {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if !recipeIDPattern.MatchString(r.ID) {
		add("id %q must be lowercase letters, digits and dashes", r.ID)
	}
	if !slices.Contains(c.kinds, r.SourceKind) {
		add("source_kind %q is not a registered data source kind", r.SourceKind)
	}
	if r.Detect.Metric != "" && !metricPattern.MatchString(r.Detect.Metric) {
		add("detect.metric %q is not a metric name", r.Detect.Metric)
	}
	declared := map[string]bool{}
	for _, p := range r.Params {
		switch {
		case !paramPattern.MatchString(p):
			add("param %q must be lowercase letters, digits and underscores", p)
		case declared[p]:
			add("param %q is declared twice", p)
		}
		declared[p] = true
	}
	if len(r.Signals) == 0 {
		add("no signals")
	}
	ids := make([]string, 0, len(r.Signals))
	for id := range r.Signals {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		query := r.Signals[id]
		if _, ok := c.signalIndex[id]; !ok {
			add("signal %q is not in the signal catalog", id)
		}
		if strings.TrimSpace(query) == "" {
			add("signal %q has an empty query", id)
		}
		seen := map[string]bool{}
		for _, use := range placeholderUses(query) {
			if seen[use.name] {
				continue
			}
			switch {
			case builtinPlaceholders[use.name]:
				if use.quote != 0 || !use.inBrackets {
					seen[use.name] = true
					add("signal %q: $%s may appear only inside [...]", id, use.name)
				}
			case !declared[use.name]:
				seen[use.name] = true
				add("signal %q uses $%s, which is neither a declared param nor a built-in", id, use.name)
			case !use.inEqualityMatcher():
				seen[use.name] = true
				add("signal %q: $%s must sit inside a double-quoted label value matched with = or !=", id, use.name)
			}
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// placeholderUse is one $name in a query and where it sits. RenderQuery
// escapes a param for a double-quoted string, which is right only for an
// equality matcher's value: inside a regex matcher the value would be a
// pattern, and anywhere outside a string it would be raw PromQL.
type placeholderUse struct {
	name       string
	quote      byte
	op         string
	inBraces   bool
	inBrackets bool
}

func (u placeholderUse) inEqualityMatcher() bool {
	return u.quote == '"' && u.inBraces && (u.op == "=" || u.op == "!=")
}

func placeholderUses(q string) []placeholderUse {
	var uses []placeholderUse
	braces, brackets := 0, 0
	for i := 0; i < len(q); i++ {
		switch c := q[i]; c {
		case '"', '\'', '`':
			op := matcherOpBefore(q, i)
			j := i + 1
			for ; j < len(q) && q[j] != c; j++ {
				if q[j] == '\\' && c != '`' {
					j++
					continue
				}
				if q[j] == '$' {
					if name := identAt(q, j+1); name != "" {
						uses = append(uses, placeholderUse{name: name, quote: c, op: op, inBraces: braces > 0, inBrackets: brackets > 0})
						j += len(name)
					}
				}
			}
			i = j
		case '{':
			braces++
		case '}':
			braces = max(braces-1, 0)
		case '[':
			brackets++
		case ']':
			brackets = max(brackets-1, 0)
		case '$':
			if name := identAt(q, i+1); name != "" {
				uses = append(uses, placeholderUse{name: name, inBraces: braces > 0, inBrackets: brackets > 0})
				i += len(name)
			}
		}
	}
	return uses
}

// matcherOpBefore is the operator right before the string starting at i:
// "=", "!=", "=~", "!~", or "" (a comparison like == or >=, or none).
func matcherOpBefore(q string, i int) string {
	k := i - 1
	for k >= 0 && (q[k] == ' ' || q[k] == '\t' || q[k] == '\n') {
		k--
	}
	if k < 0 {
		return ""
	}
	prev := byte(0)
	if k > 0 {
		prev = q[k-1]
	}
	switch {
	case q[k] == '~' && (prev == '=' || prev == '!'):
		return string([]byte{prev, '~'})
	case q[k] == '=' && prev == '!':
		return "!="
	case q[k] == '=' && prev != '=' && prev != '<' && prev != '>':
		return "="
	}
	return ""
}

func identAt(q string, i int) string {
	j := i
	for j < len(q) {
		c := q[j]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (j > i && c >= '0' && c <= '9') {
			j++
			continue
		}
		break
	}
	return q[i:j]
}
