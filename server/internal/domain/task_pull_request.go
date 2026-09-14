package domain

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// ErrBoardTaskNotFound names the one reason a task read can fail that the caller
// could have avoided, so the transport can answer 404 for it without having to
// read every store error as "missing". Mirrors ErrSessionNotFound.
var ErrBoardTaskNotFound = errors.New("board task not found")

// TaskBranchName derives the branch an agent works a task on.
//
// The `feature/` prefix is load-bearing, not cosmetic: CI (`.github/workflows`)
// triggers the test suite and the auto-PR gate on `feature/**`, so a branch
// named anything else gets neither. It used to produce `task/<id8>-<slug>`,
// which silently fell outside that trigger and left agent branches untested.
// Branches already pushed under the old `task/` prefix are not renamed — an
// in-flight task simply gets a fresh branch on its next run.
//
// It lives in domain rather than in the board runner because a task's branch is
// a fact about the task, and three callers outside the runner now need it: the
// task-scoped chat (which must check out that branch instead of the shared
// mirror), the PR tools, and the commit path they share. Duplicating the naming
// logic would let two of them disagree about which branch a task owns, and the
// board's whole isolation story rests on them agreeing.
//
// The name is the task's key and nothing else. It used to carry a slug of the
// title, which is written in whatever language the board is used in: a Turkish
// title produced a branch no one else could read, and the slug dropped every
// non-ASCII letter rather than transliterating it ("wishlist kısmını kaldır" ->
// "wishlist-ksmn-kaldr"). The key is already the identifier everyone — board,
// PR, human — refers to the task by, and it is ASCII by construction. Tasks
// with a branch pushed under the old name are not renamed; they get a fresh
// branch on their next run, exactly as the `task/` -> `feature/` change did.
func TaskBranchName(task BoardTask) string {
	name := strings.ToLower(strings.TrimSpace(task.Key))
	if name == "" {
		// Key is composed from the board's prefix at read time, so a task built
		// in memory (or read by a caller that skipped the join) may not have
		// one. The number identifies the task just as well; the id is the last
		// resort, and only a zero-value task reaches it.
		if task.TaskNumber > 0 {
			name = "task-" + strconv.Itoa(task.TaskNumber)
		} else {
			name = "task-" + task.ID.String()[:8]
		}
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	cleaned := strings.Trim(b.String(), "-")
	for strings.Contains(cleaned, "--") {
		cleaned = strings.ReplaceAll(cleaned, "--", "-")
	}
	if cleaned == "" {
		cleaned = "task-" + task.ID.String()[:8]
	}
	return "feature/" + cleaned
}

// ParsePullRequestNumber pulls the PR number out of a GitHub pull-request URL
// (`https://github.com/<owner>/<repo>/pull/123`, with or without a trailing
// path such as `/files` or a `#discussion_r1` fragment).
//
// It reports ok=false instead of guessing, and every caller is expected to store
// the URL anyway: the number is what the PR API is keyed by, but the link is
// what a human clicks, and losing the link because GitHub rendered a shape this
// function does not know would be the worse failure.
func ParsePullRequestNumber(prURL string) (int, bool) {
	s := strings.TrimSpace(prURL)
	if s == "" {
		return 0, false
	}
	// Anchors and queries never carry the number; strip them before splitting so
	// ".../pull/12#discussion_r1" does not become a segment of its own.
	if i := strings.IndexAny(s, "#?"); i >= 0 {
		s = s[:i]
	}
	segments := strings.Split(strings.Trim(s, "/"), "/")
	for i := 0; i < len(segments)-1; i++ {
		// GitHub's html_url says "pull"; the API's url says "pulls". Accept both
		// so a caller that only has the API URL is not forced to rewrite it.
		if segments[i] != "pull" && segments[i] != "pulls" {
			continue
		}
		n, err := strconv.Atoi(segments[i+1])
		if err != nil || n <= 0 {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// TaskPullRequest is the task's pull request as an agent (or the chat prompt)
// reads it: the PR's own state, what it changed, what people said on it, and a
// bounded slice of the diff.
//
// Known=false is a normal answer, not an error — a task whose branch was never
// pushed has no PR, and saying so is more useful than failing the tool call.
type TaskPullRequest struct {
	Known  bool   `json:"known"`
	Number int    `json:"number,omitempty"`
	URL    string `json:"url,omitempty"`
	Title  string `json:"title,omitempty"`
	// State is GitHub's own vocabulary: "open" | "closed". Draft and Merged are
	// separate because a closed-and-merged PR and a closed-and-abandoned one
	// mean opposite things to whoever is deciding what to do next.
	State     string `json:"state,omitempty"`
	Draft     bool   `json:"draft"`
	Merged    bool   `json:"merged"`
	Mergeable string `json:"mergeable_state,omitempty"`
	HeadRef   string `json:"head_ref,omitempty"`
	BaseRef   string `json:"base_ref,omitempty"`
	HeadSHA   string `json:"head_sha,omitempty"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	// ChangedFiles is GitHub's count, which can exceed len(Files) when the file
	// listing was capped.
	ChangedFiles   int                  `json:"changed_files"`
	Files          []PullRequestFile    `json:"files,omitempty"`
	ReviewComments []PullRequestComment `json:"review_comments,omitempty"`
	Comments       []PullRequestComment `json:"comments,omitempty"`
	Diff           string               `json:"diff,omitempty"`
	// DiffTruncated says the diff below is a prefix. Without it a model reads a
	// cut-off patch as the whole change and reports files as untouched.
	DiffTruncated bool `json:"diff_truncated,omitempty"`
	// Note carries why a field is missing (no PR yet, GitHub not connected, diff
	// too large) so the agent explains it instead of inventing a reason.
	Note string `json:"note,omitempty"`
}

// PullRequestFile is one entry of a PR's changed-file list.
type PullRequestFile struct {
	Path         string `json:"path"`
	Status       string `json:"status"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	PreviousPath string `json:"previous_path,omitempty"`
}

// PullRequestComment is either a review comment (anchored to a file and line,
// possibly a reply) or a plain PR conversation comment. Path/Line/InReplyTo are
// empty on the latter, which is how a caller tells them apart.
type PullRequestComment struct {
	ID        int64     `json:"id"`
	Author    string    `json:"author,omitempty"`
	Body      string    `json:"body"`
	Path      string    `json:"path,omitempty"`
	Line      int       `json:"line,omitempty"`
	InReplyTo int64     `json:"in_reply_to,omitempty"`
	URL       string    `json:"url,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

// TaskCommitResult reports what reached origin when an agent committed the task
// workspace on a human's instruction.
//
// Committed=false with a Message is the nothing-to-commit outcome: the agent was
// asked to push changes it never made, and the honest answer is that the branch
// is unchanged rather than an error that reads like a broken repository.
type TaskCommitResult struct {
	Committed    bool     `json:"committed"`
	Branch       string   `json:"branch,omitempty"`
	SHA          string   `json:"sha,omitempty"`
	ChangedFiles []string `json:"changed_files,omitempty"`
	PRURL        string   `json:"pr_url,omitempty"`
	PRNumber     int      `json:"pr_number,omitempty"`
	Message      string   `json:"message,omitempty"`
}
