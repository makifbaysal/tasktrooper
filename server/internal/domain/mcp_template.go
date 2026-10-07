package domain

import (
	"os"
	"strconv"
	"strings"
)

type MCPConfigField struct {
	Key         string `json:"key"`
	Location    string `json:"location"`
	Secret      bool   `json:"secret"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

type MCPTemplate struct {
	ID           string
	Label        string
	Description  string
	Enabled      bool
	Transport    string
	Command      string
	Args         []string
	Env          map[string]string
	URL          string
	Headers      map[string]string
	AllowedTools []string
	ConfigFields []MCPConfigField
}

func MCPTemplates() []MCPTemplate {
	return []MCPTemplate{
		{
			ID: "filesystem", Label: "Filesystem", Description: "Filesystem access",
			Enabled: false, Transport: "stdio", Command: "npx",
			// The OS temp dir rather than a literal /tmp, which on Windows
			// resolves to C:\tmp and does not exist.
			Args: []string{"-y", "@modelcontextprotocol/server-filesystem", os.TempDir()},
			ConfigFields: []MCPConfigField{
				{Key: "2", Location: "args", Required: true, Description: "Root directory path"},
			},
		},
		{
			// Upstream never shipped this one to npm; it is a Python package and
			// uvx is the only first-party way to run it.
			ID: "git", Label: "Git", Description: "Git repository operations",
			Enabled: false, Transport: "stdio", Command: "uvx",
			Args: []string{"mcp-server-git", "--repository", "."},
			ConfigFields: []MCPConfigField{
				{Key: "2", Location: "args", Required: true, Description: "Repository path"},
			},
		},
		{
			// @modelcontextprotocol/server-github was deprecated; GitHub's own
			// server is remote, so there is nothing to install.
			ID: "github", Label: "GitHub", Description: "GitHub API integration",
			Enabled: false, Transport: "http", URL: "https://api.githubcopilot.com/mcp/",
			Headers: map[string]string{"Authorization": "Bearer ${GITHUB_TOKEN}"},
			ConfigFields: []MCPConfigField{
				{Key: "Authorization", Location: "headers", Secret: true, Required: true, Description: "Authorization header, e.g. Bearer ghp_..."},
			},
		},
		{
			ID: "gitlab", Label: "GitLab", Description: "GitLab projects, merge requests, issues and pipelines",
			Enabled: false, Transport: "stdio", Command: "npx",
			Args: []string{"-y", "@zereight/mcp-gitlab"},
			Env: map[string]string{
				"GITLAB_PERSONAL_ACCESS_TOKEN": "${GITLAB_TOKEN}",
				"GITLAB_API_URL":               "https://gitlab.com/api/v4",
			},
			ConfigFields: []MCPConfigField{
				{Key: "GITLAB_PERSONAL_ACCESS_TOKEN", Location: "env", Secret: true, Required: true},
				{Key: "GITLAB_API_URL", Location: "env", Required: true, Description: "https://gitlab.com/api/v4, or your self-hosted instance"},
			},
		},
		{
			ID: "postgres", Label: "PostgreSQL", Description: "PostgreSQL database",
			Enabled: false, Transport: "stdio", Command: "npx",
			Args: []string{"-y", "@modelcontextprotocol/server-postgres", "${POSTGRES_URL}"},
			ConfigFields: []MCPConfigField{
				{Key: "2", Location: "args", Required: true, Description: "PostgreSQL connection URL"},
			},
		},
		{
			ID: "slack", Label: "Slack", Description: "Slack messaging",
			Enabled: false, Transport: "stdio", Command: "npx",
			Args: []string{"-y", "@modelcontextprotocol/server-slack"},
			ConfigFields: []MCPConfigField{
				{Key: "SLACK_BOT_TOKEN", Location: "env", Secret: true, Required: true},
				{Key: "SLACK_TEAM_ID", Location: "env", Secret: false, Required: true},
			},
		},
		{
			ID: "huggingface", Label: "Hugging Face", Description: "Model search",
			Enabled: false, Transport: "http", URL: "https://huggingface.co/mcp",
			Headers:      map[string]string{"Authorization": "Bearer ${HF_TOKEN}"},
			AllowedTools: []string{"model_search"},
			ConfigFields: []MCPConfigField{
				{Key: "Authorization", Location: "headers", Secret: true, Required: true},
			},
		},
		{
			ID: "browser", Label: "Browser", Description: "Browser automation",
			Enabled: true, Transport: "stdio", Command: "npx",
			Args: []string{"-y", "@browsermcp/mcp@latest"},
		},
	}
}

func MCPTemplateByID(id string) (MCPTemplate, bool) {
	for _, t := range MCPTemplates() {
		if t.ID == id {
			return t, true
		}
	}
	return MCPTemplate{}, false
}

func ConfigFieldsForServer(id string) []MCPConfigField {
	if t, ok := MCPTemplateByID(id); ok {
		return t.ConfigFields
	}
	return nil
}

func SecretFieldsForServer(id string) []MCPConfigField {
	fields := ConfigFieldsForServer(id)
	var secrets []MCPConfigField
	for _, f := range fields {
		if f.Secret {
			secrets = append(secrets, f)
		}
	}
	return secrets
}

// MissingConfigFields lists the required fields a server has no usable value
// for yet — the reason an enabled server will not connect. hasSecret answers
// whether an encrypted value is on file, since a secret's value never reaches
// the stored server.
func MissingConfigFields(server MCPServer, hasSecret func(location, key string) bool) []MCPConfigField {
	var missing []MCPConfigField
	for _, field := range ConfigFieldsForServer(server.ID) {
		if !field.Required {
			continue
		}
		if field.Secret {
			if hasSecret == nil || !hasSecret(field.Location, field.Key) {
				missing = append(missing, field)
			}
			continue
		}
		if !hasConfigValue(server, field) {
			missing = append(missing, field)
		}
	}
	return missing
}

func hasConfigValue(server MCPServer, field MCPConfigField) bool {
	switch field.Location {
	case "env":
		return isFilledIn(server.Env[field.Key])
	case "headers":
		return isFilledIn(server.Headers[field.Key])
	case "args":
		idx, err := strconv.Atoi(field.Key)
		if err != nil || idx < 0 || idx >= len(server.Args) {
			return false
		}
		return isFilledIn(server.Args[idx])
	default:
		return true
	}
}

// An unexpanded ${VAR} is the template's placeholder, not an answer.
func isFilledIn(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.Contains(value, "${")
}
