package runtime

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestGuardProseUnchanged pins the exact wording these tools' pure helpers
// hand back to the model, ahead of moving it into catalog/system/guards.
func TestGuardProseUnchanged(t *testing.T) {
	if _, err := resolveRepositoryID(context.Background(), "", "get_environment"); err == nil ||
		err.Error() != "get_environment needs a repository in context or repository_id; this run has none" {
		t.Errorf("resolveRepositoryID error = %v", err)
	}

	repoID := uuid.New()
	store := newFakeComponentStore()
	store.put(domain.Component{RepositoryID: repoID, Path: "api", Status: domain.ComponentStatusActive})
	store.put(domain.Component{RepositoryID: repoID, Path: "web", Status: domain.ComponentStatusActive})
	if _, err := resolveComponent(context.Background(), store, repoID, ""); err == nil ||
		err.Error() != "this repository has 2 components: pass component (one of api, web)" {
		t.Errorf("resolveComponent error = %v", err)
	}

	if _, err := parseSinceDuration("5x"); err == nil ||
		err.Error() != `invalid since "5x": use a duration like "30m", "2h", "1d", or an RFC3339 timestamp such as a release's deployed_at` {
		t.Errorf("parseSinceDuration error = %v", err)
	}
}

func TestEnvironmentNotBoundMessageUnchanged(t *testing.T) {
	fx := newTestKit(t)
	repoID := uuid.New()
	comp := fx.components.put(domain.Component{RepositoryID: repoID, Path: ".", Status: domain.ComponentStatusActive})

	_, _, err := resolveBoundEnvironment(context.Background(), fx.kit, repoID, "", "")
	if err == nil {
		t.Fatal("expected an error")
	}
	want := "no production environment is bound for " + comp.DisplayName() + " — the human connects it on the repository's Deploy tab"
	if err.Error() != want {
		t.Errorf("resolveBoundEnvironment error = %q, want %q", err.Error(), want)
	}
}
