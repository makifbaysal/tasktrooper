package designsystem

import "testing"

// TestDesignSystemToolDefinitionsCarryNoGoProse enforces the migration is
// actually done — see board/no_go_prose_test.go (WP8a).
func TestDesignSystemToolDefinitionsCarryNoGoProse(t *testing.T) {
	kit, _, _ := newTestKit(t)
	for _, ex := range NewExecutors(kit) {
		def := ex.Definition()
		if def.Function.Description != "" {
			t.Errorf("%s: Definition() still returns a Go-literal description: %q", ex.Name(), def.Function.Description)
		}
		assertNoParamProse(t, ex.Name(), def.Function.Parameters)
	}
}

func assertNoParamProse(t *testing.T, toolName string, node map[string]interface{}) {
	t.Helper()
	if node == nil {
		return
	}
	if desc, ok := node["description"].(string); ok && desc != "" {
		t.Errorf("%s: a parameter still carries a Go-literal description: %q", toolName, desc)
	}
	if items, ok := node["items"].(map[string]interface{}); ok {
		assertNoParamProse(t, toolName, items)
	}
	if props, ok := node["properties"].(map[string]interface{}); ok {
		for _, raw := range props {
			if sub, ok := raw.(map[string]interface{}); ok {
				assertNoParamProse(t, toolName, sub)
			}
		}
	}
}
