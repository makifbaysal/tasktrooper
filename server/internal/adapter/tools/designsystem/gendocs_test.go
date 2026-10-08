package designsystem

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// TestGenerateToolDocs is WP8b's migration script for this package — see
// board/gendocs_test.go for the full explanation of what it does and
// why it is gated behind GENERATE_TOOL_DOCS and never runs in CI.
func TestGenerateToolDocs(t *testing.T) {
	if os.Getenv("GENERATE_TOOL_DOCS") == "" {
		t.Skip("set GENERATE_TOOL_DOCS=1 to (re)generate catalog/system/tools/*.md from current Go prose")
	}

	kit, _, _ := newTestKit(t)
	dir := repoToolsDir(t)
	execs := NewExecutors(kit)

	for _, ex := range execs {
		writeToolDoc(t, dir, ex.Definition())
	}

	writeGoldenToolDefinitions(t, execs)
}

func writeGoldenToolDefinitions(t *testing.T, execs []port.ToolExecutor) {
	t.Helper()
	reg := registry.New()
	for _, ex := range execs {
		reg.Register(ex)
	}
	defs := reg.Definitions()
	sort.Slice(defs, func(i, j int) bool { return defs[i].Function.Name < defs[j].Function.Name })

	out, err := json.MarshalIndent(defs, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden definitions: %v", err)
	}
	out = append(out, '\n')
	if err := os.WriteFile(goldenToolDefinitionsPath, out, 0o644); err != nil {
		t.Fatalf("write %s: %v", goldenToolDefinitionsPath, err)
	}
}

func repoToolsDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(wd, "..", "..", "..", "..", "..", "catalog", "system", "tools")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("catalog/system/tools not found at %s: %v", dir, err)
	}
	return dir
}

type toolFrontMatter struct {
	Key     string            `yaml:"key"`
	Version string            `yaml:"version"`
	Params  map[string]string `yaml:"params,omitempty"`
}

func writeToolDoc(t *testing.T, dir string, def domain.ToolDefinition) {
	t.Helper()
	name := def.Function.Name
	if name == "" {
		t.Fatalf("tool definition has no name: %+v", def)
	}
	if def.Function.Description == "" {
		return
	}

	params := map[string]string{}
	collectParamDescriptions(def.Function.Parameters, params)

	fm := toolFrontMatter{Key: "tool." + name, Version: "1", Params: params}
	fmBytes, err := yaml.Marshal(fm)
	if err != nil {
		t.Fatalf("%s: marshal front matter: %v", name, err)
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.Write(fmBytes)
	b.WriteString("---\n")
	b.WriteString(def.Function.Description)
	b.WriteString("\n")

	path := filepath.Join(dir, name+".md")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("%s: write %s: %v", name, path, err)
	}
}

func collectParamDescriptions(parameters map[string]interface{}, out map[string]string) {
	props, ok := parameters["properties"].(map[string]interface{})
	if !ok {
		return
	}
	for _, name := range sortedKeys(props) {
		node, _ := props[name].(map[string]interface{})
		walkParamNode(node, name, out)
	}
}

func walkParamNode(node map[string]interface{}, path string, out map[string]string) {
	if node == nil {
		return
	}
	if desc, ok := node["description"].(string); ok && desc != "" {
		out[path] = desc
	}
	if items, ok := node["items"].(map[string]interface{}); ok {
		walkParamNode(items, path+".items", out)
	}
	if props, ok := node["properties"].(map[string]interface{}); ok {
		for _, name := range sortedKeys(props) {
			sub, _ := props[name].(map[string]interface{})
			walkParamNode(sub, path+".properties."+name, out)
		}
	}
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
