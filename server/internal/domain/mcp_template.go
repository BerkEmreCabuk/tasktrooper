package domain

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
			Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
			ConfigFields: []MCPConfigField{
				{Key: "3", Location: "args", Required: true, Description: "Root directory path"},
			},
		},
		{
			ID: "git", Label: "Git", Description: "Git repository operations",
			Enabled: false, Transport: "stdio", Command: "npx",
			Args: []string{"-y", "@modelcontextprotocol/server-git", "--repository", "."},
			ConfigFields: []MCPConfigField{
				{Key: "3", Location: "args", Required: true, Description: "Repository path"},
			},
		},
		{
			ID: "github", Label: "GitHub", Description: "GitHub API integration",
			Enabled: false, Transport: "stdio", Command: "npx",
			Args: []string{"-y", "@modelcontextprotocol/server-github"},
			ConfigFields: []MCPConfigField{
				{Key: "GITHUB_PERSONAL_ACCESS_TOKEN", Location: "env", Secret: true, Required: true},
			},
		},
		{
			ID: "postgres", Label: "PostgreSQL", Description: "PostgreSQL database",
			Enabled: false, Transport: "stdio", Command: "npx",
			Args: []string{"-y", "@modelcontextprotocol/server-postgres", "${POSTGRES_URL}"},
			ConfigFields: []MCPConfigField{
				{Key: "3", Location: "args", Required: true, Description: "PostgreSQL connection URL"},
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
