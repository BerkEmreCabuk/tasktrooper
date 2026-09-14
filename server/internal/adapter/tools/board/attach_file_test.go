package board

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAttachments struct {
	linkedRepo, linkedTask, linkedAttachment uuid.UUID
	meta                                     domain.AttachmentMeta
	err                                      error
}

func (f *fakeAttachments) LinkTask(_ context.Context, repositoryID, taskID, attachmentID uuid.UUID) (domain.AttachmentMeta, error) {
	f.linkedRepo, f.linkedTask, f.linkedAttachment = repositoryID, taskID, attachmentID
	if f.err != nil {
		return domain.AttachmentMeta{}, f.err
	}
	return f.meta, nil
}

// attachTasks answers the listings resolveTaskRef / resolveTaskRepositoryID walk.
type attachTasks struct {
	TaskManager
	tasks []domain.BoardTask
}

func (m attachTasks) ListAllTasks(context.Context) ([]domain.BoardTask, error) {
	return m.tasks, nil
}

func (m attachTasks) FindTaskRepositoryID(_ context.Context, taskID uuid.UUID) (uuid.UUID, error) {
	for _, t := range m.tasks {
		if t.ID == taskID {
			return t.RepositoryID, nil
		}
	}
	return uuid.Nil, assert.AnError
}

func TestAttachTaskFile_LinksByBoardKey(t *testing.T) {
	taskID, repoID, attachmentID := uuid.New(), uuid.New(), uuid.New()
	attachments := &fakeAttachments{meta: domain.AttachmentMeta{ID: attachmentID, Filename: "shot.png"}}
	kit := &ToolKit{
		Tasks:       attachTasks{tasks: []domain.BoardTask{{ID: taskID, RepositoryID: repoID, Key: "DE-7"}}},
		Attachments: attachments,
	}
	tool := newAttachTaskFileTool(kit)

	args, _ := json.Marshal(map[string]string{"task_id": "DE-7", "attachment_id": attachmentID.String()})
	result := tool.Execute(context.Background(), string(args))

	require.False(t, result.IsError, result.Content)
	assert.Equal(t, repoID, attachments.linkedRepo)
	assert.Equal(t, taskID, attachments.linkedTask)
	assert.Equal(t, attachmentID, attachments.linkedAttachment)
	var meta domain.AttachmentMeta
	require.NoError(t, json.Unmarshal([]byte(result.Content), &meta))
	assert.Equal(t, "shot.png", meta.Filename)
}

func TestAttachTaskFile_InvalidArgumentsJSON(t *testing.T) {
	kit := &ToolKit{Tasks: attachTasks{}, Attachments: &fakeAttachments{}}
	tool := newAttachTaskFileTool(kit)

	result := tool.Execute(context.Background(), "{not json")

	require.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid arguments")
}

func TestAttachTaskFile_UnknownTaskRef(t *testing.T) {
	kit := &ToolKit{Tasks: attachTasks{}, Attachments: &fakeAttachments{}}
	tool := newAttachTaskFileTool(kit)

	args, _ := json.Marshal(map[string]string{"task_id": "DE-404", "attachment_id": uuid.New().String()})
	result := tool.Execute(context.Background(), string(args))

	require.True(t, result.IsError)
	assert.Contains(t, result.Content, "DE-404")
}

func TestAttachTaskFile_InvalidAttachmentID(t *testing.T) {
	taskID := uuid.New()
	kit := &ToolKit{
		Tasks:       attachTasks{tasks: []domain.BoardTask{{ID: taskID, RepositoryID: uuid.New(), Key: "DE-1"}}},
		Attachments: &fakeAttachments{},
	}
	tool := newAttachTaskFileTool(kit)

	args, _ := json.Marshal(map[string]string{"task_id": taskID.String(), "attachment_id": "not-a-uuid"})
	result := tool.Execute(context.Background(), string(args))

	require.True(t, result.IsError)
	assert.Contains(t, result.Content, "attachment_id")
}

func TestAttachTaskFile_LinkErrorSurfaces(t *testing.T) {
	taskID := uuid.New()
	kit := &ToolKit{
		Tasks:       attachTasks{tasks: []domain.BoardTask{{ID: taskID, RepositoryID: uuid.New(), Key: "DE-1"}}},
		Attachments: &fakeAttachments{err: assert.AnError},
	}
	tool := newAttachTaskFileTool(kit)

	args, _ := json.Marshal(map[string]string{"task_id": "DE-1", "attachment_id": uuid.New().String()})
	result := tool.Execute(context.Background(), string(args))

	require.True(t, result.IsError)
}

// The tool only registers when the kit carries an attachment manager, so a
// build without Postgres never advertises a tool that cannot work.
func TestNewExecutors_AttachToolRequiresAttachmentManager(t *testing.T) {
	without := NewExecutors(&ToolKit{Tasks: attachTasks{}})
	for _, exec := range without {
		assert.NotEqual(t, attachTaskFileToolName, exec.Name())
	}

	with := NewExecutors(&ToolKit{Tasks: attachTasks{}, Attachments: &fakeAttachments{}})
	found := false
	for _, exec := range with {
		if exec.Name() == attachTaskFileToolName {
			found = true
		}
	}
	assert.True(t, found)
}
