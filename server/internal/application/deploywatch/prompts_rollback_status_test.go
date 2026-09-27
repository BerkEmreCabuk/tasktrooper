package deploywatch

import (
	"errors"
	"fmt"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
)

// TestGolden_RollbackStatusWording pins the exact byte output of
// assertTrigger's and assertOwnsLiveRelease's status-unreadable prefixes —
// Rollback wraps the underlying error with them, and errors.Is on the
// wrapped error keeps working since only the prefix moved.
func TestGolden_RollbackStatusWording(t *testing.T) {
	underlying := errors.New("context deadline exceeded")

	got := fmt.Errorf("%s: %w", prompt.Text(rollbackStatusUnconfirmedKey), underlying)
	want := "deploy watch: could not confirm the deploy failed, refusing to roll back: context deadline exceeded"
	if got.Error() != want {
		t.Fatalf("got %q, want %q", got.Error(), want)
	}
	if !errors.Is(got, underlying) {
		t.Fatal("wrapped error lost errors.Is identity")
	}

	got2 := fmt.Errorf("%s: %w", rollbackLiveDeploymentUnreadableKey.Render(rollbackEnvInput{Env: "prod"}), underlying)
	want2 := "deploy watch: reading the live deployment for prod failed, refusing to roll back on unknown state: context deadline exceeded"
	if got2.Error() != want2 {
		t.Fatalf("got %q, want %q", got2.Error(), want2)
	}
	if !errors.Is(got2, underlying) {
		t.Fatal("wrapped error lost errors.Is identity")
	}
}
