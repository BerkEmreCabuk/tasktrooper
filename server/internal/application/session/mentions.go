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

type WorkspaceLister interface {
	ListProjects(ctx context.Context) ([]domain.InitiativeProject, error)
	ListRepositories(ctx context.Context) ([]domain.Repository, error)
}

type mentionCandidate struct {
	kind   string
	id     uuid.UUID
	name   string
	detail string
}

func (c mentionCandidate) key() string {
	if c.id != uuid.Nil {
		return c.kind + "\x00" + c.id.String()
	}
	return c.kind + "\x00" + strings.ToLower(c.name)
}

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
