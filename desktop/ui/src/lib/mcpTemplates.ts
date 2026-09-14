import type { MCPEnvSchemaField, MCPServerCreateInput } from "@/api";

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
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
    env_schema: [{ key: "path", label: "Accessible directory", placeholder: "/tmp" }],
  },
  {
    id: "git",
    label: "Git",
    description: "Git repository operations",
    enabled: false,
    transport: "stdio",
    command: "npx",
    args: ["-y", "@modelcontextprotocol/server-git", "--repository", "."],
  },
  {
    id: "github",
    label: "GitHub",
    description: "GitHub API integration",
    enabled: false,
    transport: "stdio",
    command: "npx",
    args: ["-y", "@modelcontextprotocol/server-github"],
    env: { GITHUB_PERSONAL_ACCESS_TOKEN: "${GITHUB_TOKEN}" },
    secret_fields: ["GITHUB_PERSONAL_ACCESS_TOKEN"],
    env_schema: [{ key: "GITHUB_PERSONAL_ACCESS_TOKEN", label: "GitHub Token", secret: true }],
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
