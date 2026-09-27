package workspace

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
)

// TestGolden_TransitionNotAllowed pins ValidateTransition's refusal wording
// — it reaches an agent through repository.Service's move_board_task/
// update_task path.
func TestGolden_TransitionNotAllowed(t *testing.T) {
	want := "moving from this column to the target column is not allowed (workflow)"
	if got := prompt.Text(transitionNotAllowedKey); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
