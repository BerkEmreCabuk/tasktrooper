package board

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// storedDocumentTaskManager is documentTaskManager plus the read/write halves of
// the document store, so the upsert and update paths can be exercised end to
// end: without a DocumentLister the tools fall back to plain appends, which is
// exactly the behaviour these tests are here to rule out.
type storedDocumentTaskManager struct {
	*documentTaskManager
	docs []domain.TaskDocument
}

func (s *storedDocumentTaskManager) ListDocuments(ctx context.Context, repositoryID, taskID uuid.UUID) ([]domain.TaskDocument, error) {
	return s.docs, nil
}

func (s *storedDocumentTaskManager) UpdateDocument(ctx context.Context, repositoryID, taskID, docID uuid.UUID, req domain.UpdateTaskDocumentRequest) (domain.TaskDocument, error) {
	for i, doc := range s.docs {
		if doc.ID != docID {
			continue
		}
		if req.Title != nil {
			s.docs[i].Title = *req.Title
		}
		if req.Content != nil {
			s.docs[i].Content = *req.Content
		}
		return s.docs[i], nil
	}
	return domain.TaskDocument{}, context.Canceled
}

func newStoredDocumentKit(docs ...domain.TaskDocument) (*ToolKit, *storedDocumentTaskManager, uuid.UUID) {
	kit, tasks, taskID := newDocumentKit("task")
	stored := &storedDocumentTaskManager{documentTaskManager: tasks}
	for _, doc := range docs {
		doc.TaskID = taskID
		stored.docs = append(stored.docs, doc)
	}
	kit.Tasks = stored
	return kit, stored, taskID
}

func TestAddDocumentRewritesSameTitleInsteadOfDuplicating(t *testing.T) {
	kit, tasks, taskID := newStoredDocumentKit(domain.TaskDocument{
		ID: uuid.New(), Title: "Spec", Content: "# old",
	})
	tool := newAddDocumentTool(kit)

	res := tool.Execute(runContext("grep_code"),
		`{"task_id":"`+taskID.String()+`","title":"spec","content":"# new"}`)

	if res.IsError {
		t.Fatalf("write must succeed, got %q", res.Content)
	}
	if len(tasks.added) != 0 {
		t.Fatalf("a title already on the card must not create a second document, got %d added", len(tasks.added))
	}
	if len(tasks.docs) != 1 || tasks.docs[0].Content != "# new" {
		t.Fatalf("existing document must be rewritten in place, got %+v", tasks.docs)
	}
	if !strings.Contains(res.Content, "update_task_document") {
		t.Fatalf("result must name the tool to use for the next revision, got %q", res.Content)
	}
}

func TestUpdateDocumentByTitle(t *testing.T) {
	kit, tasks, taskID := newStoredDocumentKit(
		domain.TaskDocument{ID: uuid.New(), Title: "Spec", Content: "# old spec"},
		domain.TaskDocument{ID: uuid.New(), Title: "Plan", Content: "# old plan"},
	)
	tool := newUpdateDocumentTool(kit)

	res := tool.Execute(runContext("grep_code"),
		`{"task_id":"`+taskID.String()+`","title":"Plan","content":"# new plan"}`)

	if res.IsError {
		t.Fatalf("update must succeed, got %q", res.Content)
	}
	if tasks.docs[1].Content != "# new plan" {
		t.Fatalf("named document must be rewritten, got %q", tasks.docs[1].Content)
	}
	if tasks.docs[0].Content != "# old spec" {
		t.Fatalf("the other document must be untouched, got %q", tasks.docs[0].Content)
	}
	if len(tasks.added) != 0 {
		t.Fatalf("an update must never add a document, got %d", len(tasks.added))
	}
}

func TestUpdateDocumentNeedsAnIdentifierWhenAmbiguous(t *testing.T) {
	kit, _, taskID := newStoredDocumentKit(
		domain.TaskDocument{ID: uuid.New(), Title: "Spec"},
		domain.TaskDocument{ID: uuid.New(), Title: "Plan"},
	)
	tool := newUpdateDocumentTool(kit)

	res := tool.Execute(runContext("grep_code"),
		`{"task_id":"`+taskID.String()+`","content":"# which one?"}`)

	if !res.IsError {
		t.Fatalf("an ambiguous update must be refused, got %q", res.Content)
	}
	if !strings.Contains(res.Content, "Spec") || !strings.Contains(res.Content, "Plan") {
		t.Fatalf("the refusal must list the titles to choose from, got %q", res.Content)
	}
}

func TestUpdateDocumentDefaultsToTheOnlyDocument(t *testing.T) {
	kit, tasks, taskID := newStoredDocumentKit(domain.TaskDocument{
		ID: uuid.New(), Title: "Spec", Content: "# old",
	})
	tool := newUpdateDocumentTool(kit)

	res := tool.Execute(runContext("grep_code"),
		`{"task_id":"`+taskID.String()+`","content":"# new"}`)

	if res.IsError {
		t.Fatalf("a single-document task needs no identifier, got %q", res.Content)
	}
	if tasks.docs[0].Content != "# new" {
		t.Fatalf("document must be rewritten, got %q", tasks.docs[0].Content)
	}
}
