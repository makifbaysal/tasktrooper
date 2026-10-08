package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lintCodes(findings []DesignLintFinding) map[string][]DesignLintFinding {
	out := map[string][]DesignLintFinding{}
	for _, f := range findings {
		out[f.Code] = append(out[f.Code], f)
	}
	return out
}

func TestLintFindsContrastBelowAAForAForegroundPair(t *testing.T) {
	findings := LintDesignSystem(DesignSystemScopeRepository, "", json.RawMessage(`{
		"color": {
			"$type": "color",
			"primary": {"$value": "#15214b"},
			"primary-foreground": {"$value": "#ffffff"},
			"accent": {"$value": "#f0b86e"},
			"on-accent": {"$value": "#ffffff"},
			"surface": {"$value": "oklch(0.985 0 0)"},
			"onSurface": {"$value": "{color.primary}"}
		}
	}`))
	codes := lintCodes(findings)
	require.Len(t, codes[DesignLintContrastBelowAA], 1, "%+v", findings)
	got := codes[DesignLintContrastBelowAA][0]
	assert.Equal(t, "color.accent", got.Path)
	assert.Equal(t, "color.on-accent", got.RelatedPath)
	assert.InDelta(t, 1.82, got.Ratio, 0.05)
	assert.Equal(t, DesignLintError, got.Severity)
}

func TestLintFindsBrokenAliasesColorsAndEmptyGroups(t *testing.T) {
	findings := LintDesignSystem(DesignSystemScopeRepository, "", json.RawMessage(`{
		"color": {
			"brand": {"$value": "{color.missing}", "$type": "color"},
			"muddy": {"$value": "not-a-color", "$type": "color"},
			"dtcg": {"$value": {"colorSpace": "srgb", "components": [0.08, 0.13, 0.29]}, "$type": "color"}
		},
		"space": {}
	}`))
	codes := lintCodes(findings)
	require.Len(t, codes[DesignLintUnresolvedAlias], 1)
	assert.Equal(t, "color.brand", codes[DesignLintUnresolvedAlias][0].Path)
	require.Len(t, codes[DesignLintInvalidColor], 1)
	assert.Equal(t, "not-a-color", codes[DesignLintInvalidColor][0].Value)
	require.Len(t, codes[DesignLintEmptyGroup], 1)
	assert.Equal(t, "space", codes[DesignLintEmptyGroup][0].Path)
}

func TestLintChecksTheBaseDesignMDSections(t *testing.T) {
	findings := LintDesignSystem(DesignSystemScopeProject, "## Overview\n## Colors\n## Typography\n## Components\n", json.RawMessage(`{}`))
	codes := lintCodes(findings)
	require.Len(t, codes[DesignLintMissingSection], 1)
	assert.Equal(t, "Do's and Don'ts", codes[DesignLintMissingSection][0].Value)

	assert.Empty(t, LintDesignSystem(DesignSystemScopeRepository, "", json.RawMessage(`{}`)), "a layer needs no sections")
}

func TestParseColorForms(t *testing.T) {
	for _, in := range []any{
		"#fff", "#15214b", "#15214bcc", "rgb(21, 33, 75)", "rgba(21 33 75 / 0.5)", "hsl(227deg 56% 19%)",
		"oklch(0.264 0.079 268.5)", "oklch(26.4% 0.079 268.5)", "oklab(0.5 0.01 -0.05)",
		map[string]any{"colorSpace": "srgb", "components": []any{0.1, 0.2, 0.3}},
		map[string]any{"hex": "#ffffff"},
	} {
		_, ok := parseColor(in)
		assert.True(t, ok, "%v", in)
	}
	navy, ok := parseColor("oklch(0.264 0.079 268.5)")
	require.True(t, ok)
	hex, _ := parseColor("#15214b")
	assert.InDelta(t, hex.r, navy.r, 0.02, "the web primary's oklch and hex agree")
	assert.InDelta(t, hex.b, navy.b, 0.02)
	assert.InDelta(t, 21.0, contrastRatio(rgb{0, 0, 0}, rgb{1, 1, 1}), 0.01)
}

func TestDesignTokensCSS(t *testing.T) {
	css, err := DesignTokensCSS(json.RawMessage(`{
		"color": {"primary": {"$value": "#15214b"}, "primaryHover": {"$value": "{color.primary}"}},
		"space": {"sm": {"$value": {"value": 8, "unit": "px"}}},
		"z": {"modal": {"$value": 40}},
		"typography": {"body": {"$value": {"fontFamily": "Inter", "fontSize": {"value": 14, "unit": "px"}}}},
		"shadow": {"raised": {"$value": [{"offsetX": "0"}]}}
	}`))
	require.NoError(t, err)
	assert.Equal(t, `:root {
  --color-primary: #15214b;
  --color-primary-hover: var(--color-primary);
  --space-sm: 8px;
  --typography-body-font-family: Inter;
  --typography-body-font-size: 14px;
  --z-modal: 40;
}
`, css)
}
