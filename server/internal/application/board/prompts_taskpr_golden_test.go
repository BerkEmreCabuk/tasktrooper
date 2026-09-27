package board

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
)

// TestGolden_TaskPRGuardWording pins the exact byte output of the refusal
// and system-comment wording taskpr.go/taskpr_merge.go render — they reach
// the agent as a merge_task_pull_request/comment_on_pull_request tool
// result (err.Error()) or as the merge's own run summary, replayed back on
// a later run.
func TestGolden_TaskPRGuardWording(t *testing.T) {
	assert := func(got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}

	assert(mergeTaskNotDoneKey.Render(taskLabelColumnInput{Label: "T-1", Column: "in_progress"}),
		"T-1 is in `in_progress`. A pull request is merged when the board has signed the task off, not while it is still being reviewed or tested")

	assert(mergeAlreadyMergedRecordedKey.Render(taskLabelSHAInput{Label: "T-1", SHA: "abc1234"}),
		"T-1 was merged as abc1234. Nothing further is needed here")

	assert(mergeNoPRRecordedKey.Render(taskLabelInput{Label: "T-1"}),
		"T-1 has no pull request recorded, so there is nothing to merge. Its branch was never pushed, or the PR was opened outside the board")

	assert(mergePRNumberUnreadableKey.Render(mergeURLInput{URL: "https://x/pr"}),
		"the recorded pull request URL (https://x/pr) carries no readable number, so it cannot be merged through the API. Merge it by hand")

	assert(prompt.Text(mergeNotConfiguredKey), "this deployment has no GitHub pull-request access wired up")
	assert(prompt.Text(mergeGitHubNotConnectedKey), "GitHub is not connected")
	assert(prompt.Text(mergeOwnerRepoUnresolvedKey), "the GitHub owner/repo for this task could not be resolved")

	assert(mergeAlreadyMergedOnGitHubKey.Render(mergeNumberInput{Number: 7}),
		"pull request #7 is already merged on GitHub (it was merged outside this board)")

	assert(mergeClosedKey.Render(mergeNumberInput{Number: 7}),
		"pull request #7 was closed without merging. Someone decided against this change; reopening it is a human's call")

	assert(mergeBaseRedKey.Render(mergeBaseRedInput{BaseRef: "main", BaseSHA: "abc1234", FailingChecks: "build"}),
		"required checks fail on main@abc1234 too: build — pre-existing on the base branch, not introduced by this pull request")

	assert(mergeChecksNotGreenKey.Render(mergeChecksNotGreenInput{Number: 7, State: "blocked", Remedy: "do X"}),
		"GitHub reports pull request #7 as `blocked` (expected `clean`). do X")

	assert(mergeStateRemedyKey.Render(mergeStateRemedyInput{State: "blocked"}),
		"A required check is red or still running, or a required review is missing. Read the PR checks (get_task_pull_request) and send the task back to need_revision if the build is broken.")
	assert(mergeStateRemedyKey.Render(mergeStateRemedyInput{State: "unstable"}),
		"A check on this PR is failing. It is not a required one, so GitHub would merge it — this board does not: report the failing check and send the task back to need_revision if it is real.")
	assert(mergeStateRemedyKey.Render(mergeStateRemedyInput{State: "dirty"}),
		"The branch conflicts with its base. It has to be rebased or merged by whoever owns the code — send the task back to need_revision.")
	assert(mergeStateRemedyKey.Render(mergeStateRemedyInput{State: "behind"}),
		"The base branch has moved and this repository requires branches to be up to date. The branch has to be brought up to date by whoever owns the code — send the task back to need_revision.")
	assert(mergeStateRemedyKey.Render(mergeStateRemedyInput{State: "unknown"}),
		"GitHub has not finished computing this PR's mergeability. Wait a moment and read the PR again before trying once more.")
	assert(mergeStateRemedyKey.Render(mergeStateRemedyInput{State: ""}),
		"GitHub has not finished computing this PR's mergeability. Wait a moment and read the PR again before trying once more.")
	assert(mergeStateRemedyKey.Render(mergeStateRemedyInput{State: "weird"}),
		"Read the PR's checks and conversation before doing anything else.")

	assert(mergePipelineFailedKey.Render(mergePipelineFailedInput{Trigger: "code_review", Detail: ""}),
		"the last code_review pipeline for this task FAILED. Send the task back to need_revision so the developer fixes it; a red build is not merged and then fixed on the default branch")
	assert(mergePipelineFailedKey.Render(mergePipelineFailedInput{Trigger: "code_review", Detail: " Failing jobs: build."}),
		"the last code_review pipeline for this task FAILED. Failing jobs: build. Send the task back to need_revision so the developer fixes it; a red build is not merged and then fixed on the default branch")

	assert(mergeTargetUnverifiedKey.Render(mergeTargetUnverifiedInput{SHA: "abc1234"}),
		"no verified commit is stamped on this task, while its pull request is at abc1234. Move it back through review (need_revision → code_review → … → done): reaching done stamps the commit that was signed off, which is what this gate compares against")

	assert(mergeTargetMovedKey.Render(mergeTargetMovedInput{VerifiedSHA: "abc1234", HeadSHA: "def5678"}),
		"verified at abc1234, but the pull request head is now at def5678. Something was pushed after this task was signed off — send it back through review so the new commits are reviewed and QA'd; returning it to done re-stamps the verified commit")

	assert(mergePreexistingNoteKey.Render(mergePreexistingNoteInput{FailingChecks: "build", BaseRef: "main", BaseSHA: "abc1234"}),
		"merged over pre-existing failing checks: build — they fail on main@abc1234 too")

	assert(prompt.Text(commentNoPRKey),
		"this task has no pull request yet, so there is nothing to comment on — push the branch first (commit_task_changes)")
	assert(commentPRNumberUnreadableKey.Render(mergeURLInput{URL: "https://x/pr"}),
		"the recorded pull request URL (https://x/pr) carries no readable number, so the comment cannot be posted through the API")
	assert(prompt.Text(commentGitHubNotConnectedKey), "GitHub is not connected, so nothing can be posted to the pull request")

	assert(mergeMessageKey.Render(mergeMessageInput{
		PRNumber: 7, BaseBranch: "main", MergeCommitSHA: "abc1234",
	}), "Merged pull request #7 into main as abc1234 (squash).")

	assert(mergeMessageKey.Render(mergeMessageInput{
		PRNumber: 7, BaseBranch: "main", MergeCommitSHA: "abc1234",
		Undrafted: true, PreexistingNote: "merged over pre-existing failing checks: build — they fail on main@sha too",
		ReleaseNext: "Next: deploy it.", BranchDeleted: true,
	}), "Merged pull request #7 into main as abc1234 (squash). The PR was still a draft and was marked ready for review first. merged over pre-existing failing checks: build — they fail on main@sha too. Next: deploy it. Branch  deleted.")

	assert(mergeMessageKey.Render(mergeMessageInput{
		PRNumber: 7, BaseBranch: "main", MergeCommitSHA: "abc1234",
		AutoReleased: true, Branch: "feature/x", BranchDeleteErr: "still has open PRs",
	}), "Merged pull request #7 into main as abc1234 (squash). This repository has no deploy target configured, so the merge released the task directly — do not call trigger_release. The branch feature/x could NOT be deleted (still has open PRs); delete it by hand.")

	assert(mergeMessageKey.Render(mergeMessageInput{
		PRNumber: 7, BaseBranch: "main", MergeCommitSHA: "abc1234",
		BranchDeleted: true, Branch: "feature/x", RecordErr: "db down",
	}), "Merged pull request #7 into main as abc1234 (squash). Branch feature/x deleted. WARNING: the merge commit could not be recorded on the task (db down), so the board may ask for this merge again — say so on the card.")
}
