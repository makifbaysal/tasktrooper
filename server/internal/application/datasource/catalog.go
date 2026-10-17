package datasource

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
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
		Better      string   `yaml:"better"`
	} `yaml:"signals"`
}

type recipeDoc struct {
	ID         string `yaml:"id"`
	SourceKind string `yaml:"source_kind"`
	AppliesTo  struct {
		Technology      string            `yaml:"technology"`
		MessagingSystem string            `yaml:"messaging_system"`
		ResourceKind    string            `yaml:"resource_kind"`
		ResourceParams  map[string]string `yaml:"resource_params"`
		StorePlatform   string            `yaml:"store_platform"`
		StoreParams     map[string]string `yaml:"store_params"`
	} `yaml:"applies_to"`
	Detect struct {
		Metric string `yaml:"metric"`
	} `yaml:"detect"`
	Params   []string            `yaml:"params"`
	Defaults map[string]string   `yaml:"defaults"`
	Signals  map[string]string   `yaml:"signals"`
	Tables   map[string]tableDoc `yaml:"tables"`
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
			Better:      domain.SignalDirection(s.Better),
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
		case !domain.ValidSignalDirection(sig.Better):
			return fmt.Errorf("%s: signal %q has better %q, not higher or lower", signalsFile, s.ID, s.Better)
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
	tables, err := decodeTables(doc.Tables)
	if err != nil {
		return domain.Recipe{}, err
	}
	return domain.Recipe{
		Tables:     tables,
		ID:         doc.ID,
		SourceKind: domain.DataSourceKind(doc.SourceKind),
		AppliesTo: domain.RecipeAppliesTo{
			Technology:      doc.AppliesTo.Technology,
			MessagingSystem: doc.AppliesTo.MessagingSystem,
			ResourceKind:    domain.CloudResourceKind(doc.AppliesTo.ResourceKind),
			ResourceParams:  doc.AppliesTo.ResourceParams,
			StorePlatform:   doc.AppliesTo.StorePlatform,
			StoreParams:     doc.AppliesTo.StoreParams,
		},
		Detect:   domain.RecipeDetect{Metric: doc.Detect.Metric},
		Params:   doc.Params,
		Defaults: doc.Defaults,
		Signals:  signals,
	}, nil
}

func (c *Catalog) Signals() []domain.Signal { return slices.Clone(c.signals) }

func (c *Catalog) Recipes() []domain.Recipe { return slices.Clone(c.recipes) }

// ValidateRecipe checks a recipe against the signal catalog, the kinds (the
// registered ones and the ones this build ships), and the placeholder rules
// of its kind's query language; every problem is reported, not just the
// first.
func (c *Catalog) ValidateRecipe(r domain.Recipe) error {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if !recipeIDPattern.MatchString(r.ID) {
		add("id %q must be lowercase letters, digits and dashes", r.ID)
	}
	if !slices.Contains(c.kinds, r.SourceKind) && !slices.Contains(domain.BuiltinDataSourceKinds(), r.SourceKind) {
		add("source_kind %q is not a registered data source kind", r.SourceKind)
	}
	lang := dialectFor(r.SourceKind)
	if r.Detect.Metric != "" && !lang.validMetricName(r.Detect.Metric) {
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
	problems = append(problems, checkResource(r.AppliesTo, declared)...)
	problems = append(problems, checkStoreApp(r.AppliesTo, declared)...)
	problems = append(problems, checkDefaults(r, lang, declared)...)
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
			continue
		}
		problems = append(problems, lang.check(fmt.Sprintf("signal %q", id), query, declared)...)
	}
	problems = append(problems, checkTables(r.Tables, lang, declared)...)
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func checkResource(a domain.RecipeAppliesTo, declared map[string]bool) []string {
	var problems []string
	if a.ResourceKind != "" && !domain.ValidCloudResourceKind(a.ResourceKind) {
		problems = append(problems, fmt.Sprintf("applies_to.resource_kind %q is not a cloud resource kind", a.ResourceKind))
	}
	if len(a.ResourceParams) > 0 && a.ResourceKind == "" {
		problems = append(problems, "applies_to.resource_params needs a resource_kind")
	}
	for _, param := range slices.Sorted(maps.Keys(a.ResourceParams)) {
		refPath := a.ResourceParams[param]
		if !declared[param] {
			problems = append(problems, fmt.Sprintf("applies_to.resource_params names %q, which is not a declared param", param))
		}
		if !domain.ValidResourceRefPath(refPath) {
			problems = append(problems, fmt.Sprintf("applies_to.resource_params.%s: %q is not id, name, region or extra.<key>", param, refPath))
		}
	}
	return problems
}
