package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// A desktop install serves on 127.0.0.1, and GitHub answers a hook pointed
// there with 422 Validation Failed. Setup, the import-time install and the boot
// reconcile all have to recognise such an address up front instead.
func TestWebhooksReachable(t *testing.T) {
	cases := map[string]bool{
		"":                          false,
		"http://127.0.0.1:52341":    false,
		"http://localhost:8085":     false,
		"http://app.localhost":      false,
		"http://mac-mini.local:80":  false,
		"http://[::1]:8085":         false,
		"http://0.0.0.0:8085":       false,
		"http://192.168.1.107:8085": false,
		"http://10.0.0.4":           false,
		"http://172.20.1.9":         false,
		"http://169.254.10.1":       false,
		"https://tasktrooper.ai":    true,
		"https://bridge.example":    true,
		"https://203.0.113.10":      true,
	}
	for base, want := range cases {
		if got := webhooksReachable(base); got != want {
			t.Errorf("webhooksReachable(%q) = %v, want %v", base, got, want)
		}
	}
}

// The manual "set up webhook" call on a loopback instance must say why it
// cannot work instead of forwarding GitHub's 422.
func TestSetupWebhookRefusesLoopbackBaseURL(t *testing.T) {
	repoID := uuid.New()
	store := &fakeWebhookRepositoryStore{repo: domain.Repository{ID: repoID, RemoteURL: "https://github.com/o/r"}}
	s := &Service{repos: store}
	s.githubToken = func(context.Context) (string, error) { return "tok", nil }
	s.SetPublicBaseURL("http://127.0.0.1:52341/")

	if s.WebhooksReachable() {
		t.Fatal("a loopback base URL must not count as reachable")
	}
	_, err := s.SetupWebhook(context.Background(), repoID)
	if err == nil || !strings.Contains(err.Error(), "polling") {
		t.Fatalf("err = %v, want the polling explanation", err)
	}
}
