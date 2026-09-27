package board

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// This file registers every LLM-facing string taskpr.go and taskpr_merge.go
// render — see catalog/system/guards/taskpr_*.md and
// catalog/system/prompts/notices/taskpr_merge_*.md. Nothing here is a Go
// string literal; each Key's sample only has to be renderable, not
// realistic.

type taskLabelColumnInput struct{ Label, Column string }

var mergeTaskNotDoneKey = prompt.Define("guard.taskpr_merge_task_not_done", taskLabelColumnInput{Label: "T-1", Column: "in_progress"})

type taskLabelSHAInput struct{ Label, SHA string }

var mergeAlreadyMergedRecordedKey = prompt.Define("guard.taskpr_merge_already_merged_recorded", taskLabelSHAInput{Label: "T-1", SHA: "abc1234"})

type taskLabelInput struct{ Label string }

var mergeNoPRRecordedKey = prompt.Define("guard.taskpr_merge_no_pr_recorded", taskLabelInput{Label: "T-1"})

type mergeURLInput struct{ URL string }

var mergePRNumberUnreadableKey = prompt.Define("guard.taskpr_merge_pr_number_unreadable", mergeURLInput{URL: "https://example.com/pr/1"})

var mergeNotConfiguredKey = prompt.Define("guard.taskpr_merge_not_configured", struct{}{})
var mergeGitHubNotConnectedKey = prompt.Define("guard.taskpr_merge_github_not_connected", struct{}{})
var mergeOwnerRepoUnresolvedKey = prompt.Define("guard.taskpr_merge_owner_repo_unresolved", struct{}{})

type mergeNumberInput struct{ Number int }

var mergeAlreadyMergedOnGitHubKey = prompt.Define("guard.taskpr_merge_already_merged_on_github", mergeNumberInput{Number: 1})
var mergeClosedKey = prompt.Define("guard.taskpr_merge_closed", mergeNumberInput{Number: 1})

type mergeBaseRedInput struct{ BaseRef, BaseSHA, FailingChecks string }

var mergeBaseRedKey = prompt.Define("guard.taskpr_merge_base_red", mergeBaseRedInput{BaseRef: "main", BaseSHA: "abc1234", FailingChecks: "build"})

type mergeChecksNotGreenInput struct {
	Number int
	State  string
	Remedy string
}

var mergeChecksNotGreenKey = prompt.Define("guard.taskpr_merge_checks_not_green", mergeChecksNotGreenInput{Number: 1, State: "blocked", Remedy: "x"})

type mergeStateRemedyInput struct{ State string }

var mergeStateRemedyKey = prompt.Define("guard.taskpr_merge_state_remedy", mergeStateRemedyInput{State: "blocked"})

type mergePipelineFailedInput struct{ Trigger, Detail string }

var mergePipelineFailedKey = prompt.Define("guard.taskpr_merge_pipeline_failed", mergePipelineFailedInput{Trigger: "code_review"})

type mergeTargetUnverifiedInput struct{ SHA string }

var mergeTargetUnverifiedKey = prompt.Define("guard.taskpr_merge_target_unverified", mergeTargetUnverifiedInput{SHA: "abc1234"})

type mergeTargetMovedInput struct{ VerifiedSHA, HeadSHA string }

var mergeTargetMovedKey = prompt.Define("guard.taskpr_merge_target_moved", mergeTargetMovedInput{VerifiedSHA: "abc1234", HeadSHA: "def5678"})

var commentNoPRKey = prompt.Define("guard.taskpr_comment_no_pr", struct{}{})
var commentPRNumberUnreadableKey = prompt.Define("guard.taskpr_comment_pr_number_unreadable", mergeURLInput{URL: "https://example.com/pr/1"})
var commentGitHubNotConnectedKey = prompt.Define("guard.taskpr_comment_github_not_connected", struct{}{})

type mergePreexistingNoteInput struct{ FailingChecks, BaseRef, BaseSHA string }

var mergePreexistingNoteKey = prompt.Define("notices.taskpr_merge_preexisting_note", mergePreexistingNoteInput{
	FailingChecks: "build", BaseRef: "main", BaseSHA: "abc1234",
})

type mergeMessageInput struct {
	PRNumber        int
	BaseBranch      string
	MergeCommitSHA  string
	Undrafted       bool
	PreexistingNote string
	ReleaseNext     string
	AutoReleased    bool
	BranchDeleted   bool
	Branch          string
	BranchDeleteErr string
	RecordErr       string
}

var mergeMessageKey = prompt.Define("notices.taskpr_merge_message", mergeMessageInput{
	PRNumber: 1, BaseBranch: "main", MergeCommitSHA: "abc1234",
})
