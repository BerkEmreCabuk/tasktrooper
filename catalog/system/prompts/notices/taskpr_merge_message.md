---
key: notices.taskpr_merge_message
version: 1
inputs: [PRNumber, BaseBranch, MergeCommitSHA, Undrafted, PreexistingNote, ReleaseNext, AutoReleased, BranchDeleted, Branch, BranchDeleteErr, RecordErr]
---
Merged pull request #{{.PRNumber}} into {{.BaseBranch}} as {{.MergeCommitSHA}} (squash).{{if .Undrafted}} The PR was still a draft and was marked ready for review first.{{end}}{{if .PreexistingNote}} {{.PreexistingNote}}.{{end}}{{if .ReleaseNext}} {{.ReleaseNext}}{{else if .AutoReleased}} This repository has no deploy target configured, so the merge released the task directly — do not call trigger_release.{{end}}{{if .BranchDeleted}} Branch {{.Branch}} deleted.{{else if .BranchDeleteErr}} The branch {{.Branch}} could NOT be deleted ({{.BranchDeleteErr}}); delete it by hand.{{end}}{{if .RecordErr}} WARNING: the merge commit could not be recorded on the task ({{.RecordErr}}), so the board may ask for this merge again — say so on the card.{{end}}
