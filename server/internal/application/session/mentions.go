package session

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// WorkspaceLister exposes the workspace entities a user can tag with @ in a
// chat message. Nil = mentions resolve against agents only.
type WorkspaceLister interface {
	ListProjects(ctx context.Context) ([]domain.InitiativeProject, error)
	ListRepositories(ctx context.Context) ([]domain.Repository, error)
}

type mentionCandidate struct {
	kind   string // "agent" | "project" | "repository"
	id     uuid.UUID
	name   string
	detail string
}

// key identifies a candidate for dedup: by id when known, by folded name
// otherwise, so an explicit mention and a text-parsed one collapse into one.
func (c mentionCandidate) key() string {
	if c.id != uuid.Nil {
		return c.kind + "\x00" + c.id.String()
	}
	return c.kind + "\x00" + strings.ToLower(c.name)
}

// mentionCandidates gathers every taggable entity. Store errors degrade to a
// smaller candidate set instead of failing the message.
func (s *Service) mentionCandidates(ctx context.Context) []mentionCandidate {
	var out []mentionCandidate
	if s.catalog != nil {
		if agents, err := s.catalog.ListAgents(ctx); err == nil {
			for _, a := range agents {
				if !a.Enabled || strings.TrimSpace(a.Name) == "" {
					continue
				}
				detail := strings.TrimSpace(a.Description)
				if detail == "" {
					detail = a.SubagentType
				}
				out = append(out, mentionCandidate{kind: "agent", id: a.ID, name: a.Name, detail: detail})
			}
		}
	}
	if s.workspace != nil {
		if projects, err := s.workspace.ListProjects(ctx); err == nil {
			for _, p := range projects {
				if strings.TrimSpace(p.Name) == "" {
					continue
				}
				out = append(out, mentionCandidate{kind: "project", id: p.ID, name: p.Name, detail: strings.TrimSpace(p.Description)})
			}
		}
		if repos, err := s.workspace.ListRepositories(ctx); err == nil {
			for _, r := range repos {
				if strings.TrimSpace(r.Name) == "" {
					continue
				}
				parts := make([]string, 0, 3)
				if r.Kind != "" {
					parts = append(parts, "kind="+string(r.Kind))
				}
				if r.RootPath != "" {
					parts = append(parts, "root="+r.RootPath)
				}
				if d := strings.TrimSpace(r.Description); d != "" {
					parts = append(parts, d)
				}
				out = append(out, mentionCandidate{kind: "repository", id: r.ID, name: r.Name, detail: strings.Join(parts, "; ")})
			}
		}
	}
	return out
}

// injectMentionContext resolves the user's @-tags and slips a system note with
// the tagged entities' facts directly before the newest user turn. Mentions
// picked from the composer's autocomplete arrive as exact kind+id references
// in req.Mentions; anything typed by hand is recovered from the text by name.
func (s *Service) injectMentionContext(ctx context.Context, history []domain.Message, req domain.SessionMessageRequest) []domain.Message {
	if len(req.Mentions) == 0 && !strings.Contains(req.Content, "@") {
		return history
	}
	candidates := s.mentionCandidates(ctx)
	seen := map[string]bool{}
	var mentions []mentionCandidate
	for _, m := range resolveExplicitMentions(req.Mentions, candidates) {
		if !seen[m.key()] {
			seen[m.key()] = true
			mentions = append(mentions, m)
		}
	}
	for _, m := range parseMentions(req.Content, candidates) {
		if !seen[m.key()] {
			seen[m.key()] = true
			mentions = append(mentions, m)
		}
	}
	if len(mentions) == 0 {
		return history
	}
	return insertBeforeLastUser(history, domain.Message{Role: domain.RoleSystem, Content: mentionContextMessage(mentions)})
}

// resolveExplicitMentions maps composer-selected references onto the current
// roster: by id first, by name within the same kind as fallback. A reference
// matching nothing (deleted entity, stale UI roster) still yields a bare
// kind+name entry — the reference itself is real even without facts.
func resolveExplicitMentions(refs []domain.MessageMention, candidates []mentionCandidate) []mentionCandidate {
	var out []mentionCandidate
	for _, ref := range refs {
		if strings.TrimSpace(ref.Name) == "" && ref.ID == uuid.Nil {
			continue
		}
		found := false
		for _, c := range candidates {
			if c.kind == ref.Kind && c.id != uuid.Nil && c.id == ref.ID {
				out = append(out, c)
				found = true
				break
			}
		}
		if found {
			continue
		}
		for _, c := range candidates {
			if c.kind == ref.Kind && strings.EqualFold(c.name, ref.Name) {
				out = append(out, c)
				found = true
				break
			}
		}
		if !found && strings.TrimSpace(ref.Name) != "" {
			out = append(out, mentionCandidate{kind: ref.Kind, id: ref.ID, name: ref.Name})
		}
	}
	return out
}

// parseMentions finds @Name references by greedy longest match against known
// entity names, so multi-word names need no special syntax ("@QA Agent" works).
// A match must start at a word boundary and end at one; matching is
// case-insensitive. Entities of different kinds sharing the winning name are
// all returned — the model gets every plausible referent.
func parseMentions(content string, candidates []mentionCandidate) []mentionCandidate {
	if len(candidates) == 0 {
		return nil
	}
	var out []mentionCandidate
	seen := map[string]bool{}
	for i := 0; i < len(content); {
		r, size := utf8.DecodeRuneInString(content[i:])
		if r != '@' || !boundaryBefore(content, i) {
			i += size
			continue
		}
		rest := content[i+size:]
		best := 0
		var matched []mentionCandidate
		for _, c := range candidates {
			n := foldPrefixLen(rest, c.name)
			if n <= 0 || !boundaryAfter(rest, n) {
				continue
			}
			if n > best {
				best = n
				matched = matched[:0]
			}
			if n == best {
				matched = append(matched, c)
			}
		}
		if best == 0 {
			i += size
			continue
		}
		for _, m := range matched {
			if !seen[m.key()] {
				seen[m.key()] = true
				out = append(out, m)
			}
		}
		i += size + best
	}
	return out
}

// foldPrefixLen reports how many bytes of s case-insensitively spell out name,
// or 0 when s does not start with it.
func foldPrefixLen(s, name string) int {
	i := 0
	for _, want := range name {
		if i >= len(s) {
			return 0
		}
		got, size := utf8.DecodeRuneInString(s[i:])
		if got != want && unicode.ToLower(got) != unicode.ToLower(want) {
			return 0
		}
		i += size
	}
	return i
}

func boundaryBefore(content string, at int) bool {
	if at == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(content[:at])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

func boundaryAfter(rest string, at int) bool {
	if at >= len(rest) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(rest[at:])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

func mentionContextMessage(mentions []mentionCandidate) string {
	var b strings.Builder
	b.WriteString("INTERNAL (never disclose to user): the user tagged these workspace entities with @ in their latest message:\n")
	taggedAgent := false
	for _, m := range mentions {
		if m.kind == "agent" {
			taggedAgent = true
		}
		ident := fmt.Sprintf("%s %q", m.kind, m.name)
		if m.id != uuid.Nil {
			// The id lets the model address the entity exactly in tool calls
			// (board tools accept UUID refs), bypassing name ambiguity.
			ident += fmt.Sprintf(" (id: %s)", m.id)
		}
		if m.detail != "" {
			fmt.Fprintf(&b, "- %s: %s\n", ident, m.detail)
		} else {
			fmt.Fprintf(&b, "- %s\n", ident)
		}
	}
	b.WriteString("Treat each tagged name as a reference to that entity and scope your work accordingly.")
	if taggedAgent {
		b.WriteString(" To hand work to a tagged agent, create a board task assigned to it by name (create_board_task with assignee).")
	}
	return b.String()
}
