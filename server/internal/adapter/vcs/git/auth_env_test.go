package git

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testToken = "ghp_test-token-stays-off-argv"

func basicToken(token string) string {
	return base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
}

type recordedCommand struct {
	argv []string
	env  []string
}

func (c recordedCommand) envValue(key string) (string, bool) {
	prefix := key + "="
	for i := len(c.env) - 1; i >= 0; i-- {
		if value, ok := strings.CutPrefix(c.env[i], prefix); ok {
			return value, true
		}
	}
	return "", false
}

type commandRecorder struct {
	cmds []recordedCommand
}

func recordCommands(c *Client) *commandRecorder {
	r := &commandRecorder{}
	c.observe = func(cmd *exec.Cmd) {
		r.cmds = append(r.cmds, recordedCommand{
			argv: append([]string(nil), cmd.Args...),
			env:  append([]string(nil), cmd.Env...),
		})
	}
	return r
}

func (r *commandRecorder) withVerb(verb string) *recordedCommand {
	for i := range r.cmds {
		for _, arg := range r.cmds[i].argv {
			if arg == verb {
				return &r.cmds[i]
			}
		}
	}
	return nil
}

func TestAuthEnvPutsAGitConfigHeaderInTheEnvironment(t *testing.T) {
	c := NewClient()
	c.SetTokenSource(func(context.Context) (string, error) { return testToken, nil })

	env := c.authEnv(context.Background())
	require.Equal(t, []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
		"GIT_CONFIG_VALUE_0=AUTHORIZATION: basic " + basicToken(testToken),
	}, env)

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(env[2], "GIT_CONFIG_VALUE_0=AUTHORIZATION: basic "))
	require.NoError(t, err)
	assert.Equal(t, "x-access-token:"+testToken, string(decoded))
}

func TestAuthEnvIsAbsentWithoutAToken(t *testing.T) {
	c := NewClient()
	assert.Nil(t, c.authEnv(context.Background()))
}

func TestGitAuthReachesTheChildOnlyThroughItsEnvironment(t *testing.T) {
	header := "AUTHORIZATION: basic " + basicToken(testToken)

	tests := []struct {
		name string
		verb string
		run  func(t *testing.T, c *Client, root string) error
	}{
		{
			name: "clone",
			verb: "clone",
			run: func(t *testing.T, c *Client, root string) error {
				origin := filepath.Join(filepath.Dir(root), "origin.git")
				return c.CloneRepo(context.Background(), origin, filepath.Join(t.TempDir(), "widgets"))
			},
		},
		{
			name: "fetch",
			verb: "fetch",
			run: func(_ *testing.T, c *Client, root string) error {
				return c.FetchLatest(context.Background(), root)
			},
		},
		{
			name: "fetch tags",
			verb: "fetch",
			run: func(_ *testing.T, c *Client, root string) error {
				_, err := c.LatestTag(context.Background(), root, "*")
				return err
			},
		},
		{
			name: "push branch",
			verb: "push",
			run: func(_ *testing.T, c *Client, root string) error {
				return c.PushBranch(context.Background(), root)
			},
		},
		{
			name: "commit and push",
			verb: "push",
			run: func(t *testing.T, c *Client, root string) error {
				if err := os.WriteFile(filepath.Join(root, "feature.go"), []byte("package main\n"), 0o644); err != nil {
					return err
				}
				return c.CommitAndPush(context.Background(), root, "feat: a change")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, _ := newSyncFixture(t)

			c := NewClient()
			c.SetTokenSource(func(context.Context) (string, error) { return testToken, nil })
			// Pre-seeded so CommitAndPush attributes the commit without a real
			// /user round-trip.
			c.identityToken = testToken
			c.identity = githubapi.Identity{Login: "tester", ID: 42}
			c.identityAt = time.Now()

			recorder := recordCommands(c)
			require.NoError(t, tt.run(t, c, root))
			require.NotEmpty(t, recorder.cmds)

			auth := recorder.withVerb(tt.verb)
			require.NotNil(t, auth, "no %s command was recorded", tt.verb)
			value, ok := auth.envValue("GIT_CONFIG_VALUE_0")
			require.True(t, ok)
			assert.Equal(t, header, value)
			key, ok := auth.envValue("GIT_CONFIG_KEY_0")
			require.True(t, ok)
			assert.Equal(t, "http.https://github.com/.extraheader", key)
			count, ok := auth.envValue("GIT_CONFIG_COUNT")
			require.True(t, ok)
			assert.Equal(t, "1", count)

			for _, cmd := range recorder.cmds {
				argv := strings.Join(cmd.argv, " ")
				assert.NotContains(t, argv, testToken)
				assert.NotContains(t, argv, basicToken(testToken))
				assert.NotContains(t, argv, "extraheader")
				prompt, ok := cmd.envValue("GIT_TERMINAL_PROMPT")
				require.True(t, ok, "GIT_TERMINAL_PROMPT missing from %v", cmd.argv)
				assert.Equal(t, "0", prompt)
			}
		})
	}
}
