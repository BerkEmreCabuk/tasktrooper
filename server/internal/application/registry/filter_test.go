package registry_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type FilterSuite struct {
	suite.Suite
}

func (s *FilterSuite) TestNoPolicyReturnsAll() {
	all := []string{"run_terminal", "web_search", "mcp_browser_navigate"}
	result := registry.FilterToolNames(all, domain.ToolPolicy{})
	s.ElementsMatch(all, result)
}

func (s *FilterSuite) TestAllowToolsOnly() {
	all := []string{"run_terminal", "web_search", "fetch_url", "mcp_browser_navigate"}
	policy := domain.ToolPolicy{AllowTools: []string{"web_search", "fetch_url"}}
	result := registry.FilterToolNames(all, policy)
	s.ElementsMatch([]string{"web_search", "fetch_url", "mcp_browser_navigate"}, result)
}

func (s *FilterSuite) TestAllowMCPServersOnly() {
	all := []string{"run_terminal", "web_search", "mcp_browser_navigate", "mcp_git_log"}
	policy := domain.ToolPolicy{AllowMCPServers: []string{"browser"}}
	result := registry.FilterToolNames(all, policy)
	s.ElementsMatch([]string{"run_terminal", "web_search", "mcp_browser_navigate"}, result)
}

func (s *FilterSuite) TestAllowToolsAndMCPServers() {
	all := []string{"run_terminal", "web_search", "mcp_browser_navigate", "mcp_git_log"}
	policy := domain.ToolPolicy{
		AllowMCPServers: []string{"browser"},
		AllowTools:      []string{"web_search"},
	}
	result := registry.FilterToolNames(all, policy)
	s.ElementsMatch([]string{"web_search", "mcp_browser_navigate"}, result)
}

func (s *FilterSuite) TestWildcardAllow() {
	all := []string{"grep_code", "get_repo_tree", "get_symbol_skeleton", "web_search"}
	policy := domain.ToolPolicy{AllowTools: []string{"get_*"}}
	result := registry.FilterToolNames(all, policy)
	s.Contains(result, "get_repo_tree")
	s.Contains(result, "get_symbol_skeleton")
	s.NotContains(result, "grep_code")
	s.NotContains(result, "web_search")
}

func TestFilterSuite(t *testing.T) {
	suite.Run(t, new(FilterSuite))
}
