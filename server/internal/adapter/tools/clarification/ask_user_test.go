package clarification_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/clarification"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ask_user's Definition() itself carries no prose any more — the function
// description and every parameter description come from
// catalog/system/tools/ask_user.md via registry.Register (see
// internal/application/registry/tooldocs.go). This is the contract check
// that used to run against the raw Definition(); it now runs against the
// decorated one, the shape every real caller sees.
func TestAskUserTool_DefinitionIncludesContract(t *testing.T) {
	reg := registry.New()
	reg.Register(clarification.NewAskUserTool())
	def := reg.Definitions()[0]
	assert.Contains(t, def.Function.Description, "free_text")
	assert.Contains(t, def.Function.Description, "other")
}

func TestAskUserTool_RawDefinitionCarriesNoGoProse(t *testing.T) {
	tool := clarification.NewAskUserTool()
	def := tool.Definition()
	assert.Empty(t, def.Function.Description,
		"ask_user's description belongs in catalog/system/tools/ask_user.md, not in Go")
	assertNoParamProse(t, def.Function.Parameters)
}

func assertNoParamProse(t *testing.T, node map[string]interface{}) {
	t.Helper()
	if node == nil {
		return
	}
	if desc, ok := node["description"].(string); ok {
		assert.Empty(t, desc, "a parameter still carries a Go-literal description")
	}
	if items, ok := node["items"].(map[string]interface{}); ok {
		assertNoParamProse(t, items)
	}
	if props, ok := node["properties"].(map[string]interface{}); ok {
		for _, raw := range props {
			if sub, ok := raw.(map[string]interface{}); ok {
				assertNoParamProse(t, sub)
			}
		}
	}
}

func TestAskUserTool_ValidChoiceMode(t *testing.T) {
	tool := clarification.NewAskUserTool()
	raw := `{"context":"Need scope","questions":[{"id":"q1","prompt":"Which area?","options":[{"id":"auth","label":"Auth"},{"id":"other","label":"Diğer"}]}]}`
	result := tool.Execute(t.Context(), raw)
	require.False(t, result.IsError)
	require.NotNil(t, result.Clarification)
	assert.Len(t, result.Clarification.Questions, 1)
}

func TestAskUserTool_ValidTextMode(t *testing.T) {
	tool := clarification.NewAskUserTool()
	raw := `{"context":"Need URL","questions":[{"id":"url","prompt":"Profile URL?","options":[{"id":"free_text","label":"Yanıtımı yazacağım"},{"id":"skip","label":"Sonra"}]}]}`
	result := tool.Execute(t.Context(), raw)
	require.False(t, result.IsError)
}

func TestAskUserTool_RejectsMixedMode(t *testing.T) {
	tool := clarification.NewAskUserTool()
	raw := `{"context":"x","questions":[{"id":"q1","prompt":"?","options":[{"id":"free_text","label":"Text"},{"id":"a","label":"A"}]}]}`
	result := tool.Execute(t.Context(), raw)
	assert.True(t, result.IsError)
}

func TestAskUserTool_RejectsChoiceWithoutOtherLast(t *testing.T) {
	tool := clarification.NewAskUserTool()
	raw := `{"context":"x","questions":[{"id":"q1","prompt":"?","options":[{"id":"a","label":"Auth"}]}]}`
	result := tool.Execute(t.Context(), raw)
	require.False(t, result.IsError)
	require.NotNil(t, result.Clarification)
	assert.Equal(t, "other", result.Clarification.Questions[0].Options[len(result.Clarification.Questions[0].Options)-1].ID)
}

func TestAskUserTool_AcceptsAllowMultiple(t *testing.T) {
	tool := clarification.NewAskUserTool()
	raw := `{"context":"x","questions":[{"id":"q1","prompt":"Sections?","allow_multiple":true,"options":[{"id":"home","label":"Home"},{"id":"other","label":"Other"}]}]}`
	result := tool.Execute(t.Context(), raw)
	require.False(t, result.IsError)
	assert.True(t, result.Clarification.Questions[0].AllowMultiple)
}

func TestAskUserTool_InvalidQuestions(t *testing.T) {
	tool := clarification.NewAskUserTool()
	raw := `{"context":"x","questions":[{"id":"q1","prompt":"?","options":[]}]}`
	result := tool.Execute(t.Context(), raw)
	assert.True(t, result.IsError)
}

func TestAskUserTool_KeepsMaxSelectionsOnMultiSelect(t *testing.T) {
	tool := clarification.NewAskUserTool()
	raw := `{"context":"x","questions":[{"id":"q1","prompt":"Where?","allow_multiple":true,"max_selections":3,"options":[{"id":"us","label":"US"},{"id":"uk","label":"UK"},{"id":"other","label":"Other"}]}]}`
	result := tool.Execute(t.Context(), raw)
	require.False(t, result.IsError)
	assert.Equal(t, 3, result.Clarification.Questions[0].MaxSelections)
}

func TestAskUserTool_DropsMaxSelectionsOnSingleSelect(t *testing.T) {
	tool := clarification.NewAskUserTool()
	raw := `{"context":"x","questions":[{"id":"q1","prompt":"Where?","max_selections":3,"options":[{"id":"us","label":"US"},{"id":"other","label":"Other"}]}]}`
	result := tool.Execute(t.Context(), raw)
	require.False(t, result.IsError)
	assert.Zero(t, result.Clarification.Questions[0].MaxSelections)
}
