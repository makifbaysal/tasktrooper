package executor

import (
	"bufio"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validToken = "0123456789abcdef0123"

func configLine(fields string) string {
	return fmt.Sprintf(`{"token":%q,"workspace_root":"/tmp/ws"%s}`, validToken, fields)
}

func TestParseConfigDefaultsAndNormalises(t *testing.T) {
	cfg, err := ParseConfig([]byte(configLine(`,"listen":"localhost:0","data_dir":"rel/data",
		"providers":[{"id":" main ","type":"anthropic","api_key":"sk-ant-secret-value","models":["claude-sonnet-4-5"]}]`)))

	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:0", cfg.Listen)
	assert.True(t, filepath.IsAbs(cfg.DataDir))
	assert.Equal(t, "main", cfg.Providers[0].ID)

	defaulted, err := ParseConfig([]byte(configLine("")))
	require.NoError(t, err)
	assert.Equal(t, defaultListen, defaulted.Listen)
}

func TestParseConfigRejects(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"not json", "token=abc"},
		{"short token", `{"token":"short","workspace_root":"/tmp/ws"}`},
		{"no workspace root", fmt.Sprintf(`{"token":%q}`, validToken)},
		{"wildcard listen", configLine(`,"listen":"0.0.0.0:0"`)},
		{"lan listen", configLine(`,"listen":"192.168.1.20:7000"`)},
		{"listen without port", configLine(`,"listen":"127.0.0.1"`)},
		{"provider without id", configLine(`,"providers":[{"type":"openai"}]`)},
		{"duplicate provider", configLine(`,"providers":[{"id":"a","type":"openai"},{"id":"a","type":"groq"}]`)},
		{"provider without type", configLine(`,"providers":[{"id":"a"}]`)},
		{"unknown type", configLine(`,"providers":[{"id":"a","type":"mystery"}]`)},
		{"agent cli type", configLine(`,"providers":[{"id":"a","type":"claude_code"}]`)},
		{"endpoint without base url", configLine(`,"providers":[{"id":"a","type":"openai_compatible"}]`)},
		{"negative timeout", configLine(`,"providers":[{"id":"a","type":"openai","timeout_seconds":-1}]`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseConfig([]byte(tt.line))

			assert.Error(t, err)
		})
	}
}

func TestReadConfigTakesTheFirstLine(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"line then more", configLine("") + "\nignored\n"},
		{"line then eof", configLine("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ReadConfig(bufio.NewReaderSize(strings.NewReader(tt.input), 16))

			require.NoError(t, err)
			assert.Equal(t, validToken, cfg.Token)
		})
	}

	_, err := ReadConfig(bufio.NewReader(strings.NewReader("\n")))
	assert.Error(t, err)
}

func TestConfigNeverPrintsAKey(t *testing.T) {
	cfg, err := ParseConfig([]byte(configLine(`,"providers":[{"id":"main","type":"openai","api_key":"sk-very-secret-value"}]`)))
	require.NoError(t, err)

	for _, printed := range []string{fmt.Sprint(cfg), fmt.Sprintf("%v", cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg), fmt.Sprint(cfg.Providers)} {
		assert.NotContains(t, printed, "sk-very-secret-value")
		assert.NotContains(t, printed, validToken)
	}
}
