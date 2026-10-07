import type { MCPEnvSchemaField, MCPServerCreateInput } from "@/api";
import { exampleFolderPath } from "@/lib/platform";

export interface MCPTemplate extends MCPServerCreateInput {
  label: string;
  description: string;
  env_schema?: MCPEnvSchemaField[];
  secret_fields?: string[];
}

export const MCP_TEMPLATES: MCPTemplate[] = [
  {
    id: "filesystem",
    label: "Filesystem",
    description: "Filesystem access",
    enabled: false,
    transport: "stdio",
    command: "npx",
    args: ["-y", "@modelcontextprotocol/server-filesystem", ""],
    env_schema: [{ key: "path", label: "Accessible directory", placeholder: exampleFolderPath() }],
  },
  {
    // Upstream never shipped this one to npm; it is a Python package and uvx is
    // the only first-party way to run it.
    id: "git",
    label: "Git",
    description: "Git repository operations",
    enabled: false,
    transport: "stdio",
    command: "uvx",
    args: ["mcp-server-git", "--repository", "."],
  },
  {
    // @modelcontextprotocol/server-github was deprecated; GitHub's own server is
    // remote, so there is nothing to install.
    id: "github",
    label: "GitHub",
    description: "GitHub API integration",
    enabled: false,
    transport: "http",
    url: "https://api.githubcopilot.com/mcp/",
    headers: { Authorization: "Bearer ${GITHUB_TOKEN}" },
    secret_fields: ["header:Authorization"],
    env_schema: [
      { key: "Authorization", label: "GitHub Token", secret: true, placeholder: "Bearer ghp_..." },
    ],
  },
  {
    id: "gitlab",
    label: "GitLab",
    description: "GitLab projects, merge requests, issues and pipelines",
    enabled: false,
    transport: "stdio",
    command: "npx",
    args: ["-y", "@zereight/mcp-gitlab"],
    env: {
      GITLAB_PERSONAL_ACCESS_TOKEN: "${GITLAB_TOKEN}",
      GITLAB_API_URL: "https://gitlab.com/api/v4",
    },
    secret_fields: ["GITLAB_PERSONAL_ACCESS_TOKEN"],
    env_schema: [
      { key: "GITLAB_PERSONAL_ACCESS_TOKEN", label: "GitLab Token", secret: true },
      { key: "GITLAB_API_URL", label: "GitLab API URL", placeholder: "https://gitlab.com/api/v4" },
    ],
  },
  {
    id: "postgres",
    label: "PostgreSQL",
    description: "PostgreSQL database",
    enabled: false,
    transport: "stdio",
    command: "npx",
    args: ["-y", "@modelcontextprotocol/server-postgres", "${POSTGRES_URL}"],
  },
  {
    id: "slack",
    label: "Slack",
    description: "Slack messaging",
    enabled: false,
    transport: "stdio",
    command: "npx",
    args: ["-y", "@modelcontextprotocol/server-slack"],
    env: { SLACK_BOT_TOKEN: "${SLACK_BOT_TOKEN}", SLACK_TEAM_ID: "${SLACK_TEAM_ID}" },
    secret_fields: ["SLACK_BOT_TOKEN"],
    env_schema: [
      { key: "SLACK_BOT_TOKEN", label: "Slack Bot Token", secret: true },
      { key: "SLACK_TEAM_ID", label: "Slack Team ID" },
    ],
  },
  {
    id: "huggingface",
    label: "Hugging Face",
    description: "Model search",
    enabled: false,
    transport: "http",
    url: "https://huggingface.co/mcp",
    headers: { Authorization: "Bearer ${HF_TOKEN}" },
    allowed_tools: ["model_search"],
    secret_fields: ["header:Authorization"],
    env_schema: [{ key: "Authorization", label: "Bearer Token", secret: true }],
  },
  {
    id: "browser",
    label: "Browser",
    description: "Browser automation",
    enabled: true,
    transport: "stdio",
    command: "npx",
    args: ["-y", "@browsermcp/mcp@latest"],
  },
];
