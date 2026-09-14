package registry

import (
	"path"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func mcpServerPrefix(serverID string) string {
	return "mcp_" + serverID + "_"
}

func matchesPattern(name, pattern string) bool {
	if pattern == name {
		return true
	}
	if strings.Contains(pattern, "*") {
		ok, err := path.Match(pattern, name)
		return err == nil && ok
	}
	return false
}

func matchesAny(name string, patterns []string) bool {
	for _, p := range patterns {
		if matchesPattern(name, p) {
			return true
		}
	}
	return false
}

func isMCPTool(name string) bool {
	return strings.HasPrefix(name, "mcp_")
}

func mcpServerFromTool(name string) string {
	if !isMCPTool(name) {
		return ""
	}
	rest := strings.TrimPrefix(name, "mcp_")
	idx := strings.Index(rest, "_")
	if idx <= 0 {
		return ""
	}
	return rest[:idx]
}

func FilterToolNames(all []string, policy domain.ToolPolicy) []string {
	if policy.IsZero() {
		return all
	}

	restrictTools := len(policy.AllowTools) > 0
	restrictMCP := len(policy.AllowMCPServers) > 0

	if !restrictTools && !restrictMCP {
		return all
	}

	result := make([]string, 0, len(all))
	for _, name := range all {
		if isMCPTool(name) {
			if !restrictMCP {
				result = append(result, name)
				continue
			}
			server := mcpServerFromTool(name)
			for _, allowed := range policy.AllowMCPServers {
				if server == allowed {
					result = append(result, name)
					break
				}
			}
			continue
		}
		if !restrictTools {
			result = append(result, name)
			continue
		}
		if matchesAny(name, policy.AllowTools) {
			result = append(result, name)
		}
	}
	return result
}

func IsToolAllowed(name string, policy domain.ToolPolicy, all []string) bool {
	if policy.IsZero() {
		return true
	}
	allowed := FilterToolNames(all, policy)
	for _, n := range allowed {
		if n == name {
			return true
		}
	}
	return false
}
