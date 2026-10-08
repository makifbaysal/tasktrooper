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
	// Access is the mode a seeded copy of the template starts in; empty is
	// "all". The engine, data and security servers are "listed": an editor or
	// a notebook kernel reaches only the agents that name it.
	Access MCPAccess
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
		{
			// Talks to the MCP for Unity package (Package Manager → git URL
			// https://github.com/CoplayDev/unity-mcp.git?path=/MCPForUnity) over a
			// local bridge, so the editor has to be open.
			ID: "unity", Access: MCPAccessListed, Label: "Unity (MCP for Unity)", Description: "Unity Editor: scenes, assets, scripts, tests",
			Enabled: false, Transport: "stdio", Command: "uvx",
			Args: []string{"--from", "mcpforunityserver==10.3.0", "mcp-for-unity", "--transport", "stdio"},
			Env:  map[string]string{"DISABLE_TELEMETRY": "true"},
			ConfigFields: []MCPConfigField{
				{Key: "UNITY_MCP_DEFAULT_INSTANCE", Location: "env", Description: "Which open editor to drive when several are running"},
			},
		},
		{
			// The server refuses an editor addon of another version, so the pin is
			// the one field to keep in step with the project's addons/godot_ai.
			ID: "godot", Access: MCPAccessListed, Label: "Godot (Godot AI)", Description: "Godot editor: scenes, nodes, scripts, tests",
			Enabled: false, Transport: "stdio", Command: "uvx",
			Args: []string{"--from", "godot-ai==4.3.0", "godot-ai", "attach", "--disable-telemetry"},
			ConfigFields: []MCPConfigField{
				{Key: "1", Location: "args", Required: true, Description: "godot-ai==<version of the project's addons/godot_ai>"},
			},
		},
		{
			// Epic's own server, built into the editor from UE 5.8 (plugin "Unreal
			// MCP"); it listens on loopback without authentication.
			ID: "unreal", Access: MCPAccessListed, Label: "Unreal Engine (Unreal MCP)", Description: "Unreal Editor's built-in MCP server (UE 5.8+)",
			Enabled: false, Transport: "http", URL: "http://127.0.0.1:8000/mcp",
		},
		{
			ID: "blender", Access: MCPAccessListed, Label: "Blender (MCP for Blender)", Description: "Blender: models, materials, renders, assets",
			Enabled: false, Transport: "stdio", Command: "uvx",
			Args: []string{"mcp-for-blender"},
			Env:  map[string]string{"DISABLE_TELEMETRY": "true", "BLENDER_MCP_SAFE_MODE": "1"},
		},
		{
			ID: "jupyter", Access: MCPAccessListed, Label: "Jupyter", Description: "Read, edit and run cells in a running JupyterLab",
			Enabled: false, Transport: "stdio", Command: "uvx",
			Args: []string{"jupyter-mcp-server==2.2.3"},
			Env:  map[string]string{"JUPYTER_URL": "http://localhost:8888", "ALLOW_IMG_OUTPUT": "true"},
			ConfigFields: []MCPConfigField{
				{Key: "JUPYTER_URL", Location: "env", Required: true, Description: "JupyterLab on localhost, e.g. http://localhost:8888"},
				{Key: "JUPYTER_TOKEN", Location: "env", Secret: true, Required: true},
			},
		},
		{
			// In-memory by default: SQL over the Parquet/CSV files the agent
			// points it at, with no database file to configure first.
			ID: "duckdb", Access: MCPAccessListed, Label: "DuckDB / MotherDuck", Description: "SQL over DuckDB files, Parquet and CSV",
			Enabled: false, Transport: "stdio", Command: "uvx",
			Args: []string{"mcp-server-motherduck", "--db-path", ":memory:", "--read-write"},
			ConfigFields: []MCPConfigField{
				{Key: "2", Location: "args", Description: "A .duckdb file path, or md: for MotherDuck (drop --read-write for a file)"},
				{Key: "motherduck_token", Location: "env", Secret: true, Description: "Only for md:"},
			},
		},
		{
			ID: "dbt", Access: MCPAccessListed, Label: "dbt", Description: "dbt lineage, compile, test and run (dbt Core)",
			Enabled: false, Transport: "stdio", Command: "uvx",
			Args: []string{"dbt-mcp"},
			Env:  map[string]string{"DBT_PROJECT_DIR": "", "DBT_PATH": ""},
			ConfigFields: []MCPConfigField{
				{Key: "DBT_PROJECT_DIR", Location: "env", Required: true, Description: "Folder holding dbt_project.yml"},
				{Key: "DBT_PATH", Location: "env", Required: true, Description: "Full path to the dbt executable"},
				{Key: "DISABLE_TOOLS", Location: "env", Description: "Comma list of tools to switch off, e.g. run,build,clone"},
				{Key: "DBT_TOKEN", Location: "env", Secret: true, Description: "dbt Platform only"},
			},
		},
		{
			ID: "mlflow", Access: MCPAccessListed, Label: "MLflow", Description: "MLflow experiments, runs, models and traces",
			Enabled: false, Transport: "stdio", Command: "uvx",
			Args: []string{"--from", "mlflow[mcp]>=3.5.1", "mlflow", "mcp", "run"},
			Env:  map[string]string{"MLFLOW_TRACKING_URI": "http://localhost:5000", "MLFLOW_MCP_TOOLS": "ml"},
			ConfigFields: []MCPConfigField{
				{Key: "MLFLOW_TRACKING_URI", Location: "env", Required: true, Description: "Tracking server, e.g. http://localhost:5000"},
			},
		},
		{
			// Rules fetched by name (p/default, p/owasp-top-ten) work with metrics
			// off; only --config auto needs them on.
			ID: "semgrep", Access: MCPAccessListed, Label: "Semgrep", Description: "Local static analysis with Semgrep rules",
			Enabled: false, Transport: "stdio", Command: "uvx",
			Args: []string{"--from", "semgrep==1.180.0", "semgrep", "mcp"},
			Env:  map[string]string{"SEMGREP_SEND_METRICS": "off"},
			ConfigFields: []MCPConfigField{
				{Key: "SEMGREP_APP_TOKEN", Location: "env", Secret: true, Description: "Optional: platform findings and Pro rules"},
			},
		},
		{
			// No npx/uvx form: the osv-scanner binary (brew, go install) carries
			// the server; only package names and versions leave the machine.
			ID: "osv-scanner", Access: MCPAccessListed, Label: "OSV-Scanner", Description: "Known-vulnerable dependencies in lockfiles",
			Enabled: false, Transport: "stdio", Command: "osv-scanner",
			Args: []string{"experimental-mcp"},
		},
		{
			// The hosted GitHub server narrowed to its security toolsets and held
			// read-only by the server itself, for a reviewer that must not write.
			ID: "github-security", Access: MCPAccessListed, Label: "GitHub security (read-only)", Description: "Code scanning, secret scanning and Dependabot alerts",
			Enabled: false, Transport: "http", URL: "https://api.githubcopilot.com/mcp/",
			Headers: map[string]string{
				"Authorization":  "Bearer ${GITHUB_TOKEN}",
				"X-MCP-Toolsets": "repos,pull_requests,code_security,secret_protection,dependabot,security_advisories",
				"X-MCP-Readonly": "true",
				"X-MCP-Lockdown": "true",
			},
			ConfigFields: []MCPConfigField{
				{Key: "Authorization", Location: "headers", Secret: true, Required: true, Description: "Bearer <fine-grained read-only token>"},
			},
		},
		{
			// Snyk Code uploads the scanned source to Snyk's cloud.
			ID: "snyk", Access: MCPAccessListed, Label: "Snyk", Description: "Snyk Code, Open Source, IaC and container scans",
			Enabled: false, Transport: "stdio", Command: "npx",
			Args: []string{"-y", "snyk@1.1307.4", "mcp", "-t", "stdio"},
			ConfigFields: []MCPConfigField{
				{Key: "SNYK_TOKEN", Location: "env", Secret: true, Required: true},
				{Key: "SNYK_CFG_ORG", Location: "env", Description: "Organization id"},
			},
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
