package datasource

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// tableColumnPattern is a label name in PromQL and a dimension name without
// spaces in CloudWatch: a key column's key is the label it reads.
var tableColumnPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var knownTables = []string{
	domain.RecipeTableQueues, domain.RecipeTableCrashes, domain.RecipeTableSignatures,
	domain.RecipeTablePages, domain.RecipeTableReferrers, domain.RecipeTableCountries, domain.RecipeTableDevices,
	domain.RecipeTableEndpoints,
}

// namedColumnTables are read from kinds whose Table answers every column by
// name (an issue's cause, users, reports; a page's views; a transaction's
// rpm, error rate and p95), so one query may fill them all.
var namedColumnTables = map[string]bool{
	domain.RecipeTableCrashes: true, domain.RecipeTableSignatures: true,
	domain.RecipeTablePages: true, domain.RecipeTableReferrers: true, domain.RecipeTableCountries: true,
	domain.RecipeTableDevices: true, domain.RecipeTableEndpoints: true,
}

type tableDoc struct {
	Columns []struct {
		Key   string `yaml:"key"`
		Label string `yaml:"label"`
		Unit  string `yaml:"unit"`
	} `yaml:"columns"`
	KeyLabels []string          `yaml:"key_labels"`
	Query     string            `yaml:"query"`
	Queries   map[string]string `yaml:"queries"`
}

// decodeTables: query is the short form of queries for a table with one
// value column, or for a named-column table the one query of all of them.
func decodeTables(docs map[string]tableDoc) (map[string]domain.RecipeTable, error) {
	if len(docs) == 0 {
		return nil, nil
	}
	out := make(map[string]domain.RecipeTable, len(docs))
	for _, name := range slices.Sorted(maps.Keys(docs)) {
		doc := docs[name]
		t := domain.RecipeTable{KeyLabels: doc.KeyLabels, Queries: map[string]string{}}
		for _, c := range doc.Columns {
			t.Columns = append(t.Columns, domain.TableColumn{Key: c.Key, Label: c.Label, Unit: c.Unit})
		}
		for col, q := range doc.Queries {
			t.Queries[col] = strings.TrimSpace(q)
		}
		if q := strings.TrimSpace(doc.Query); q != "" {
			values := t.ValueColumns()
			switch {
			case len(doc.Queries) > 0:
				return nil, fmt.Errorf("tables.%s: give query or queries, not both", name)
			case len(values) != 1 && !namedColumnTables[name]:
				return nil, fmt.Errorf("tables.%s: query fills one value column, and %d columns are not key labels; use queries", name, len(values))
			}
			for _, c := range values {
				t.Queries[c.Key] = q
			}
		}
		out[name] = t
	}
	return out, nil
}

// checkTables holds a table's queries to the same placeholder rules as a
// signal's, and its columns to one query per value column.
func checkTables(tables map[string]domain.RecipeTable, lang dialect, declared map[string]bool) []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	for _, name := range slices.Sorted(maps.Keys(tables)) {
		t := tables[name]
		if !slices.Contains(knownTables, name) {
			add("tables.%s is not a known table (%s)", name, strings.Join(knownTables, ", "))
		}
		if len(t.Columns) == 0 {
			add("tables.%s has no columns", name)
		}
		seen := map[string]bool{}
		for _, c := range t.Columns {
			switch {
			case !tableColumnPattern.MatchString(c.Key):
				add("tables.%s: column key %q must be letters, digits and underscores", name, c.Key)
			case seen[c.Key]:
				add("tables.%s: column %q is listed twice", name, c.Key)
			case strings.TrimSpace(c.Label) == "":
				add("tables.%s: column %q has no label", name, c.Key)
			}
			seen[c.Key] = true
		}
		if len(t.KeyLabels) == 0 {
			add("tables.%s has no key_labels", name)
		}
		keys := map[string]bool{}
		for _, k := range t.KeyLabels {
			switch {
			case keys[k]:
				add("tables.%s: key label %q is listed twice", name, k)
			case !seen[k]:
				add("tables.%s: key label %q is not a column", name, k)
			}
			keys[k] = true
		}
		values := t.ValueColumns()
		if len(t.Columns) > 0 && len(values) == 0 {
			add("tables.%s has no value column", name)
		}
		for _, c := range values {
			q, ok := t.Queries[c.Key]
			if !ok || strings.TrimSpace(q) == "" {
				add("tables.%s: column %q has no query", name, c.Key)
				continue
			}
			problems = append(problems, lang.check(fmt.Sprintf("table %q column %q", name, c.Key), q, declared)...)
		}
		for _, col := range slices.Sorted(maps.Keys(t.Queries)) {
			if !seen[col] || t.IsKey(col) {
				add("tables.%s: queries.%s is not a value column", name, col)
			}
		}
	}
	return problems
}

// TableParams are the params of r its table name references, sorted: what
// two bindings of r must agree on to read the same table.
func TableParams(r domain.Recipe, name string) []string {
	var out []string
	for _, q := range r.Tables[name].Queries {
		for _, m := range placeholderRegex.FindAllStringSubmatch(q, -1) {
			if p := m[1]; slices.Contains(r.Params, p) && !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	slices.Sort(out)
	return out
}
