package board

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type reviewTaskManager struct {
	*storedDocumentTaskManager
	updates     []domain.UpdateTaskDocumentRequest
	annotations []domain.TaskDocumentAnnotation
	resolved    []domain.AnnotationResolution
}

func (r *reviewTaskManager) UpdateDocument(ctx context.Context, repositoryID, taskID, docID uuid.UUID, req domain.UpdateTaskDocumentRequest) (domain.TaskDocument, error) {
	r.updates = append(r.updates, req)
	doc, err := r.storedDocumentTaskManager.UpdateDocument(ctx, repositoryID, taskID, docID, req)
	if err != nil {
		return doc, err
	}
	if req.Format != nil {
		for i := range r.docs {
			if r.docs[i].ID == docID {
				r.docs[i].Format = *req.Format
				doc = r.docs[i]
			}
		}
	}
	return doc, nil
}

func (r *reviewTaskManager) ListAnnotations(_ context.Context, _, taskID uuid.UUID, _ *uuid.UUID) ([]domain.TaskDocumentAnnotation, error) {
	return r.annotations, nil
}

func (r *reviewTaskManager) ResolveAnnotations(_ context.Context, _, _ uuid.UUID, items []domain.AnnotationResolution) (domain.AnnotationResolveResult, error) {
	result := domain.AnnotationResolveResult{Resolved: []domain.TaskDocumentAnnotation{}}
	for _, item := range items {
		found := false
		for i, a := range r.annotations {
			if a.ID != item.ID {
				continue
			}
			found = true
			r.annotations[i].Status = domain.AnnotationStatusResolved
			r.annotations[i].Reply = item.Reply
			result.Resolved = append(result.Resolved, r.annotations[i])
		}
		if !found {
			result.Failed = append(result.Failed, domain.AnnotationResolveFailure{ID: item.ID.String(), Error: domain.ErrAnnotationNotFound.Error()})
		}
		r.resolved = append(r.resolved, item)
	}
	return result, nil
}

const reportHTML = `<!DOCTYPE html><html><head><style>h1{color:red}</style></head><body>` +
	`<h1 id="summary">Summary</h1><p>Use a queue for exports.</p><ol><li>Add the worker</li></ol></body></html>`

func newReviewKit(t *testing.T, docs ...domain.TaskDocument) (*ToolKit, *reviewTaskManager, uuid.UUID) {
	t.Helper()
	kit, stored, taskID := newStoredDocumentKit(docs...)
	stored.task.TaskType = "analiz"
	review := &reviewTaskManager{storedDocumentTaskManager: stored}
	kit.Tasks = review
	return kit, review, taskID
}

func decode(t *testing.T, content string) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(content), &out), content)
	return out
}

func TestAddDocumentPassesTheHTMLFormat(t *testing.T) {
	kit, tasks, taskID := newDocumentKit("analiz")
	res := newAddDocumentTool(kit).Execute(runContext("grep_code"),
		`{"task_id":"`+taskID.String()+`","title":"analiz: 2026-09-26 export","format":"html","content":"<h1>x</h1>"}`)

	require.False(t, res.IsError, res.Content)
	require.Len(t, tasks.added, 1)
	assert.Equal(t, domain.DocumentFormatHTML, tasks.added[0].Format)
}

func TestAddDocumentStillRequiresGroundingForHTML(t *testing.T) {
	kit, tasks, taskID := newDocumentKit("analiz")
	res := newAddDocumentTool(kit).Execute(runContext("load_skill"),
		`{"task_id":"`+taskID.String()+`","title":"analiz: x","format":"html","content":"<h1>x</h1>"}`)

	assert.True(t, res.IsError)
	assert.Empty(t, tasks.added)
}

func TestUpdateDocumentAppliesEditsToTheStoredSource(t *testing.T) {
	docID := uuid.New()
	kit, tasks, taskID := newReviewKit(t, domain.TaskDocument{ID: docID, Title: "analiz: x", Content: reportHTML, Format: domain.DocumentFormatHTML})

	res := newUpdateDocumentTool(kit).Execute(runContext("grep_code"),
		`{"task_id":"`+taskID.String()+`","edits":[{"old_text":"Use a queue for exports.","new_text":"Use a cron job for exports."},{"old_text":"<li>Add the worker</li>","new_text":"<li>Add the job</li>"}]}`)

	require.False(t, res.IsError, res.Content)
	require.Len(t, tasks.updates, 1)
	assert.Nil(t, tasks.updates[0].Format, "the document keeps its format")
	stored := tasks.docs[0].Content
	assert.Contains(t, stored, "Use a cron job for exports.")
	assert.Contains(t, stored, "<li>Add the job</li>")
	assert.Contains(t, stored, "<style>h1{color:red}</style>", "untouched source survives")

	out := decode(t, res.Content)
	assert.NotContains(t, out, "content", "an html write does not echo the report back")
	assert.Equal(t, docID.String(), out["id"], "the ledger still finds the document id")
}

func TestUpdateDocumentRefusesAmbiguousOrMissingEdits(t *testing.T) {
	kit, tasks, taskID := newReviewKit(t, domain.TaskDocument{ID: uuid.New(), Title: "analiz: x", Content: reportHTML, Format: domain.DocumentFormatHTML})
	tool := newUpdateDocumentTool(kit)

	missing := tool.Execute(runContext("grep_code"),
		`{"task_id":"`+taskID.String()+`","edits":[{"old_text":"not in the report","new_text":"x"}]}`)
	assert.True(t, missing.IsError)
	assert.Contains(t, missing.Content, "not found")

	twice := tool.Execute(runContext("grep_code"),
		`{"task_id":"`+taskID.String()+`","edits":[{"old_text":"<","new_text":"x"}]}`)
	assert.True(t, twice.IsError)
	assert.Contains(t, twice.Content, "exactly once")

	both := tool.Execute(runContext("grep_code"),
		`{"task_id":"`+taskID.String()+`","content":"x","edits":[{"old_text":"Summary","new_text":"x"}]}`)
	assert.True(t, both.IsError)

	assert.Empty(t, tasks.updates, "a refused edit saves nothing")
	assert.Equal(t, reportHTML, tasks.docs[0].Content)
}

func TestUpdateDocumentCanChangeTheFormat(t *testing.T) {
	kit, tasks, taskID := newReviewKit(t, domain.TaskDocument{ID: uuid.New(), Title: "spec", Content: "# spec"})

	res := newUpdateDocumentTool(kit).Execute(runContext("grep_code"),
		`{"task_id":"`+taskID.String()+`","format":"html","content":"<h1>spec</h1>"}`)

	require.False(t, res.IsError, res.Content)
	require.NotNil(t, tasks.updates[0].Format)
	assert.Equal(t, domain.DocumentFormatHTML, *tasks.updates[0].Format)
}

func TestListDocumentsRendersHTMLAsTextByDefault(t *testing.T) {
	kit, _, taskID := newReviewKit(t,
		domain.TaskDocument{ID: uuid.New(), Title: "analiz: x", Content: reportHTML, Format: domain.DocumentFormatHTML},
		domain.TaskDocument{ID: uuid.New(), Title: "notes", Content: "# markdown <b>stays</b>", Format: domain.DocumentFormatMarkdown},
	)

	res := newListDocumentsTool(kit).Execute(context.Background(), `{"task_id":"`+taskID.String()+`"}`)

	require.False(t, res.IsError, res.Content)
	docs := decode(t, res.Content)["documents"].([]any)
	report := docs[0].(map[string]any)
	assert.Equal(t, "html", report["format"])
	assert.Equal(t, "text", report["rendered_as"])
	assert.Contains(t, report["content"], "# Summary")
	assert.Contains(t, report["content"], "1. Add the worker")
	assert.NotContains(t, report["content"], "<")
	assert.NotContains(t, report["content"], "color:red")

	notes := docs[1].(map[string]any)
	assert.Equal(t, "markdown", notes["format"])
	assert.Equal(t, "# markdown <b>stays</b>", notes["content"])
	assert.NotContains(t, notes, "rendered_as")
}

func TestListDocumentsRawReturnsTheHTMLSourceInWindows(t *testing.T) {
	docID := uuid.New()
	big := "<!DOCTYPE html><html><body>" + strings.Repeat("<p>é</p>", 3000) + "</body></html>"
	kit, _, taskID := newReviewKit(t,
		domain.TaskDocument{ID: docID, Title: "analiz: x", Content: big, Format: domain.DocumentFormatHTML},
		domain.TaskDocument{ID: uuid.New(), Title: "notes", Content: "# md"},
	)
	tool := newListDocumentsTool(kit)

	first := tool.Execute(context.Background(), `{"task_id":"`+taskID.String()+`","document_id":"`+docID.String()+`","raw":true}`)
	require.False(t, first.IsError, first.Content)
	docs := decode(t, first.Content)["documents"].([]any)
	require.Len(t, docs, 1, "document_id narrows the result")
	window := docs[0].(map[string]any)
	assert.Len(t, []rune(window["content"].(string)), rawWindowChars)
	assert.EqualValues(t, len([]rune(big)), window["content_total_chars"])
	next := int(window["next_offset"].(float64))
	assert.Equal(t, rawWindowChars, next)

	var rebuilt strings.Builder
	rebuilt.WriteString(window["content"].(string))
	for next > 0 {
		res := tool.Execute(context.Background(), `{"task_id":"`+taskID.String()+`","document_id":"`+docID.String()+`","raw":true,"offset":`+itoa(next)+`}`)
		require.False(t, res.IsError, res.Content)
		w := decode(t, res.Content)["documents"].([]any)[0].(map[string]any)
		rebuilt.WriteString(w["content"].(string))
		if n, ok := w["next_offset"]; ok {
			next = int(n.(float64))
		} else {
			next = 0
		}
	}
	assert.Equal(t, big, rebuilt.String(), "the windows put back together are the whole source")

	multi := tool.Execute(context.Background(), `{"task_id":"`+taskID.String()+`","raw":true,"offset":10}`)
	assert.True(t, multi.IsError, "an offset without a document is ambiguous")
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestListDocumentAnnotationsFiltersAndNamesTheDocument(t *testing.T) {
	docID := uuid.New()
	kit, review, taskID := newReviewKit(t, domain.TaskDocument{ID: docID, Title: "analiz: 2026-09-26 export", Content: reportHTML, Format: domain.DocumentFormatHTML})
	now := time.Now()
	review.annotations = []domain.TaskDocumentAnnotation{
		{ID: uuid.New(), TaskID: taskID, DocumentID: docID, Quote: "Use a queue", Body: "why?", Status: domain.AnnotationStatusSubmitted, SubmittedAt: &now},
		{ID: uuid.New(), TaskID: taskID, DocumentID: docID, Quote: "Summary", Body: "draft", Status: domain.AnnotationStatusOpen},
	}
	tool := newListAnnotationsTool(kit)

	res := tool.Execute(context.Background(), `{"task_id":"`+taskID.String()+`","status":"submitted"}`)
	require.False(t, res.IsError, res.Content)
	out := decode(t, res.Content)
	assert.EqualValues(t, 1, out["count"])
	item := out["annotations"].([]any)[0].(map[string]any)
	assert.Equal(t, "analiz: 2026-09-26 export", item["document_title"])
	assert.Equal(t, "Use a queue", item["quote"])
	assert.Equal(t, "why?", item["comment"])

	all := decode(t, tool.Execute(context.Background(), `{"task_id":"`+taskID.String()+`"}`).Content)
	assert.EqualValues(t, 2, all["count"])

	bad := tool.Execute(context.Background(), `{"task_id":"`+taskID.String()+`","status":"done"}`)
	assert.True(t, bad.IsError)
}

func TestResolveDocumentAnnotationsReportsPartialFailures(t *testing.T) {
	docID := uuid.New()
	kit, review, taskID := newReviewKit(t, domain.TaskDocument{ID: docID, Title: "analiz: x", Content: reportHTML, Format: domain.DocumentFormatHTML})
	ann := domain.TaskDocumentAnnotation{ID: uuid.New(), TaskID: taskID, DocumentID: docID, Quote: "q", Body: "b", Status: domain.AnnotationStatusSubmitted}
	review.annotations = []domain.TaskDocumentAnnotation{ann}
	tool := newResolveAnnotationsTool(kit)

	res := tool.Execute(context.Background(), `{"task_id":"`+taskID.String()+`","items":[`+
		`{"id":"`+ann.ID.String()+`","reply":"Switched to cron — see §4"},`+
		`{"id":"not-a-uuid","reply":"x"},`+
		`{"id":"`+uuid.NewString()+`","reply":"x"}]}`)

	require.False(t, res.IsError, res.Content)
	out := decode(t, res.Content)
	assert.EqualValues(t, 1, out["resolved"])
	assert.Len(t, out["failed"], 2)
	assert.Equal(t, domain.AnnotationStatusResolved, review.annotations[0].Status)
	assert.Equal(t, "Switched to cron — see §4", review.annotations[0].Reply)

	none := tool.Execute(context.Background(), `{"task_id":"`+taskID.String()+`","items":[{"id":"`+uuid.NewString()+`","reply":"x"}]}`)
	assert.True(t, none.IsError, "nothing resolved is a failed call")

	empty := tool.Execute(context.Background(), `{"task_id":"`+taskID.String()+`","items":[]}`)
	assert.True(t, empty.IsError)
}

func TestAnnotationToolsSayWhenTheBuildCannotServeThem(t *testing.T) {
	kit, _, taskID := newDocumentKit("analiz")
	res := newListAnnotationsTool(kit).Execute(context.Background(), `{"task_id":"`+taskID.String()+`"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "cannot read document annotations")
}
