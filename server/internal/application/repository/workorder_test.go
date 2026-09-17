package repository

// Relations that are worth something: the order a cycle cannot be written into,
// the note the release cannot drift from, and the analysis an implementation
// task can actually reach.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// graphRelationStore is a full relation store over an edge list, unlike
// fakeRelationStore which only ever answers deploy_depends_on. The cycle guards
// walk the graph, so the fake has to BE a graph.
type graphRelationStore struct {
	edges []domain.TaskRelation
	keys  map[uuid.UUID]string
	title map[uuid.UUID]string
}

func newGraphRelations() *graphRelationStore {
	return &graphRelationStore{keys: map[uuid.UUID]string{}, title: map[uuid.UUID]string{}}
}

func (g *graphRelationStore) add(source, target uuid.UUID, relType domain.TaskRelationType) {
	g.edges = append(g.edges, domain.TaskRelation{
		ID: uuid.New(), SourceTaskID: source, TargetTaskID: target, RelationType: relType,
	})
}

func (g *graphRelationStore) ReplaceForTask(_ context.Context, sourceTaskID uuid.UUID, rels []domain.TaskRelationInput) ([]domain.TaskRelation, error) {
	kept := g.edges[:0:0]
	for _, e := range g.edges {
		if e.SourceTaskID != sourceTaskID {
			kept = append(kept, e)
		}
	}
	g.edges = kept
	var out []domain.TaskRelation
	for _, rel := range rels {
		g.add(sourceTaskID, rel.TargetTaskID, rel.RelationType)
		out = append(out, g.edges[len(g.edges)-1])
	}
	return out, nil
}

func (g *graphRelationStore) ReplaceForTaskOfType(_ context.Context, sourceTaskID uuid.UUID, relType domain.TaskRelationType, rels []domain.TaskRelationInput) ([]domain.TaskRelation, error) {
	kept := g.edges[:0:0]
	for _, e := range g.edges {
		if e.SourceTaskID != sourceTaskID || e.RelationType != relType {
			kept = append(kept, e)
		}
	}
	g.edges = kept
	var out []domain.TaskRelation
	for _, rel := range rels {
		g.add(sourceTaskID, rel.TargetTaskID, relType)
		out = append(out, g.edges[len(g.edges)-1])
	}
	return out, nil
}

func (g *graphRelationStore) ListBySource(_ context.Context, sourceTaskID uuid.UUID) ([]domain.TaskRelation, error) {
	var out []domain.TaskRelation
	for _, e := range g.edges {
		if e.SourceTaskID != sourceTaskID {
			continue
		}
		e.TargetKey = g.keys[e.TargetTaskID]
		e.TargetTitle = g.title[e.TargetTaskID]
		out = append(out, e)
	}
	return out, nil
}

func (g *graphRelationStore) ListBlockedBy(_ context.Context, targetTaskID uuid.UUID) ([]domain.TaskRelation, error) {
	var out []domain.TaskRelation
	for _, e := range g.edges {
		if e.TargetTaskID != targetTaskID || e.RelationType != domain.TaskRelationBlocks {
			continue
		}
		e.SourceKey = g.keys[e.SourceTaskID]
		e.SourceTitle = g.title[e.SourceTaskID]
		out = append(out, e)
	}
	return out, nil
}

func (g *graphRelationStore) ListBlockingSources(context.Context, uuid.UUID) ([]domain.BoardTask, error) {
	return nil, nil
}

func (g *graphRelationStore) ListUnfinishedBlockers(context.Context) ([]domain.TaskRelation, error) {
	return nil, nil
}

func (g *graphRelationStore) AddBlockers(_ context.Context, targetTaskID uuid.UUID, sourceTaskIDs []uuid.UUID) ([]domain.TaskRelation, error) {
	var out []domain.TaskRelation
	for _, sourceID := range sourceTaskIDs {
		g.add(sourceID, targetTaskID, domain.TaskRelationBlocks)
		out = append(out, g.edges[len(g.edges)-1])
	}
	return out, nil
}

// memoryDocumentStore is a port.TaskDocumentStore over a per-task slice.
type memoryDocumentStore struct {
	byTask map[uuid.UUID][]domain.TaskDocument
}

func (m *memoryDocumentStore) Create(_ context.Context, doc domain.TaskDocument) (domain.TaskDocument, error) {
	doc.ID = uuid.New()
	if m.byTask == nil {
		m.byTask = map[uuid.UUID][]domain.TaskDocument{}
	}
	m.byTask[doc.TaskID] = append(m.byTask[doc.TaskID], doc)
	return doc, nil
}
func (m *memoryDocumentStore) Get(context.Context, uuid.UUID, uuid.UUID) (domain.TaskDocument, error) {
	return domain.TaskDocument{}, nil
}
func (m *memoryDocumentStore) ListByTask(_ context.Context, taskID uuid.UUID) ([]domain.TaskDocument, error) {
	return m.byTask[taskID], nil
}
func (m *memoryDocumentStore) Update(_ context.Context, doc domain.TaskDocument) (domain.TaskDocument, error) {
	return doc, nil
}
func (m *memoryDocumentStore) Delete(context.Context, uuid.UUID, uuid.UUID) error { return nil }

type orderFixture struct {
	svc       *Service
	repoID    uuid.UUID
	tasks     *fakePackageTaskStore
	relations *graphRelationStore
	documents *memoryDocumentStore
}

// newOrderFixture builds a board of tasks addressed by board key.
func newOrderFixture(t *testing.T, keys ...string) *orderFixture {
	t.Helper()
	repoID := uuid.New()
	tasks := &fakePackageTaskStore{tasks: map[uuid.UUID]domain.BoardTask{}}
	relations := newGraphRelations()
	documents := &memoryDocumentStore{byTask: map[uuid.UUID][]domain.TaskDocument{}}
	for _, key := range keys {
		id := uuid.New()
		taskType := domain.TaskTypeTask
		if strings.HasPrefix(key, "A-") {
			taskType = domain.TaskTypeAnaliz
		}
		tasks.tasks[id] = domain.BoardTask{
			ID: id, RepositoryID: repoID, Key: key, Title: "work for " + key,
			Column: domain.TaskColumnTodo, TaskType: taskType,
		}
		relations.keys[id] = key
		relations.title[id] = "work for " + key
	}
	return &orderFixture{
		svc: &Service{
			repos:     &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}},
			tasks:     tasks,
			relations: relations,
			documents: documents,
		},
		repoID: repoID, tasks: tasks, relations: relations, documents: documents,
	}
}

func (f *orderFixture) id(t *testing.T, key string) uuid.UUID {
	t.Helper()
	for id, task := range f.tasks.tasks {
		if task.Key == key {
			return id
		}
	}
	t.Fatalf("no task with key %s", key)
	return uuid.Nil
}

// ------------------------------------------------------------ relation writing

func TestSetBlockersWritesTheEdgeWithTheBlockerAsSource(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2")
	api, web := f.id(t, "T-1"), f.id(t, "T-2")

	written, err := f.svc.SetBlockers(context.Background(), web,
		[]domain.TaskRelationInput{{TargetTaskID: api}})

	require.NoError(t, err)
	require.Len(t, written, 1)
	assert.Equal(t, api, written[0].SourceTaskID, "the blocker is the source")
	assert.Equal(t, web, written[0].TargetTaskID, "the blocked task is the target")
	assert.Equal(t, domain.TaskRelationBlocks, written[0].RelationType)
}

// The same blocker named twice in one call is an intent that already holds, not
// an error.
func TestSetBlockersDeduplicatesWithinOneCall(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2")
	api, web := f.id(t, "T-1"), f.id(t, "T-2")

	written, err := f.svc.SetBlockers(context.Background(), web,
		[]domain.TaskRelationInput{{TargetTaskID: api}, {TargetTaskID: api}})

	require.NoError(t, err)
	assert.Len(t, written, 1)
}

func TestSetBlockersRefusesSelfBlocking(t *testing.T) {
	f := newOrderFixture(t, "T-1")
	api := f.id(t, "T-1")

	_, err := f.svc.SetBlockers(context.Background(), api, []domain.TaskRelationInput{{TargetTaskID: api}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked by itself")
}

// ------------------------------------------------------------- cycle refusal

// The deadlock this prevents is total: every task in the cycle parks on
// work_order forever while the sweeper confirms, once a minute, that none of
// them can start.
func TestWorkOrderCycleIsRefusedWithThePathThatCausesIt(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2", "T-3")
	one, two, three := f.id(t, "T-1"), f.id(t, "T-2"), f.id(t, "T-3")

	// T-1 blocks T-2 blocks T-3. Asking for "T-1 blocked_by T-3" closes it.
	require.NoError(t, mustBlock(f, two, one))
	require.NoError(t, mustBlock(f, three, two))

	_, err := f.svc.SetBlockers(context.Background(), one, []domain.TaskRelationInput{{TargetTaskID: three}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "work-order cycle refused")
	assert.Contains(t, err.Error(), "T-1", "the error names the task being blocked")
	assert.Contains(t, err.Error(), "T-3", "and the blocker it cannot wait for")
	assert.Contains(t, err.Error(), "→", "and the chain that closes the loop")
}

func TestWorkOrderCycleRefusalIsDirect(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2")
	one, two := f.id(t, "T-1"), f.id(t, "T-2")
	require.NoError(t, mustBlock(f, two, one)) // T-1 blocks T-2

	_, err := f.svc.SetBlockers(context.Background(), one, []domain.TaskRelationInput{{TargetTaskID: two}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "work-order cycle refused")
}

// An order with no cycle in it is still written, including one that reconverges
// (a diamond) — the visited set must not mistake that for a loop.
func TestWorkOrderDiamondIsAllowed(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2", "T-3", "T-4")
	one, two, three, four := f.id(t, "T-1"), f.id(t, "T-2"), f.id(t, "T-3"), f.id(t, "T-4")
	require.NoError(t, mustBlock(f, two, one))
	require.NoError(t, mustBlock(f, three, one))

	_, err := f.svc.SetBlockers(context.Background(), four,
		[]domain.TaskRelationInput{{TargetTaskID: two}, {TargetTaskID: three}})
	require.NoError(t, err)
}

func TestDeployOrderCycleIsRefused(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2")
	api, web := f.id(t, "T-1"), f.id(t, "T-2")

	// T-2 ships after T-1.
	_, err := f.svc.ReplaceDeployDependencies(context.Background(), web,
		[]domain.TaskRelationInput{{TargetTaskID: api}})
	require.NoError(t, err)

	// Asking for "T-1 ships after T-2" closes the loop.
	_, err = f.svc.ReplaceDeployDependencies(context.Background(), api,
		[]domain.TaskRelationInput{{TargetTaskID: web}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "deploy-order cycle refused")
	assert.Contains(t, err.Error(), "T-1")
	assert.Contains(t, err.Error(), "T-2")
}

func mustBlock(f *orderFixture, blocked, blocker uuid.UUID) error {
	_, err := f.svc.SetBlockers(context.Background(), blocked,
		[]domain.TaskRelationInput{{TargetTaskID: blocker}})
	return err
}

// ------------------------------------------------------------ the order note

// Generated from the relations, so it cannot say something the release gate
// will not enforce — and placed around whatever the agent wrote, not instead
// of it.
func TestOrderNoteIsGeneratedAndKeepsTheAgentsOwnRunbook(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2")
	api, web := f.id(t, "T-1"), f.id(t, "T-2")
	_, err := f.svc.ReplaceDeployDependencies(context.Background(), web,
		[]domain.TaskRelationInput{{TargetTaskID: api}})
	require.NoError(t, err)

	agentText := "Warm the CDN cache for /export before flipping the flag."
	task := f.tasks.tasks[web]
	task.BeforeDeploy = &agentText
	f.tasks.tasks[web] = task

	updated := f.svc.syncOrderNote(context.Background(), f.tasks.tasks[web])

	require.NotNil(t, updated.BeforeDeploy)
	got := *updated.BeforeDeploy
	assert.Contains(t, got, "Ships after: T-1 (work for T-1)")
	assert.Contains(t, got, agentText, "the agent's own runbook survives verbatim")
	assert.Contains(t, got, domain.OrderNoteOpen)
	assert.Contains(t, got, domain.OrderNoteClose)
	assert.True(t, strings.Index(got, domain.OrderNoteOpen) < strings.Index(got, agentText),
		"the precondition goes first, ahead of the checklist it applies to")
}

// The whole reason the block is fenced: regenerating replaces it rather than
// appending a second, stale one.
func TestOrderNoteIsReplacedNotAppendedWhenTheOrderChanges(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2", "T-3")
	api, web, other := f.id(t, "T-1"), f.id(t, "T-2"), f.id(t, "T-3")

	_, err := f.svc.ReplaceDeployDependencies(context.Background(), web,
		[]domain.TaskRelationInput{{TargetTaskID: api}})
	require.NoError(t, err)
	first := f.svc.syncOrderNote(context.Background(), f.tasks.tasks[web])
	f.tasks.tasks[web] = first

	_, err = f.svc.ReplaceDeployDependencies(context.Background(), web,
		[]domain.TaskRelationInput{{TargetTaskID: other}})
	require.NoError(t, err)
	second := f.svc.syncOrderNote(context.Background(), f.tasks.tasks[web])

	require.NotNil(t, second.BeforeDeploy)
	got := *second.BeforeDeploy
	assert.Contains(t, got, "T-3")
	assert.NotContains(t, got, "T-1", "the superseded ordering must not survive as a second block")
	assert.Equal(t, 1, strings.Count(got, domain.OrderNoteOpen))
}

// Work order reaches the runbook too: "who codes first" and "who ships first"
// are the same question at two moments, and a release runbook that answers only
// one of them leaves the reader to rediscover the other.
func TestOrderNoteStatesTheWorkOrderAsWellAsTheDeployOrder(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2")
	api, web := f.id(t, "T-1"), f.id(t, "T-2")
	require.NoError(t, mustBlock(f, web, api))

	updated := f.svc.syncOrderNote(context.Background(), f.tasks.tasks[web])

	require.NotNil(t, updated.BeforeDeploy)
	assert.Contains(t, *updated.BeforeDeploy, "Built after: T-1 (work for T-1)")
}

// A task with no ordering at all keeps an empty runbook — a generated block
// saying "no dependencies" would be on every card forever.
func TestOrderNoteIsSilentWhenThereIsNoOrder(t *testing.T) {
	f := newOrderFixture(t, "T-1")
	updated := f.svc.syncOrderNote(context.Background(), f.tasks.tasks[f.id(t, "T-1")])
	assert.Nil(t, updated.BeforeDeploy)
}

// The two calls triggerRelease makes, in the order it makes them: regenerate
// the note, then post the checklist. The comment must carry the generated
// ordering AND the agent's own runbook, and none of the fence markers — those
// are meaningful in a field that is regenerated, noise in a snapshot.
func TestReleaseChecklistCarriesTheGeneratedOrderingAndTheAgentsText(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2")
	comments := &fakeReleaseComments{}
	f.svc.comments = comments
	api, web := f.id(t, "T-1"), f.id(t, "T-2")
	_, err := f.svc.ReplaceDeployDependencies(context.Background(), web,
		[]domain.TaskRelationInput{{TargetTaskID: api}})
	require.NoError(t, err)

	agentText := "Confirm the export feature flag is off in prod."
	rollback := "Revert the deploy and re-run the previous image."
	task := f.tasks.tasks[web]
	task.BeforeDeploy = &agentText
	task.RollbackPlan = &rollback
	f.tasks.tasks[web] = task

	f.svc.postPreDeployChecklist(context.Background(), f.svc.syncOrderNote(context.Background(), f.tasks.tasks[web]))

	require.Len(t, comments.comments, 1)
	body := comments.comments[0].Content
	assert.Contains(t, body, "Ships after: T-1 (work for T-1)")
	assert.Contains(t, body, agentText)
	assert.Contains(t, body, rollback)
	assert.NotContains(t, body, domain.OrderNoteOpen)
	assert.NotContains(t, body, domain.OrderNoteClose)
}

// A task with an ordering but no agent-authored runbook still gets the
// checklist: the ordering alone is worth posting, and before this it was not
// posted at all because both fields were empty.
func TestReleaseChecklistIsPostedForOrderingAlone(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2")
	comments := &fakeReleaseComments{}
	f.svc.comments = comments
	api, web := f.id(t, "T-1"), f.id(t, "T-2")
	_, err := f.svc.ReplaceDeployDependencies(context.Background(), web,
		[]domain.TaskRelationInput{{TargetTaskID: api}})
	require.NoError(t, err)

	f.svc.postPreDeployChecklist(context.Background(), f.svc.syncOrderNote(context.Background(), f.tasks.tasks[web]))

	require.Len(t, comments.comments, 1)
	assert.Contains(t, comments.comments[0].Content, "Ships after: T-1")
}

// ------------------------------------------------------- the analysis reference

func TestAnalysisReferencesReturnsTheAnalizTasksDocuments(t *testing.T) {
	f := newOrderFixture(t, "A-12", "T-2")
	analysis, impl := f.id(t, "A-12"), f.id(t, "T-2")
	_, err := f.documents.Create(context.Background(), domain.TaskDocument{
		TaskID: analysis, Title: "spec: 2026-08-17 export", Content: "## Context\nCSV export.",
	})
	require.NoError(t, err)
	_, err = f.documents.Create(context.Background(), domain.TaskDocument{
		TaskID: analysis, Title: "plan: 2026-08-17 export", Content: "### Task 1\nTaskExporter.",
	})
	require.NoError(t, err)
	f.relations.add(impl, analysis, domain.TaskRelationDerivedFrom)

	refs, err := f.svc.AnalysisReferences(context.Background(), impl)

	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, "A-12", refs[0].Key)
	require.Len(t, refs[0].Documents, 2)
	assert.Equal(t, "spec: 2026-08-17 export", refs[0].Documents[0].Title)
	assert.Contains(t, refs[0].Documents[1].Content, "TaskExporter")
}

// The other relation types on the same task are not analyses.
func TestAnalysisReferencesIgnoresOrderingRelations(t *testing.T) {
	f := newOrderFixture(t, "A-12", "T-1", "T-2")
	impl := f.id(t, "T-2")
	f.relations.add(impl, f.id(t, "T-1"), domain.TaskRelationDeployDependsOn)

	refs, err := f.svc.AnalysisReferences(context.Background(), impl)

	require.NoError(t, err)
	assert.Empty(t, refs)
}

// ------------------------------------------------------------ the fence itself

func TestOrderNoteFenceRoundTrips(t *testing.T) {
	note := domain.OrderNote([]string{"T-1 (API)"}, nil)
	require.NotEmpty(t, note)

	body := "Flip the feature flag.\nWarm the cache."
	combined := domain.ApplyOrderNote(body, note)
	assert.Equal(t, body, domain.StripOrderNote(combined), "stripping the block gives the agent's text back exactly")

	// A second generation replaces rather than stacks.
	again := domain.ApplyOrderNote(combined, domain.OrderNote([]string{"T-9 (other)"}, nil))
	assert.Equal(t, 1, strings.Count(again, domain.OrderNoteOpen))
	assert.Contains(t, again, "T-9")
	assert.NotContains(t, again, "T-1 (API)")
	assert.Contains(t, again, body)
}

// A truncated field — an opening marker with no close — must not make the next
// generation append a second block. Taking the rest of the text with it is the
// safe reading: the block is regenerated anyway.
func TestOrderNoteFenceSurvivesATruncatedBlock(t *testing.T) {
	broken := "Flip the flag.\n" + domain.OrderNoteOpen + "\n- Ships after: T-1"
	out := domain.ApplyOrderNote(broken, domain.OrderNote([]string{"T-9 (other)"}, nil))
	assert.Equal(t, 1, strings.Count(out, domain.OrderNoteOpen))
	assert.Contains(t, out, "Flip the flag.")
}
