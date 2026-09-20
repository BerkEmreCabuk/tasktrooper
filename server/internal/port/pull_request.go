package port

import (
	"context"
	"time"
)

// PullRequest is one pull request as the PR client reports it.
type PullRequest struct {
	Number int
	Title  string
	Body   string
	State  string // "open" | "closed"
	Draft  bool
	Merged bool
	// MergeableState is GitHub's own summary ("clean", "dirty", "blocked",
	// "behind", "unknown"). It is the field that answers "can this be merged
	// yet?", which is most of what a human asks about an open PR.
	MergeableState string
	HeadRef        string
	BaseRef        string
	HeadSHA        string
	Additions      int
	Deletions      int
	ChangedFiles   int
	HTMLURL        string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// PullRequestFile is one changed file of a pull request.
type PullRequestFile struct {
	Path         string
	Status       string // added | modified | removed | renamed | copied | changed
	Additions    int
	Deletions    int
	Changes      int
	PreviousPath string
}

// PullRequestComment covers both kinds of comment a PR carries: a review comment
// anchored to a file and line (Path/Line set, InReplyTo set when it is a reply in
// a thread) and a plain conversation comment (neither set).
type PullRequestComment struct {
	ID        int64
	Author    string
	Body      string
	Path      string
	Line      int
	InReplyTo int64
	HTMLURL   string
	CreatedAt time.Time
}

// PullRequestClient is what the board needs from GitHub's pull-request API: read
// a PR and the conversation on it, and answer that conversation. The concrete
// implementation wraps the free functions in internal/adapter/vcs/github; this
// interface exists so the task-PR service is testable without a token or a
// network (same arrangement as ActionsClient).
//
// Every method takes the token per call rather than holding one, because the
// stored GitHub token is read fresh from settings on each use — a client that
// captured it at construction would keep using a token the user had revoked.
type PullRequestClient interface {
	GetPullRequest(ctx context.Context, token, owner, repo string, number int) (PullRequest, error)
	// ListPullRequestFiles returns the changed files, capped by the
	// implementation so a 4,000-file PR cannot become a tool result.
	ListPullRequestFiles(ctx context.Context, token, owner, repo string, number int) ([]PullRequestFile, error)
	// PullRequestDiff returns the unified diff, truncated to maxBytes. truncated
	// reports whether anything was cut, so the caller can say so instead of
	// letting a model read a prefix as the whole change.
	PullRequestDiff(ctx context.Context, token, owner, repo string, number int, maxBytes int) (diff string, truncated bool, err error)
	// ListPullRequestReviewComments returns the line-anchored review comments —
	// the ones that actually say what to change.
	ListPullRequestReviewComments(ctx context.Context, token, owner, repo string, number int) ([]PullRequestComment, error)
	// ListIssueComments returns the PR's conversation comments (GitHub models a
	// PR as an issue for these, hence the name).
	ListIssueComments(ctx context.Context, token, owner, repo string, number int) ([]PullRequestComment, error)
	CreateIssueComment(ctx context.Context, token, owner, repo string, number int, body string) (PullRequestComment, error)
	// ReplyToReviewComment answers one review comment inside its own thread,
	// which is where a reviewer looks for the answer — a new conversation comment
	// would leave the thread unanswered.
	ReplyToReviewComment(ctx context.Context, token, owner, repo string, number int, commentID int64, body string) (PullRequestComment, error)
}
