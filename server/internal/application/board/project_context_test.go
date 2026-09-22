package board

import (
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestPrependProjectContext(t *testing.T) {
	history := []domain.Message{{Role: domain.RoleUser, Content: "task"}}

	t.Run("profile is truncated to the cap", func(t *testing.T) {
		profile := strings.Repeat("z", maxInjectedProfileChars+500)
		out := prependProjectContext(history, "a demo service", profile)
		if len(out) != 2 {
			t.Fatalf("messages = %d, want context + task", len(out))
		}
		msg := out[0]
		if msg.Role != domain.RoleSystem {
			t.Fatalf("context message role = %s, want system", msg.Role)
		}
		if !strings.HasPrefix(msg.Content, "Project context: a demo service") {
			t.Fatalf("description must lead the message, got %q", msg.Content[:60])
		}
		if !strings.Contains(msg.Content, "## Project profile (maintained by agents)") {
			t.Fatal("profile header missing")
		}
		if got := strings.Count(msg.Content, "z"); got != maxInjectedProfileChars {
			t.Fatalf("injected profile chars = %d, want the %d cap", got, maxInjectedProfileChars)
		}
	})

	t.Run("profile alone injects without a description", func(t *testing.T) {
		out := prependProjectContext(history, "", "## Stack\nGo + React")
		if len(out) != 2 {
			t.Fatalf("messages = %d, want context + task", len(out))
		}
		if strings.Contains(out[0].Content, "Project context:") {
			t.Fatalf("no description was given, got %q", out[0].Content)
		}
		if !strings.Contains(out[0].Content, "Go + React") {
			t.Fatal("profile body missing")
		}
	})

	t.Run("description alone keeps the old shape", func(t *testing.T) {
		out := prependProjectContext(history, "a demo service", "")
		if len(out) != 2 {
			t.Fatalf("messages = %d, want context + task", len(out))
		}
		if out[0].Content != "Project context: a demo service" {
			t.Fatalf("description-only message = %q", out[0].Content)
		}
	})

	t.Run("nothing to inject leaves history untouched", func(t *testing.T) {
		out := prependProjectContext(history, "", "")
		if len(out) != 1 {
			t.Fatalf("messages = %d, want the task alone", len(out))
		}
	})
}
