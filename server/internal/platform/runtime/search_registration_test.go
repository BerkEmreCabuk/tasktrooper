package runtime

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/search"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/require"
)

func registeredToolNames(t *testing.T, searchCfg domain.SearchConfig) []string {
	t.Helper()
	e := &engine{reg: registry.New()}
	cfg := &domain.Config{}
	cfg.Tools.Search = searchCfg
	require.NoError(t, e.registerBuiltinTools(cfg))
	return e.reg.AllToolNames()
}

func hasTool(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

// TestWebSearchFollowsTheEnabledFlagAlone. web_search carries no credential —
// it queries DuckDuckGo and falls back to Bing — so the flag is the whole
// decision. The key gate that used to sit here was the fix for a different
// problem: a provider with no subscription could only ever answer "not
// configured", which one production run read as bad luck and retried three
// times. There is nothing left to be missing.
func TestWebSearchFollowsTheEnabledFlagAlone(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  domain.SearchConfig
		want bool
	}{
		{
			name: "enabled",
			cfg:  domain.SearchConfig{Enabled: true, MaxResults: 5},
			want: true,
		},
		{
			// No max_results either: New picks its own default rather than
			// registering a tool that returns nothing.
			name: "enabled with nothing else set",
			cfg:  domain.SearchConfig{Enabled: true},
			want: true,
		},
		{
			name: "disabled",
			cfg:  domain.SearchConfig{Enabled: false, MaxResults: 5},
			want: false,
		},
		{
			name: "zero value",
			cfg:  domain.SearchConfig{},
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			names := registeredToolNames(t, tc.cfg)
			require.Equal(t, tc.want, hasTool(names, search.ToolName),
				"registered tools: %v", names)
		})
	}
}
