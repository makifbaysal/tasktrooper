package designsystem

import (
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
)

const goldenToolDefinitionsPath = "testdata/tool_definitions.golden.json"

// TestGoldenToolDefinitionsSurviveCatalogMigration pins the JSON this
// package's tools present to an LLM, decorated through registry.Register
// exactly the way platform/runtime wires the real registry — see
// board/golden_tool_definitions_test.go (WP8a) for the full rationale.
func TestGoldenToolDefinitionsSurviveCatalogMigration(t *testing.T) {
	kit, _, _ := newTestKit(t)
	reg := registry.New()
	for _, ex := range NewExecutors(kit) {
		reg.Register(ex)
	}

	defs := reg.Definitions()
	sort.Slice(defs, func(i, j int) bool { return defs[i].Function.Name < defs[j].Function.Name })

	got, err := json.MarshalIndent(defs, "", "  ")
	if err != nil {
		t.Fatalf("marshal definitions: %v", err)
	}
	got = append(got, '\n')

	want, err := os.ReadFile(goldenToolDefinitionsPath)
	if err != nil {
		t.Fatalf("read golden file %s: %v", goldenToolDefinitionsPath, err)
	}
	if string(got) != string(want) {
		t.Errorf("tool definitions changed byte-for-byte from %s\n--- want ---\n%s\n--- got ---\n%s",
			goldenToolDefinitionsPath, want, got)
	}
}
