// Package reviewhistory assembles a task's review story from the records the
// board already keeps, and derives the flags that need a person's eyes. It
// reads; the only thing it never does is ask a model.
package reviewhistory

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const rulesFile = "human_review_rules.yaml"

type SensitiveRule struct {
	ID    string   `yaml:"id"`
	Globs []string `yaml:"globs"`
	res   []*regexp.Regexp
}

type Rules struct {
	Sensitive []SensitiveRule `yaml:"sensitive_paths"`
	LargeDiff struct {
		MaxFiles int `yaml:"max_files"`
	} `yaml:"large_diff"`
	Coverage CoverageRules `yaml:"coverage"`
	Tests    struct {
		SourceExtensions []string `yaml:"source_extensions"`
		TestGlobs        []string `yaml:"test_globs"`
		ExemptGlobs      []string `yaml:"exempt_globs"`
		test, exempt     []*regexp.Regexp
	} `yaml:"tests"`
}

// LoadRules reads the rules file from fsys (catalog.ReviewFS()).
func LoadRules(fsys fs.FS) (*Rules, error) {
	raw, err := fs.ReadFile(fsys, rulesFile)
	if err != nil {
		return nil, fmt.Errorf("review rules: %w", err)
	}
	var r Rules
	if err := yaml.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("review rules: %w", err)
	}
	for i := range r.Sensitive {
		if r.Sensitive[i].ID == "" {
			return nil, fmt.Errorf("review rules: sensitive_paths[%d] has no id", i)
		}
		res, err := compileGlobs(r.Sensitive[i].Globs)
		if err != nil {
			return nil, fmt.Errorf("review rules: %s: %w", r.Sensitive[i].ID, err)
		}
		r.Sensitive[i].res = res
	}
	if r.Tests.test, err = compileGlobs(r.Tests.TestGlobs); err != nil {
		return nil, fmt.Errorf("review rules: tests.test_globs: %w", err)
	}
	if r.Tests.exempt, err = compileGlobs(r.Tests.ExemptGlobs); err != nil {
		return nil, fmt.Errorf("review rules: tests.exempt_globs: %w", err)
	}
	if err := r.Coverage.compile(); err != nil {
		return nil, fmt.Errorf("review rules: %w", err)
	}
	return &r, nil
}

func compileGlobs(globs []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(globs))
	for _, g := range globs {
		re, err := globToRegexp(strings.ToLower(g))
		if err != nil {
			return nil, err
		}
		out = append(out, re)
	}
	return out, nil
}

func globToRegexp(glob string) (*regexp.Regexp, error) {
	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(glob); i++ {
		switch c := glob[i]; c {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				i++
				if i+1 < len(glob) && glob[i+1] == '/' {
					i++
					sb.WriteString("(?:.*/)?")
				} else {
					sb.WriteString(".*")
				}
			} else {
				sb.WriteString("[^/]*")
			}
		case '?':
			sb.WriteString("[^/]")
		default:
			sb.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	sb.WriteString("$")
	return regexp.Compile(sb.String())
}

func matchAny(res []*regexp.Regexp, p string) bool {
	for _, re := range res {
		if re.MatchString(p) {
			return true
		}
	}
	return false
}

func cleanPath(p string) string {
	return strings.ToLower(strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(p), "\\", "/"), "./"))
}

// SensitiveMatches groups the files that match a sensitive rule by rule id.
func (r *Rules) SensitiveMatches(files []string) map[string][]string {
	out := map[string][]string{}
	for _, f := range files {
		lower := cleanPath(f)
		for _, rule := range r.Sensitive {
			if matchAny(rule.res, lower) {
				out[rule.ID] = append(out[rule.ID], f)
			}
		}
	}
	return out
}

func (r *Rules) isTestFile(f string) bool { return matchAny(r.Tests.test, cleanPath(f)) }

// SourceNeedingTests are the changed files a test is expected to accompany.
func (r *Rules) SourceNeedingTests(files []string) (source []string, anyTest bool) {
	for _, f := range files {
		lower := cleanPath(f)
		if r.isTestFile(f) {
			anyTest = true
			continue
		}
		if matchAny(r.Tests.exempt, lower) {
			continue
		}
		ext := strings.ToLower(path.Ext(lower))
		for _, want := range r.Tests.SourceExtensions {
			if ext == want {
				source = append(source, f)
				break
			}
		}
	}
	return source, anyTest
}

type AspectSkill struct {
	Match  string `yaml:"match"`
	Aspect string `yaml:"aspect"`
}

type ReviewerRole struct {
	Match          []string `yaml:"match"`
	Aspect         string   `yaml:"aspect"`
	OutOfRoleGlobs []string `yaml:"out_of_role_globs"`
	outOfRole      []*regexp.Regexp
}

type CoverageRules struct {
	SkippedGlobs   []string       `yaml:"skipped_globs"`
	LargeFileLines int            `yaml:"large_file_lines"`
	AspectSkills   []AspectSkill  `yaml:"aspect_skills"`
	ReviewerRoles  []ReviewerRole `yaml:"reviewer_roles"`
	skipped        []*regexp.Regexp
}

func (c *CoverageRules) compile() error {
	var err error
	if c.skipped, err = compileGlobs(c.SkippedGlobs); err != nil {
		return fmt.Errorf("coverage.skipped_globs: %w", err)
	}
	for i := range c.ReviewerRoles {
		if c.ReviewerRoles[i].outOfRole, err = compileGlobs(c.ReviewerRoles[i].OutOfRoleGlobs); err != nil {
			return fmt.Errorf("coverage.reviewer_roles[%d]: %w", i, err)
		}
	}
	return nil
}

func (r *Rules) isSkipped(f string) bool { return matchAny(r.Coverage.skipped, cleanPath(f)) }

// roleOf is the reviewer role a name falls under, nil for a general reviewer.
func (r *Rules) roleOf(agentName string) *ReviewerRole {
	lower := strings.ToLower(agentName)
	for i := range r.Coverage.ReviewerRoles {
		for _, m := range r.Coverage.ReviewerRoles[i].Match {
			if strings.Contains(lower, strings.ToLower(m)) {
				return &r.Coverage.ReviewerRoles[i]
			}
		}
	}
	return nil
}

func (r *Rules) aspectOfSkill(skill string) string {
	lower := strings.ToLower(skill)
	for _, a := range r.Coverage.AspectSkills {
		if strings.Contains(lower, strings.ToLower(a.Match)) {
			return a.Aspect
		}
	}
	return ""
}
