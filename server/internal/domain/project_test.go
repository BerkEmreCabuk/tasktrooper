package domain_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestValidTaskColumn(t *testing.T) {
	assert.True(t, domain.ValidTaskColumn(domain.TaskColumnBacklog))
	assert.True(t, domain.ValidTaskColumn(domain.TaskColumnReleased))
	assert.False(t, domain.ValidTaskColumn(domain.TaskColumn("invalid")))
}

func TestWorkspaceIndexProgressPercent(t *testing.T) {
	idx := domain.WorkspaceIndex{FilesTotal: 10, FilesProcessed: 4}
	assert.Equal(t, 40, idx.ProgressPercent())

	idx = domain.WorkspaceIndex{Status: domain.IndexStatusCompleted}
	assert.Equal(t, 100, idx.ProgressPercent())

	idx = domain.WorkspaceIndex{Status: domain.IndexStatusRunning}
	assert.Equal(t, 0, idx.ProgressPercent())
}
