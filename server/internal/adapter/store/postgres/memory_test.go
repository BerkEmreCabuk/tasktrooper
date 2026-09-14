package postgres

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestBuildMemoryListQuery(t *testing.T) {
	agentID := uuid.New()
	repoID := uuid.New()

	tests := []struct {
		name     string
		query    domain.MemoryQuery
		wantSQL  []string
		wantArgs []any
	}{
		{
			name:     "agent inside a repository sees its own and the team's, global plus that repo",
			query:    domain.MemoryQuery{AgentID: agentID, RepositoryID: &repoID, Limit: 10},
			wantSQL:  []string{"(agent_id = $1 OR agent_id IS NULL)", "(repository_id IS NULL OR repository_id = $2)", "LIMIT $3"},
			wantArgs: []any{agentID, repoID, 10},
		},
		{
			name:     "agent without a repository only sees global memories",
			query:    domain.MemoryQuery{AgentID: agentID, Limit: 5},
			wantSQL:  []string{"(agent_id = $1 OR agent_id IS NULL)", "repository_id IS NULL", "LIMIT $2"},
			wantArgs: []any{agentID, 5},
		},
		{
			name:     "project scope excludes global memories",
			query:    domain.MemoryQuery{AgentID: agentID, RepositoryID: &repoID, Repo: domain.MemoryRepoScopeProject, Limit: 5},
			wantSQL:  []string{"repository_id = $2"},
			wantArgs: []any{agentID, repoID, 5},
		},
		{
			name:     "global scope ignores the repository in play",
			query:    domain.MemoryQuery{AgentID: agentID, RepositoryID: &repoID, Repo: domain.MemoryRepoScopeGlobal, Limit: 5},
			wantSQL:  []string{"repository_id IS NULL"},
			wantArgs: []any{agentID, 5},
		},
		{
			name:     "any scope drops the repository constraint entirely",
			query:    domain.MemoryQuery{AgentID: agentID, Repo: domain.MemoryRepoScopeAny, Limit: 7},
			wantSQL:  []string{"(agent_id = $1 OR agent_id IS NULL)", "LIMIT $2"},
			wantArgs: []any{agentID, 7},
		},
		{
			name:     "agent owner excludes team memories",
			query:    domain.MemoryQuery{AgentID: agentID, Owner: domain.MemoryOwnerAgent, Repo: domain.MemoryRepoScopeAny, Limit: 3},
			wantSQL:  []string{"agent_id = $1"},
			wantArgs: []any{agentID, 3},
		},
		{
			name:     "team owner reads only shared rows",
			query:    domain.MemoryQuery{AgentID: agentID, Owner: domain.MemoryOwnerTeam, Repo: domain.MemoryRepoScopeAny, Limit: 3},
			wantSQL:  []string{"agent_id IS NULL"},
			wantArgs: []any{3},
		},
		{
			name:     "team memories for one repository",
			query:    domain.MemoryQuery{Owner: domain.MemoryOwnerTeam, RepositoryID: &repoID, Repo: domain.MemoryRepoScopeProject, Limit: 4},
			wantSQL:  []string{"agent_id IS NULL", "repository_id = $1"},
			wantArgs: []any{repoID, 4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args := buildMemoryListQuery(tt.query)
			for _, want := range tt.wantSQL {
				if !strings.Contains(sql, want) {
					t.Fatalf("sql %q missing %q", sql, want)
				}
			}
			if len(args) != len(tt.wantArgs) {
				t.Fatalf("args = %v, want %v", args, tt.wantArgs)
			}
			for i := range args {
				if args[i] != tt.wantArgs[i] {
					t.Fatalf("arg %d = %v, want %v", i, args[i], tt.wantArgs[i])
				}
			}
		})
	}
}

func TestBuildMemoryListQueryAgentOwnerNeverLeaksTeamRows(t *testing.T) {
	sql, _ := buildMemoryListQuery(domain.MemoryQuery{
		AgentID: uuid.New(), Owner: domain.MemoryOwnerAgent, Repo: domain.MemoryRepoScopeAny,
	})
	if strings.Contains(sql, "agent_id IS NULL") {
		t.Fatalf("agent-only query returned team rows: %s", sql)
	}
}

func TestBuildMemoryListQueryDefaultLimit(t *testing.T) {
	_, args := buildMemoryListQuery(domain.MemoryQuery{AgentID: uuid.New(), Repo: domain.MemoryRepoScopeAny})
	if args[len(args)-1] != 50 {
		t.Fatalf("default limit = %v, want 50", args[len(args)-1])
	}
}
