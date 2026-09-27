package release

import "testing"

// TestGolden_AfterDeployComment pins the exact byte output of the
// after-deploy-steps comment postAfterDeployComments posts once a release's
// tasks move to released — read by whichever agent picks the task up next.
func TestGolden_AfterDeployComment(t *testing.T) {
	want := "Released — do these after-deploy steps now: Enable the new_pricing flag for 100% of traffic."
	got := afterDeployCommentKey.Render(afterDeployCommentInput{Steps: "Enable the new_pricing flag for 100% of traffic."})
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
