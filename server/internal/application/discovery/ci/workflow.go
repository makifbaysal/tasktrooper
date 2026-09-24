package ci

import (
	"path"
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"gopkg.in/yaml.v3"
)

// rawWorkflow is the subset of a GitHub Actions workflow file this package
// reads. `on` is a raw node because it is legally a string, a list or a map.
type rawWorkflow struct {
	Name     string            `yaml:"name"`
	On       yaml.Node         `yaml:"on"`
	Defaults rawDefaults       `yaml:"defaults"`
	Jobs     map[string]rawJob `yaml:"jobs"`
}

type rawDefaults struct {
	Run struct {
		WorkingDirectory string `yaml:"working-directory"`
	} `yaml:"run"`
}

type rawJob struct {
	Name        string      `yaml:"name"`
	Uses        string      `yaml:"uses"`
	Environment yaml.Node   `yaml:"environment"`
	Defaults    rawDefaults `yaml:"defaults"`
	Needs       yaml.Node   `yaml:"needs"`
	Steps       []rawStep   `yaml:"steps"`
}

type rawStep struct {
	Name             string            `yaml:"name"`
	Run              string            `yaml:"run"`
	Uses             string            `yaml:"uses"`
	WorkingDirectory string            `yaml:"working-directory"`
	With             map[string]string `yaml:"with"`
}

// workflow, job and step are the parsed model the rest of the package works
// from, freed of YAML's three shapes for `on` and `environment`.
type workflow struct {
	File         string
	Name         string
	Triggers     []string
	PathFilters  []string
	Dispatchable bool
	WorkingDir   string
	Jobs         []job
}

type job struct {
	Needs       []string
	Key         string
	Name        string
	Uses        string
	Environment string
	WorkingDir  string
	Steps       []step
}

type step struct {
	Name       string
	Run        string
	Uses       string
	WorkingDir string
	With       map[string]string
}

// parseWorkflows reads every .github/workflows/*.yml|.yaml directly under the
// workflows directory; one that fails to parse is reported as a warning and
// skipped rather than aborting the whole scan.
func parseWorkflows(tree *inventory.Tree) ([]workflow, []string) {
	var workflows []workflow
	var warnings []string
	for _, p := range workflowPaths(tree) {
		raw := tree.ReadString(p)
		var wf rawWorkflow
		if err := yaml.Unmarshal([]byte(raw), &wf); err != nil {
			warnings = append(warnings, p+" is not valid YAML: "+err.Error())
			continue
		}
		workflows = append(workflows, buildWorkflow(p, wf))
	}
	return workflows, warnings
}

func workflowPaths(tree *inventory.Tree) []string {
	const dir = ".github/workflows"
	const prefix = dir + "/"
	var out []string
	for _, f := range tree.Under(dir) {
		if strings.Contains(strings.TrimPrefix(f, prefix), "/") {
			continue // GitHub does not read workflow files in subdirectories
		}
		switch strings.ToLower(path.Ext(f)) {
		case ".yml", ".yaml":
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

func buildWorkflow(file string, wf rawWorkflow) workflow {
	w := workflow{File: file, Name: wf.Name, WorkingDir: wf.Defaults.Run.WorkingDirectory}
	w.Triggers, w.PathFilters, w.Dispatchable = parseOn(wf.On)

	keys := make([]string, 0, len(wf.Jobs))
	for k := range wf.Jobs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w.Jobs = append(w.Jobs, buildJob(k, wf.Jobs[k]))
	}
	return w
}

func buildJob(key string, raw rawJob) job {
	j := job{
		Key:        key,
		Name:       raw.Name,
		Uses:       raw.Uses,
		WorkingDir: raw.Defaults.Run.WorkingDirectory,
	}
	j.Environment = parseEnvironment(raw.Environment)
	j.Needs = parseNeeds(raw.Needs)
	for _, s := range raw.Steps {
		j.Steps = append(j.Steps, step{
			Name:       s.Name,
			Run:        s.Run,
			Uses:       s.Uses,
			WorkingDir: s.WorkingDirectory,
			With:       s.With,
		})
	}
	return j
}

func parseEnvironment(node yaml.Node) string {
	switch node.Kind {
	case yaml.ScalarNode:
		return node.Value
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == "name" {
				return node.Content[i+1].Value
			}
		}
	}
	return ""
}

// parseOn renders the `on:` node into event strings, keeping a branch filter
// attached ("push:main,develop") because "runs on push" and "runs on push to
// main" are different facts for the mapper's push-to-main deploy inference.
func parseOn(node yaml.Node) (triggers, pathFilters []string, dispatchable bool) {
	switch node.Kind {
	case yaml.ScalarNode:
		ev := node.Value
		if ev == "" {
			return nil, nil, false
		}
		return []string{ev}, nil, ev == "workflow_dispatch"
	case yaml.SequenceNode:
		for _, child := range node.Content {
			triggers = append(triggers, child.Value)
			if child.Value == "workflow_dispatch" {
				dispatchable = true
			}
		}
		sort.Strings(triggers)
		return triggers, nil, dispatchable
	case yaml.MappingNode:
		paths := map[string]bool{}
		for i := 0; i+1 < len(node.Content); i += 2 {
			event := node.Content[i].Value
			spec := node.Content[i+1]
			if event == "workflow_dispatch" {
				dispatchable = true
			}
			triggers = append(triggers, renderEvent(event, spec))
			collectPathFilters(event, spec, paths)
		}
		sort.Strings(triggers)
		for p := range paths {
			pathFilters = append(pathFilters, p)
		}
		sort.Strings(pathFilters)
		return triggers, pathFilters, dispatchable
	}
	return nil, nil, false
}

func renderEvent(event string, spec *yaml.Node) string {
	if spec == nil || spec.Kind != yaml.MappingNode {
		return event
	}
	if event != "push" && event != "pull_request" {
		return event
	}
	var branches []string
	hasTags := false
	for i := 0; i+1 < len(spec.Content); i += 2 {
		key := spec.Content[i].Value
		val := spec.Content[i+1]
		switch key {
		case "branches":
			if val.Kind == yaml.SequenceNode {
				for _, b := range val.Content {
					branches = append(branches, b.Value)
				}
			}
		case "tags":
			if val.Kind == yaml.SequenceNode && len(val.Content) > 0 {
				hasTags = true
			}
		}
	}
	if len(branches) > 0 {
		return event + ":" + strings.Join(branches, ",")
	}
	if hasTags {
		return event + ":tags"
	}
	return event
}

func collectPathFilters(event string, spec *yaml.Node, out map[string]bool) {
	if event != "push" && event != "pull_request" {
		return
	}
	if spec == nil || spec.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(spec.Content); i += 2 {
		if spec.Content[i].Value != "paths" {
			continue
		}
		val := spec.Content[i+1]
		if val.Kind != yaml.SequenceNode {
			continue
		}
		for _, c := range val.Content {
			out[c.Value] = true
		}
	}
}

func parseNeeds(node yaml.Node) []string {
	switch node.Kind {
	case yaml.ScalarNode:
		if v := strings.TrimSpace(node.Value); v != "" {
			return []string{v}
		}
	case yaml.SequenceNode:
		var out []string
		for _, n := range node.Content {
			if v := strings.TrimSpace(n.Value); v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	return nil
}
