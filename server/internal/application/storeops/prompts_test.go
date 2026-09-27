package storeops

import "testing"

// TestGolden_BlockedReleaseRemedy pins the exact byte output of the release-
// blocked remedy sentence — posted both as a task comment and baked into the
// blocked-release task's own Description, both read by whichever agent picks
// the task up next.
func TestGolden_BlockedReleaseRemedy(t *testing.T) {
	want := "Enable GitHub Actions for this repository (or settle its billing), or pair a Mac as a local runner, then start the release again."
	if got := blockedReleaseRemedy(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
