package domain

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeDesignTokensLayerOverridesTokenWholeAndMergesGroups(t *testing.T) {
	base := json.RawMessage(`{
		"color": {
			"primary": {"$value": "#15214b", "$type": "color"},
			"surface": {"$value": "#ffffff", "$type": "color"}
		},
		"radius": {"base": {"$value": "0.625rem"}}
	}`)
	layer := json.RawMessage(`{
		"color": {
			"primary": {"$value": "#f0b86e"},
			"accent": {"$value": "#f0b86e"}
		}
	}`)
	merged, err := MergeDesignTokens(base, layer)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"color": {
			"primary": {"$value": "#f0b86e"},
			"surface": {"$value": "#ffffff", "$type": "color"},
			"accent": {"$value": "#f0b86e"}
		},
		"radius": {"base": {"$value": "0.625rem"}}
	}`, string(merged))
}

func TestMergeDesignTokensWithEmptySides(t *testing.T) {
	merged, err := MergeDesignTokens(nil, json.RawMessage(`{"a":{"$value":1}}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"a":{"$value":1}}`, string(merged))

	merged, err = MergeDesignTokens(json.RawMessage(`{"a":{"$value":1}}`), nil)
	require.NoError(t, err)
	assert.JSONEq(t, `{"a":{"$value":1}}`, string(merged))
}

func TestCheckLayerTokensRefusesRemoval(t *testing.T) {
	err := CheckLayerTokens(json.RawMessage(`{"color":{"primary":null}}`))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDesignSystemInvalid))
	assert.Contains(t, err.Error(), "color.primary")

	assert.NoError(t, CheckLayerTokens(json.RawMessage(`{"color":{"primary":{"$value":"#000"}}}`)))
}

func TestParseDesignTokens(t *testing.T) {
	got, err := ParseDesignTokens(nil)
	require.NoError(t, err)
	assert.Equal(t, `{}`, string(got))

	got, err = ParseDesignTokens(json.RawMessage(" {\n \"a\": {\"$value\": 1} } "))
	require.NoError(t, err)
	assert.Equal(t, `{"a":{"$value":1}}`, string(got))

	_, err = ParseDesignTokens(json.RawMessage(`[1,2]`))
	assert.ErrorIs(t, err, ErrDesignSystemInvalid)
}

func TestOverriddenTokenPaths(t *testing.T) {
	paths, err := OverriddenTokenPaths(
		json.RawMessage(`{"color":{"primary":{"$value":"#000"},"surface":{"$value":"#fff"}},"space":{"sm":{"$value":"4px"}}}`),
		json.RawMessage(`{"color":{"primary":{"$value":"#111"},"accent":{"$value":"#f0b86e"}},"space":{"sm":{"$value":"8px"}}}`),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"color.primary", "space.sm"}, paths)
}

func TestDesignTaskTypeIsDocumentWork(t *testing.T) {
	assert.True(t, TaskTypeDesign.IsDocumentWork())
	assert.True(t, TaskTypeAnaliz.IsDocumentWork())
	assert.False(t, TaskTypeTask.IsDocumentWork())
	assert.False(t, TaskTypeDesign.PublishesBranch())
	assert.True(t, TaskTypeBug.PublishesBranch())
	assert.Equal(t, "D-7", FormatTaskKey(TaskTypeDesign, 7))
}
