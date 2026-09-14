package repoprofile

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	apprepoprofile "github.com/makifbaysal/tasktrooper/server/internal/application/repoprofile"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeWriter struct {
	repoID uuid.UUID
	// writes records the sections that reached the service, legacyContent the
	// single-blob fallback.
	writes        []apprepoprofile.SectionWrite
	legacyContent string
	calls         int
	// reject names sections the fake service refuses, standing in for evidence
	// that does not resolve in the working copy.
	reject map[string]string
}

func (f *fakeWriter) ApplySections(_ context.Context, repositoryID uuid.UUID, writes []apprepoprofile.SectionWrite) ([]apprepoprofile.SectionResult, error) {
	f.calls++
	f.repoID = repositoryID
	results := make([]apprepoprofile.SectionResult, 0, len(writes))
	for _, w := range writes {
		if reason, bad := f.reject[w.Section]; bad {
			results = append(results, apprepoprofile.SectionResult{Section: w.Section, Reason: reason})
			continue
		}
		f.writes = append(f.writes, w)
		results = append(results, apprepoprofile.SectionResult{Section: w.Section, Accepted: true})
	}
	return results, nil
}

func (f *fakeWriter) UpdateProfile(_ context.Context, repositoryID uuid.UUID, content string) (domain.Repository, error) {
	f.calls++
	f.repoID = repositoryID
	f.legacyContent = content
	return domain.Repository{ID: repositoryID, Name: "demo"}, nil
}

func newTool(w *fakeWriter) *updateProfileTool {
	return &updateProfileTool{kit: &ToolKit{Profiles: w}}
}

func repoCtx(id uuid.UUID) context.Context {
	return registry.ContextWithRepositoryID(context.Background(), id)
}

func sectionArgs(t *testing.T, sections ...map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"sections": sections})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func section(name, body string, paths ...string) map[string]any {
	evidence := make([]map[string]any, 0, len(paths))
	for _, p := range paths {
		evidence = append(evidence, map[string]any{"path": p})
	}
	return map[string]any{"section": name, "body_md": body, "evidence": evidence}
}

// TestExecuteStoresSections: a well-formed call reaches the service with its
// evidence intact and reports what landed.
func TestExecuteStoresSections(t *testing.T) {
	w := &fakeWriter{}
	tool := newTool(w)
	id := uuid.New()

	result := tool.Execute(repoCtx(id), sectionArgs(t,
		section(domain.ProfileSectionInvariants, "every handler resolves the tenant first", "internal/http/mw.go")))
	if result.IsError {
		t.Fatalf("valid section write failed: %s", result.Content)
	}
	if w.repoID != id {
		t.Fatalf("wrote to repo %s, want %s", w.repoID, id)
	}
	if len(w.writes) != 1 || w.writes[0].Section != domain.ProfileSectionInvariants {
		t.Fatalf("service received %+v", w.writes)
	}
	if len(w.writes[0].Evidence) != 1 || w.writes[0].Evidence[0].Path != "internal/http/mw.go" {
		t.Fatalf("evidence did not reach the service: %+v", w.writes[0].Evidence)
	}
}

// TestExecuteCapsSectionBody: an over-long body is truncated rather than
// rejected — a too-long section still beats the stale one it replaces.
func TestExecuteCapsSectionBody(t *testing.T) {
	w := &fakeWriter{}
	tool := newTool(w)

	long := strings.Repeat("x", maxSectionChars+2000)
	result := tool.Execute(repoCtx(uuid.New()), sectionArgs(t, section(domain.ProfileSectionGotchas, long, "main.go")))
	if result.IsError {
		t.Fatalf("over-long body must truncate, not fail: %s", result.Content)
	}
	if len(w.writes) != 1 || len(w.writes[0].BodyMD) != maxSectionChars {
		t.Fatalf("body length = %d, want %d", len(w.writes[0].BodyMD), maxSectionChars)
	}
}

// TestExecuteReportsRejections: when every section is refused the tool result
// is an error, so the run sees the failure and can retry with better evidence.
func TestExecuteReportsRejections(t *testing.T) {
	w := &fakeWriter{reject: map[string]string{
		domain.ProfileSectionConventions: "evidence path does not exist",
	}}
	tool := newTool(w)

	result := tool.Execute(repoCtx(uuid.New()), sectionArgs(t,
		section(domain.ProfileSectionConventions, "use typescript for type safety", "src/nope.ts")))
	if !result.IsError {
		t.Fatalf("a call where nothing landed must be an error result: %s", result.Content)
	}
	if !strings.Contains(result.Content, "evidence path does not exist") {
		t.Fatalf("the rejection reason must reach the model, got %s", result.Content)
	}
}

// TestExecutePartialAcceptance: one good section and one bad one is a success
// carrying the reason for the bad one.
func TestExecutePartialAcceptance(t *testing.T) {
	w := &fakeWriter{reject: map[string]string{domain.ProfileSectionGotchas: "no evidence"}}
	tool := newTool(w)

	result := tool.Execute(repoCtx(uuid.New()), sectionArgs(t,
		section(domain.ProfileSectionPurpose, "the bridge service", "main.go"),
		section(domain.ProfileSectionGotchas, "watch out", "missing.go")))
	if result.IsError {
		t.Fatalf("a partially accepted call is not an error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "no evidence") || !strings.Contains(result.Content, domain.ProfileSectionPurpose) {
		t.Fatalf("result must name what landed and what did not: %s", result.Content)
	}
}

// TestExecuteLegacyContent: the old single-blob shape still records something,
// in the notes section, with a warning pointing at the sections argument.
func TestExecuteLegacyContent(t *testing.T) {
	w := &fakeWriter{}
	tool := newTool(w)

	raw, err := json.Marshal(map[string]string{"content": "## Purpose\nA demo repo."})
	if err != nil {
		t.Fatal(err)
	}
	result := tool.Execute(repoCtx(uuid.New()), string(raw))
	if result.IsError {
		t.Fatalf("legacy content write failed: %s", result.Content)
	}
	if w.legacyContent != "## Purpose\nA demo repo." {
		t.Fatalf("legacy content = %q", w.legacyContent)
	}
	if !strings.Contains(result.Content, "notes") || !strings.Contains(result.Content, "evidence") {
		t.Fatalf("legacy write must warn about the sections argument: %s", result.Content)
	}
}

// TestExecuteRequiresRepositoryContext: a run without a repository in context
// has nowhere to write.
func TestExecuteRequiresRepositoryContext(t *testing.T) {
	w := &fakeWriter{}
	tool := newTool(w)

	result := tool.Execute(context.Background(), sectionArgs(t, section(domain.ProfileSectionPurpose, "x", "main.go")))
	if !result.IsError {
		t.Fatal("missing repository context must fail")
	}
	if w.calls != 0 {
		t.Fatal("nothing may be written without a repository")
	}
}

// TestExecuteRequiresSections: an empty call is a mistake, not a deletion.
func TestExecuteRequiresSections(t *testing.T) {
	w := &fakeWriter{}
	tool := newTool(w)

	result := tool.Execute(repoCtx(uuid.New()), `{"sections":[]}`)
	if !result.IsError {
		t.Fatal("an empty sections list must fail")
	}
	if w.calls != 0 {
		t.Fatal("an empty call must not reach the store")
	}
}

// TestDefinitionOffersOnlyWritableSections: the schema must not advertise a
// derived section the service would refuse.
func TestDefinitionOffersOnlyWritableSections(t *testing.T) {
	def := newTool(&fakeWriter{}).Definition()
	raw, err := json.Marshal(def.Function.Parameters)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, derived := range []string{domain.ProfileSectionStack, domain.ProfileSectionCommands, domain.ProfileSectionDeploy} {
		if strings.Contains(body, `"`+derived+`"`) {
			t.Fatalf("derived section %q must not appear in the tool schema", derived)
		}
	}
	if !strings.Contains(body, domain.ProfileSectionInvariants) {
		t.Fatal("the schema must offer the agent-writable sections")
	}
}
