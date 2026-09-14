package mcp_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	mcpadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/mcp"
)

type MCPNamespaceSuite struct {
	suite.Suite
}

func (s *MCPNamespaceSuite) TestNamespacedToolName() {
	cases := []struct {
		serverID string
		toolName string
		expected string
	}{
		{"filesystem", "read_file", "mcp_filesystem_read_file"},
		{"github", "create_pr", "mcp_github_create_pr"},
		{"hf", "model_search", "mcp_hf_model_search"},
	}

	for _, tc := range cases {
		result := mcpadapter.NamespacedToolName(tc.serverID, tc.toolName)
		s.Equal(tc.expected, result, "serverID=%s toolName=%s", tc.serverID, tc.toolName)
	}
}

func TestMCPNamespaceSuite(t *testing.T) {
	suite.Run(t, new(MCPNamespaceSuite))
}
