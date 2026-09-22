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
	// GitHub's own summary ("clean", "dirty", "blocked", "behind", "unknown").
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

// PullRequestComment covers both kinds of comment a PR carries: a review
// comment anchored to a file and line, and a plain conversation comment.
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

// PullRequestClient is what the board needs from GitHub's pull-request API:
// read a PR and the conversation on it, and answer that conversation. Every
// method takes the token per call because the stored GitHub token is read
// fresh from settings on each use.
type PullRequestClient interface {
	GetPullRequest(ctx context.Context, token, owner, repo string, number int) (PullRequest, error)
	// Capped by the implementation so a 4,000-file PR cannot become a tool result.
	ListPullRequestFiles(ctx context.Context, token, owner, repo string, number int) ([]PullRequestFile, error)
	// Returns the unified diff truncated to maxBytes; truncated reports whether
	// anything was cut, so the caller can say so.
	PullRequestDiff(ctx context.Context, token, owner, repo string, number int, maxBytes int) (diff string, truncated bool, err error)
	ListPullRequestReviewComments(ctx context.Context, token, owner, repo string, number int) ([]PullRequestComment, error)
	// The PR's conversation comments — GitHub models a PR as an issue for these,
	// hence the name.
	ListIssueComments(ctx context.Context, token, owner, repo string, number int) ([]PullRequestComment, error)
	CreateIssueComment(ctx context.Context, token, owner, repo string, number int, body string) (PullRequestComment, error)
	// Answers one review comment inside its own thread, where the reviewer
	// looks for the answer.
	ReplyToReviewComment(ctx context.Context, token, owner, repo string, number int, commentID int64, body string) (PullRequestComment, error)
}
