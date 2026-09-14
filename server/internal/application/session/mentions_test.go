package session

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestParseMentions(t *testing.T) {
	candidates := []mentionCandidate{
		{kind: "agent", name: "QA", detail: "tests things"},
		{kind: "agent", name: "QA Agent", detail: "runs QA on tasks"},
		{kind: "agent", name: "Ürün Yöneticisi", detail: "product manager"},
		{kind: "repository", name: "backend", detail: "kind=service"},
		{kind: "project", name: "backend", detail: "backend initiative"},
		{kind: "repository", name: "local-llm", detail: "root=/srv/local-llm"},
	}

	cases := []struct {
		name    string
		content string
		want    []string // "kind:name"
	}{
		{"simple", "ask @QA about it", []string{"agent:QA"}},
		{"longest match wins over prefix", "@QA Agent please review", []string{"agent:QA Agent"}},
		{"case insensitive", "ping @qa agent now", []string{"agent:QA Agent"}},
		{"turkish case fold", "@ürün yöneticisi ile konuş", []string{"agent:Ürün Yöneticisi"}},
		{"same name both kinds", "deploy @backend today", []string{"repository:backend", "project:backend"}},
		{"punctuation boundary", "check @local-llm, then report", []string{"repository:local-llm"}},
		{"email is not a mention", "mail me at qa@backend.example", nil},
		{"no boundary after", "look at @backends", nil},
		{"duplicate collapses", "@QA and again @QA", []string{"agent:QA"}},
		{"multiple mentions", "@QA check @local-llm", []string{"agent:QA", "repository:local-llm"}},
		{"unknown name", "hello @nobody", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseMentions(tc.content, candidates)
			keys := make([]string, 0, len(got))
			for _, m := range got {
				keys = append(keys, m.kind+":"+m.name)
			}
			if len(keys) != len(tc.want) {
				t.Fatalf("got %v, want %v", keys, tc.want)
			}
			for i := range keys {
				if keys[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", keys, tc.want)
				}
			}
		})
	}
}

func TestResolveExplicitMentions(t *testing.T) {
	projectID := uuid.New()
	repoID := uuid.New()
	candidates := []mentionCandidate{
		{kind: "project", id: projectID, name: "acme", detail: "mobile initiative"},
		{kind: "repository", id: repoID, name: "acme", detail: "kind=service"},
	}

	// Same name, different kinds: the id picks the exact entity.
	got := resolveExplicitMentions([]domain.MessageMention{{Kind: "project", ID: projectID, Name: "acme"}}, candidates)
	if len(got) != 1 || got[0].kind != "project" || got[0].id != projectID || got[0].detail != "mobile initiative" {
		t.Fatalf("id match failed: %+v", got)
	}

	// Unknown id falls back to a name match within the same kind.
	got = resolveExplicitMentions([]domain.MessageMention{{Kind: "repository", ID: uuid.New(), Name: "Acme"}}, candidates)
	if len(got) != 1 || got[0].id != repoID {
		t.Fatalf("name fallback failed: %+v", got)
	}

	// Nothing matches: keep a bare reference rather than dropping it.
	got = resolveExplicitMentions([]domain.MessageMention{{Kind: "agent", ID: uuid.New(), Name: "Ghost"}}, candidates)
	if len(got) != 1 || got[0].name != "Ghost" || got[0].detail != "" {
		t.Fatalf("bare reference failed: %+v", got)
	}

	// Empty reference is ignored.
	if got = resolveExplicitMentions([]domain.MessageMention{{Kind: "agent"}}, candidates); len(got) != 0 {
		t.Fatalf("empty ref must be dropped: %+v", got)
	}
}

func TestMentionContextMessageIncludesID(t *testing.T) {
	id := uuid.New()
	msg := mentionContextMessage([]mentionCandidate{{kind: "project", id: id, name: "acme", detail: "mobile"}})
	if !strings.Contains(msg, "(id: "+id.String()+")") {
		t.Fatalf("message missing entity id:\n%s", msg)
	}
}

func TestMentionContextMessage(t *testing.T) {
	msg := mentionContextMessage([]mentionCandidate{
		{kind: "agent", name: "QA Agent", detail: "runs QA"},
		{kind: "repository", name: "local-llm"},
	})
	for _, want := range []string{"INTERNAL", `agent "QA Agent": runs QA`, `repository "local-llm"`, "create_board_task"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, `repository "local-llm":`) {
		t.Fatalf("empty detail must not leave a trailing colon:\n%s", msg)
	}

	noAgent := mentionContextMessage([]mentionCandidate{{kind: "project", name: "X"}})
	if strings.Contains(noAgent, "create_board_task") {
		t.Fatalf("agent handoff hint must only appear when an agent is tagged:\n%s", noAgent)
	}
}
