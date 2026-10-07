package components

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func regexpScriptLine(packageJSON, name string) int {
	loc := regexp.MustCompile(`"` + regexp.QuoteMeta(name) + `"\s*:`).FindStringIndex(packageJSON)
	if loc == nil {
		return 0
	}
	return lineOf(packageJSON, loc[0])
}

func TestScriptLineMatchesTheKeyPatternItReplaced(t *testing.T) {
	tests := []struct {
		name        string
		packageJSON string
		script      string
		want        int
	}{
		{
			name:        "name absent",
			packageJSON: "{\n  \"scripts\": {\n    \"build\": \"tsc\"\n  }\n}",
			script:      "test",
			want:        0,
		},
		{
			name:        "name present only as a value",
			packageJSON: "{\n  \"scripts\": {\n    \"ci\": \"lint\"\n  }\n}",
			script:      "lint",
			want:        0,
		},
		{
			name:        "colon inside the name",
			packageJSON: "{\n  \"scripts\": {\n    \"test\": \"jest\",\n    \"test:unit\": \"jest unit\"\n  }\n}",
			script:      "test:unit",
			want:        4,
		},
		{
			name:        "dot in the name is literal",
			packageJSON: "{\n  \"aXb\": \"no\",\n  \"a.b\": \"yes\"\n}",
			script:      "a.b",
			want:        3,
		},
		{
			name:        "plus in the name is literal",
			packageJSON: "{\n  \"xxy\": \"no\",\n  \"x+y\": \"yes\"\n}",
			script:      "x+y",
			want:        3,
		},
		{
			name:        "spaces and tabs before the colon",
			packageJSON: "{\n  \"lint\" \t : \"eslint\"\n}",
			script:      "lint",
			want:        2,
		},
		{
			name:        "newline before the colon reports the key's line",
			packageJSON: "{\n  \"lint\"\r\n\f\n  : \"eslint\"\n}",
			script:      "lint",
			want:        2,
		},
		{
			name:        "vertical tab is not whitespace",
			packageJSON: "{\n  \"lint\"\v: \"eslint\"\n}",
			script:      "lint",
			want:        0,
		},
		{
			name:        "value occurrence before the real key",
			packageJSON: "{\n  \"scripts\": {\n    \"ci\": \"build\",\n    \"build\": \"tsc\"\n  }\n}",
			script:      "build",
			want:        4,
		},
		{
			name:        "overlapping candidates",
			packageJSON: "{\n  \"\"\"\": 1\n}",
			script:      `"`,
			want:        2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, regexpScriptLine(tt.packageJSON, tt.script), "fixture disagrees with the regexp")
			require.Equal(t, tt.want, scriptLine(tt.packageJSON, tt.script))
		})
	}
}
