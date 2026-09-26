package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repository"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type annTaskStore struct {
	port.BoardTaskStore
	mu    sync.Mutex
	tasks map[uuid.UUID]domain.BoardTask
}

func (f *annTaskStore) Get(_ context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[taskID]
	if !ok || t.RepositoryID != repositoryID {
		return domain.BoardTask{}, fmt.Errorf("%w: %s", domain.ErrBoardTaskNotFound, taskID)
	}
	return t, nil
}

func (f *annTaskStore) Update(_ context.Context, task domain.BoardTask) (domain.BoardTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[task.ID] = task
	return task, nil
}

type annDocStore struct {
	docs map[uuid.UUID]domain.TaskDocument
}

func (f *annDocStore) Create(_ context.Context, doc domain.TaskDocument) (domain.TaskDocument, error) {
	doc.ID = uuid.New()
	f.docs[doc.ID] = doc
	return doc, nil
}

func (f *annDocStore) Get(_ context.Context, taskID, docID uuid.UUID) (domain.TaskDocument, error) {
	d, ok := f.docs[docID]
	if !ok || d.TaskID != taskID {
		return domain.TaskDocument{}, fmt.Errorf("%w: %s", domain.ErrTaskDocumentNotFound, docID)
	}
	return d, nil
}

func (f *annDocStore) ListByTask(_ context.Context, taskID uuid.UUID) ([]domain.TaskDocument, error) {
	var out []domain.TaskDocument
	for _, d := range f.docs {
		if d.TaskID == taskID {
			out = append(out, d)
		}
	}
	return out, nil
}

func (f *annDocStore) Update(_ context.Context, doc domain.TaskDocument) (domain.TaskDocument, error) {
	f.docs[doc.ID] = doc
	return doc, nil
}

func (f *annDocStore) Delete(_ context.Context, _, docID uuid.UUID) error {
	delete(f.docs, docID)
	return nil
}

type annCommentStore struct {
	comments []domain.TaskComment
}

func (f *annCommentStore) Create(_ context.Context, c domain.TaskComment) (domain.TaskComment, error) {
	c.ID = uuid.New()
	f.comments = append(f.comments, c)
	return c, nil
}

func (f *annCommentStore) ListByTask(context.Context, uuid.UUID) ([]domain.TaskComment, error) {
	return f.comments, nil
}

type annStore struct {
	mu    sync.Mutex
	items map[uuid.UUID]domain.TaskDocumentAnnotation
	clock time.Time
}

func (f *annStore) Create(_ context.Context, a domain.TaskDocumentAnnotation) (domain.TaskDocumentAnnotation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clock = f.clock.Add(time.Second)
	a.ID = uuid.New()
	a.CreatedAt, a.UpdatedAt = f.clock, f.clock
	f.items[a.ID] = a
	return a, nil
}

func (f *annStore) Get(_ context.Context, taskID, id uuid.UUID) (domain.TaskDocumentAnnotation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.items[id]
	if !ok || a.TaskID != taskID {
		return domain.TaskDocumentAnnotation{}, fmt.Errorf("%w: %s", domain.ErrAnnotationNotFound, id)
	}
	return a, nil
}

func (f *annStore) ListByTask(_ context.Context, taskID uuid.UUID, documentID *uuid.UUID) ([]domain.TaskDocumentAnnotation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.TaskDocumentAnnotation{}
	for _, a := range f.items {
		if a.TaskID == taskID && (documentID == nil || a.DocumentID == *documentID) {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (f *annStore) Update(_ context.Context, a domain.TaskDocumentAnnotation) (domain.TaskDocumentAnnotation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[a.ID] = a
	return a, nil
}

func (f *annStore) Delete(_ context.Context, _, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.items, id)
	return nil
}

func (f *annStore) MarkSubmitted(_ context.Context, _ uuid.UUID, ids []uuid.UUID, at time.Time) ([]uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var moved []uuid.UUID
	for _, id := range ids {
		a := f.items[id]
		if a.Status != domain.AnnotationStatusOpen {
			continue
		}
		a.Status = domain.AnnotationStatusSubmitted
		a.SubmittedAt = &at
		f.items[id] = a
		moved = append(moved, id)
	}
	return moved, nil
}

type recordingRevisionNotifier struct{ tasks []domain.BoardTask }

func (r *recordingRevisionNotifier) NotifyRevision(_ context.Context, task domain.BoardTask) {
	r.tasks = append(r.tasks, task)
}

type annFixture struct {
	app      *fiber.App
	repoID   uuid.UUID
	taskID   uuid.UUID
	docID    uuid.UUID
	tasks    *annTaskStore
	docs     *annDocStore
	comments *annCommentStore
	anns     *annStore
	notifier *recordingRevisionNotifier
}

func newAnnotationTestApp(t *testing.T, column domain.TaskColumn) *annFixture {
	t.Helper()
	repoID, taskID, docID := uuid.New(), uuid.New(), uuid.New()
	f := &annFixture{
		repoID: repoID, taskID: taskID, docID: docID,
		tasks: &annTaskStore{tasks: map[uuid.UUID]domain.BoardTask{taskID: {
			ID: taskID, RepositoryID: repoID, Key: "A-7", TaskType: "analiz", Column: column,
		}}},
		docs: &annDocStore{docs: map[uuid.UUID]domain.TaskDocument{docID: {
			ID: docID, TaskID: taskID, Title: "analiz: 2026-09-26 export", Format: domain.DocumentFormatHTML,
			Content: "<!DOCTYPE html><html><head></head><body><p>Use a queue.</p></body></html>",
		}}},
		comments: &annCommentStore{},
		anns:     &annStore{items: map[uuid.UUID]domain.TaskDocumentAnnotation{}, clock: time.Unix(1_700_000_000, 0)},
		notifier: &recordingRevisionNotifier{},
	}
	repos := newFakeDeployOpsRepositoryStore([]domain.Repository{{ID: repoID, Name: "tasktrooper"}})
	svc := repository.NewService(repos, f.tasks, nil, nil, f.docs, f.comments, nil, nil, nil, nil)
	svc.SetAnnotationStore(f.anns)
	svc.SetEvolution(f.notifier)
	h := &Handler{repositorySvc: svc}
	f.app = fiber.New()
	h.registerRepositoryRoutes(f.app)
	return f
}

func (f *annFixture) base() string {
	return "/v1/repositories/" + f.repoID.String() + "/tasks/" + f.taskID.String()
}

func (f *annFixture) do(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := f.app.Test(req, 5000)
	require.NoError(t, err)
	out, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, out
}

func (f *annFixture) create(t *testing.T, quote, body string) domain.TaskDocumentAnnotation {
	t.Helper()
	status, raw := f.do(t, "POST", f.base()+"/documents/"+f.docID.String()+"/annotations",
		map[string]string{"quote": quote, "prefix": "", "suffix": "", "body": body})
	require.Equal(t, fiber.StatusCreated, status, string(raw))
	var ann domain.TaskDocumentAnnotation
	require.NoError(t, json.Unmarshal(raw, &ann))
	return ann
}

func (f *annFixture) setStatus(id uuid.UUID, status domain.AnnotationStatus, reply string) {
	a := f.anns.items[id]
	a.Status = status
	a.Reply = reply
	now := time.Now()
	if status == domain.AnnotationStatusResolved {
		a.ResolvedAt = &now
	}
	f.anns.items[id] = a
}

func TestCreateAnnotationReturnsAnOpenUserComment(t *testing.T) {
	f := newAnnotationTestApp(t, domain.TaskColumnAnalizReview)

	status, raw := f.do(t, "POST", f.base()+"/documents/"+f.docID.String()+"/annotations",
		map[string]string{"quote": "Use a queue.", "prefix": "", "suffix": "", "body": "  Why not a cron?  "})

	require.Equal(t, fiber.StatusCreated, status, string(raw))
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))
	assert.Equal(t, "open", body["status"])
	assert.Equal(t, "user", body["created_by_type"])
	assert.Equal(t, "Why not a cron?", body["body"])
	assert.Equal(t, f.docID.String(), body["document_id"])
	assert.Equal(t, "", body["reply"])
	assert.NotContains(t, body, "submitted_at")
	assert.NotContains(t, body, "resolved_at")
}

func TestCreateAnnotationValidates(t *testing.T) {
	f := newAnnotationTestApp(t, domain.TaskColumnAnalizReview)
	path := f.base() + "/documents/" + f.docID.String() + "/annotations"

	cases := map[string]map[string]string{
		"empty quote":   {"quote": "  ", "body": "x"},
		"empty body":    {"quote": "q", "body": "   "},
		"long quote":    {"quote": strings.Repeat("q", domain.MaxAnnotationQuoteChars+1), "body": "x"},
		"long body":     {"quote": "q", "body": strings.Repeat("b", domain.MaxAnnotationBodyChars+1)},
		"long prefix":   {"quote": "q", "body": "x", "prefix": strings.Repeat("p", domain.MaxAnnotationContextChars+1)},
		"long suffix":   {"quote": "q", "body": "x", "suffix": strings.Repeat("s", domain.MaxAnnotationContextChars+1)},
		"multibyte max": {"quote": strings.Repeat("ş", domain.MaxAnnotationQuoteChars), "body": "x"},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			status, raw := f.do(t, "POST", path, body)
			if name == "multibyte max" {
				assert.Equal(t, fiber.StatusCreated, status, "limits count characters, not bytes: %s", raw)
				return
			}
			assert.Equal(t, fiber.StatusBadRequest, status, string(raw))
		})
	}

	status, _ := f.do(t, "POST", f.base()+"/documents/"+uuid.NewString()+"/annotations",
		map[string]string{"quote": "q", "body": "x"})
	assert.Equal(t, fiber.StatusNotFound, status, "a document not on this task")

	status, _ = f.do(t, "POST", "/v1/repositories/"+f.repoID.String()+"/tasks/"+uuid.NewString()+"/documents/"+f.docID.String()+"/annotations",
		map[string]string{"quote": "q", "body": "x"})
	assert.Equal(t, fiber.StatusNotFound, status, "an unknown task")
}

func TestListAnnotationsOrdersByCreationAndFiltersByDocument(t *testing.T) {
	f := newAnnotationTestApp(t, domain.TaskColumnAnalizReview)
	first := f.create(t, "Use a queue.", "one")
	second := f.create(t, "Use a queue.", "two")
	other := uuid.New()
	f.docs.docs[other] = domain.TaskDocument{ID: other, TaskID: f.taskID, Title: "notes"}
	f.docID = other
	third := f.create(t, "notes", "three")

	status, raw := f.do(t, "GET", f.base()+"/annotations", nil)
	require.Equal(t, fiber.StatusOK, status)
	var all struct {
		Annotations []domain.TaskDocumentAnnotation `json:"annotations"`
	}
	require.NoError(t, json.Unmarshal(raw, &all))
	require.Len(t, all.Annotations, 3)
	assert.Equal(t, []uuid.UUID{first.ID, second.ID, third.ID},
		[]uuid.UUID{all.Annotations[0].ID, all.Annotations[1].ID, all.Annotations[2].ID})

	status, raw = f.do(t, "GET", f.base()+"/annotations?document_id="+other.String(), nil)
	require.Equal(t, fiber.StatusOK, status)
	var filtered struct {
		Annotations []domain.TaskDocumentAnnotation `json:"annotations"`
	}
	require.NoError(t, json.Unmarshal(raw, &filtered))
	require.Len(t, filtered.Annotations, 1)
	assert.Equal(t, third.ID, filtered.Annotations[0].ID)

	status, _ = f.do(t, "GET", f.base()+"/annotations?document_id=nope", nil)
	assert.Equal(t, fiber.StatusBadRequest, status)
}

func TestListAnnotationsIsAnEmptyArrayNotNull(t *testing.T) {
	f := newAnnotationTestApp(t, domain.TaskColumnAnalizReview)
	status, raw := f.do(t, "GET", f.base()+"/annotations", nil)
	require.Equal(t, fiber.StatusOK, status)
	assert.JSONEq(t, `{"annotations":[]}`, string(raw))
}

func TestPatchAnnotationFollowsTheStateMachine(t *testing.T) {
	f := newAnnotationTestApp(t, domain.TaskColumnAnalizReview)
	ann := f.create(t, "Use a queue.", "first")
	path := f.base() + "/annotations/" + ann.ID.String()

	status, raw := f.do(t, "PATCH", path, map[string]string{"body": "edited"})
	require.Equal(t, fiber.StatusOK, status, string(raw))
	assert.Equal(t, "edited", f.anns.items[ann.ID].Body)

	f.setStatus(ann.ID, domain.AnnotationStatusSubmitted, "")
	status, _ = f.do(t, "PATCH", path, map[string]string{"body": "too late"})
	assert.Equal(t, fiber.StatusConflict, status, "a submitted comment is part of a review")
	status, _ = f.do(t, "PATCH", path, map[string]string{"status": "open"})
	assert.Equal(t, fiber.StatusConflict, status, "a submitted comment waits for the agent")

	f.setStatus(ann.ID, domain.AnnotationStatusResolved, "done: switched to cron")
	status, raw = f.do(t, "PATCH", path, map[string]string{"status": "open"})
	require.Equal(t, fiber.StatusOK, status, string(raw))
	reopened := f.anns.items[ann.ID]
	assert.Equal(t, domain.AnnotationStatusOpen, reopened.Status)
	assert.Empty(t, reopened.Reply)
	assert.Nil(t, reopened.ResolvedAt)

	status, _ = f.do(t, "PATCH", path, map[string]string{"status": "resolved"})
	assert.Equal(t, fiber.StatusBadRequest, status, "only reopening is a human transition")

	status, _ = f.do(t, "PATCH", f.base()+"/annotations/"+uuid.NewString(), map[string]string{"body": "x"})
	assert.Equal(t, fiber.StatusNotFound, status)
}

func TestDeleteAnnotationOnlyWhileOpen(t *testing.T) {
	f := newAnnotationTestApp(t, domain.TaskColumnAnalizReview)
	open := f.create(t, "Use a queue.", "drop me")
	sent := f.create(t, "Use a queue.", "keep me")
	f.setStatus(sent.ID, domain.AnnotationStatusSubmitted, "")

	status, _ := f.do(t, "DELETE", f.base()+"/annotations/"+open.ID.String(), nil)
	assert.Equal(t, fiber.StatusNoContent, status)
	assert.NotContains(t, f.anns.items, open.ID)

	status, _ = f.do(t, "DELETE", f.base()+"/annotations/"+sent.ID.String(), nil)
	assert.Equal(t, fiber.StatusConflict, status)
	assert.Contains(t, f.anns.items, sent.ID)
}

func TestSubmitMovesTheTaskToNeedRevisionWithOneSummaryComment(t *testing.T) {
	f := newAnnotationTestApp(t, domain.TaskColumnAnalizReview)
	a := f.create(t, "Use a queue.", "Why not a cron job?")
	b := f.create(t, "Use a queue.", "Name the retry policy.")
	resolved := f.create(t, "Use a queue.", "old round")
	f.setStatus(resolved.ID, domain.AnnotationStatusResolved, "fixed")

	status, raw := f.do(t, "POST", f.base()+"/annotations/submit", map[string]string{"note": "Mostly good."})

	require.Equal(t, fiber.StatusOK, status, string(raw))
	var body struct {
		Submitted int              `json:"submitted"`
		Task      domain.BoardTask `json:"task"`
	}
	require.NoError(t, json.Unmarshal(raw, &body))
	assert.Equal(t, 2, body.Submitted)
	assert.Equal(t, domain.TaskColumnNeedRevision, body.Task.Column)
	assert.Equal(t, domain.TaskColumnNeedRevision, f.tasks.tasks[f.taskID].Column)

	for _, id := range []uuid.UUID{a.ID, b.ID} {
		got := f.anns.items[id]
		assert.Equal(t, domain.AnnotationStatusSubmitted, got.Status)
		assert.NotNil(t, got.SubmittedAt)
	}
	assert.Equal(t, domain.AnnotationStatusResolved, f.anns.items[resolved.ID].Status, "only open comments are sent")

	require.Len(t, f.comments.comments, 1, "one comment for the whole review")
	comment := f.comments.comments[0]
	assert.Equal(t, "user", comment.AuthorType)
	assert.Contains(t, comment.Content, "Analysis review: 2 comments")
	assert.Contains(t, comment.Content, `1. "Use a queue." → Why not a cron job?`)
	assert.Contains(t, comment.Content, `2. "Use a queue." → Name the retry policy.`)
	assert.Contains(t, comment.Content, "Mostly good.")

	require.Len(t, f.notifier.tasks, 1, "a submitted review is a human rejection like any other")
}

func TestSubmitRefusesOutsideAnalizReview(t *testing.T) {
	f := newAnnotationTestApp(t, domain.TaskColumnInProgress)
	f.create(t, "Use a queue.", "x")

	status, _ := f.do(t, "POST", f.base()+"/annotations/submit", nil)

	assert.Equal(t, fiber.StatusConflict, status)
	assert.Equal(t, domain.TaskColumnInProgress, f.tasks.tasks[f.taskID].Column)
	assert.Empty(t, f.comments.comments)
}

func TestSubmitRefusesWithNothingOpen(t *testing.T) {
	f := newAnnotationTestApp(t, domain.TaskColumnAnalizReview)
	ann := f.create(t, "Use a queue.", "x")
	f.setStatus(ann.ID, domain.AnnotationStatusSubmitted, "")

	status, _ := f.do(t, "POST", f.base()+"/annotations/submit", map[string]string{})

	assert.Equal(t, fiber.StatusBadRequest, status)
	assert.Equal(t, domain.TaskColumnAnalizReview, f.tasks.tasks[f.taskID].Column)
	assert.Empty(t, f.comments.comments)
}

func TestCreateHTMLDocumentIsSanitizedAndSizeLimited(t *testing.T) {
	f := newAnnotationTestApp(t, domain.TaskColumnAnalizReview)

	status, raw := f.do(t, "POST", f.base()+"/documents", map[string]string{
		"title": "analiz: report", "format": "html",
		"content": `<h1 onclick="x()">Report</h1><script>alert(1)</script>`,
	})
	require.Equal(t, fiber.StatusCreated, status, string(raw))
	var doc domain.TaskDocument
	require.NoError(t, json.Unmarshal(raw, &doc))
	assert.Equal(t, domain.DocumentFormatHTML, doc.Format)
	assert.True(t, strings.HasPrefix(doc.Content, "<!DOCTYPE html>"))
	assert.NotContains(t, doc.Content, "script")
	assert.NotContains(t, doc.Content, "onclick")

	status, raw = f.do(t, "POST", f.base()+"/documents", map[string]string{"title": "md", "content": "# hi"})
	require.Equal(t, fiber.StatusCreated, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &doc))
	assert.Equal(t, domain.DocumentFormatMarkdown, doc.Format, "format defaults to markdown")
	assert.Equal(t, "# hi", doc.Content)

	status, _ = f.do(t, "POST", f.base()+"/documents", map[string]string{
		"title": "huge", "format": "html", "content": strings.Repeat("a", domain.MaxHTMLDocumentBytes+1),
	})
	assert.Equal(t, fiber.StatusBadRequest, status)

	status, _ = f.do(t, "POST", f.base()+"/documents", map[string]string{"title": "x", "format": "pdf"})
	assert.Equal(t, fiber.StatusBadRequest, status)

	status, raw = f.do(t, "PATCH", f.base()+"/documents/"+f.docID.String(), map[string]string{
		"content": `<p onmouseover="x()">revised</p>`,
	})
	require.Equal(t, fiber.StatusOK, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &doc))
	assert.Equal(t, domain.DocumentFormatHTML, doc.Format, "an update keeps the existing format")
	assert.Contains(t, doc.Content, "<p>revised</p>")
}
