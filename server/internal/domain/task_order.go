package domain

import (
	"strings"

	"github.com/google/uuid"
)

// The order note is the generated half of a task's pre-deploy runbook: the
// sentence that says this task ships after another one, written from the
// relations rather than from an agent's memory of them.
//
// It lives INSIDE board_tasks.before_deploy rather than beside it, fenced by
// these two markers, because that field is what the release path reads, what
// the pre-deploy checklist posts and what the SPA renders — a second column
// would have to be plumbed into all three and would still be missed by anyone
// reading the runbook. Fencing is what makes the field safe to regenerate: the
// generator replaces only what is between the markers and never touches a
// character the agent wrote outside them.
//
// The markers are HTML comments so they vanish in every markdown renderer the
// runbook passes through, and they are literal — no regex — so text that merely
// resembles them cannot be mistaken for a fence.
const (
	OrderNoteOpen  = "<!-- tt:order -->"
	OrderNoteClose = "<!-- /tt:order -->"
)

// OrderNote renders the generated block for a task, or "" when the task
// declares no ordering at all.
//
// deployAfter names the tasks that must be live in production first
// (deploy_depends_on); workAfter names the tasks that must be finished before
// this one may be worked on (the `blocks` rows pointing at it). Both are
// rendered because both are the same question asked at two moments — which task
// goes first — and a release runbook that states only half of it leaves whoever
// reads it to rediscover the other half.
func OrderNote(deployAfter, workAfter []string) string {
	var lines []string
	if len(deployAfter) > 0 {
		lines = append(lines, "- Ships after: "+strings.Join(deployAfter, ", ")+
			". Each one must be live in production before this task is released; the release is refused otherwise.")
	}
	if len(workAfter) > 0 {
		lines = append(lines, "- Built after: "+strings.Join(workAfter, ", ")+
			". Work on this task does not start until those are done.")
	}
	if len(lines) == 0 {
		return ""
	}
	return OrderNoteOpen + "\n**Release order (generated from this task's relations — do not edit by hand):**\n" +
		strings.Join(lines, "\n") + "\n" + OrderNoteClose
}

// StripOrderNote removes a previously generated block, leaving everything a
// human or an agent wrote around it.
//
// An unterminated opening marker takes the rest of the text with it. That is
// the safe reading of a truncated field: the alternative — giving up and
// leaving the marker in place — would make the next generation append a second
// block, and the field would grow one stale ordering statement per release.
func StripOrderNote(text string) string {
	for {
		start := strings.Index(text, OrderNoteOpen)
		if start < 0 {
			return strings.TrimSpace(text)
		}
		rest := text[start+len(OrderNoteOpen):]
		end := strings.Index(rest, OrderNoteClose)
		if end < 0 {
			text = text[:start]
			continue
		}
		text = text[:start] + rest[end+len(OrderNoteClose):]
	}
}

// ApplyOrderNote returns the runbook text with the generated block refreshed:
// any previous block removed, the new one placed first, and every other line
// kept verbatim.
//
// First rather than last on purpose. "This ships after T-12" is a precondition
// for the whole checklist below it, and a reader who stops after the first
// screen of a long runbook must not be the one who misses it.
func ApplyOrderNote(existing, note string) string {
	body := StripOrderNote(existing)
	switch {
	case note == "" && body == "":
		return ""
	case note == "":
		return body
	case body == "":
		return note
	default:
		return note + "\n\n" + body
	}
}

// AnalysisReference is one analiz task an implementation task was opened out of,
// together with the documents that analysis produced.
//
// It is a domain type rather than a repository-package one so the board runner
// can put it in front of a run without importing the application service that
// assembles it — the same reason RunJob carries a BoardTask rather than a
// service handle.
type AnalysisReference struct {
	TaskID    uuid.UUID      `json:"task_id"`
	Key       string         `json:"key"`
	Title     string         `json:"title"`
	Documents []TaskDocument `json:"documents"`
}
