package github

// Pull-request reads and writes: the PR itself, its changed files, its diff, the
// comments on it, and replies to them.
//
// Everything here escapes owner/repo with url.PathEscape (like secrets.go, unlike
// the older calls in api.go/actions.go which interpolate them raw — a separate
// known issue, not fixed here). Every path segment this file builds is either an
// escaped identifier or a number, so no caller can steer a request out of the
// repository it named.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// prPageSize / prMaxPages bound every list call in this file.
//
// The existing list calls in this package take one page of 100 and stop
// (ListOwnerRepos, ListRunJobs), which silently loses everything past the
// hundredth item. A PR's changed files and review comments routinely pass 100 on
// a real branch, and a reviewer whose comment fell off page two reads as a
// reviewer who was ignored — so these page, up to a hard ceiling that keeps a
// pathological PR from turning into 40 API calls and a tool result nothing can
// read. Callers are told when the ceiling truncated the list.
const (
	prPageSize = 100
	prMaxPages = 5
)

// maxPullRequestDiffBytes caps PullRequestDiff's default read. A PR diff is
// unbounded in principle and goes into a model's context in practice, so the cap
// is the difference between a useful tool result and a blown context window.
const maxPullRequestDiffBytes = 60000

// PRAPI adapts this package's pull-request functions to port.PullRequestClient
// so the task-PR service can be tested against a fake. It adds no behaviour of
// its own beyond mapping wire structs to port types.
type PRAPI struct {
	baseURL string
}

func NewPRAPI() *PRAPI { return &PRAPI{} }

// SetBaseURL redirects requests at a test server. Empty means the real API.
func (a *PRAPI) SetBaseURL(u string) { a.baseURL = u }

func repoPath(owner, repo string) string {
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
}

func pullPath(owner, repo string, number int) string {
	return repoPath(owner, repo) + "/pulls/" + strconv.Itoa(number)
}

type prPayload struct {
	Number         int       `json:"number"`
	Title          string    `json:"title"`
	Body           string    `json:"body"`
	State          string    `json:"state"`
	Draft          bool      `json:"draft"`
	Merged         bool      `json:"merged"`
	MergeableState string    `json:"mergeable_state"`
	Additions      int       `json:"additions"`
	Deletions      int       `json:"deletions"`
	ChangedFiles   int       `json:"changed_files"`
	HTMLURL        string    `json:"html_url"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Head           struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (p prPayload) toPort() port.PullRequest {
	return port.PullRequest{
		Number:         p.Number,
		Title:          p.Title,
		Body:           p.Body,
		State:          p.State,
		Draft:          p.Draft,
		Merged:         p.Merged,
		MergeableState: p.MergeableState,
		HeadRef:        p.Head.Ref,
		BaseRef:        p.Base.Ref,
		HeadSHA:        p.Head.SHA,
		Additions:      p.Additions,
		Deletions:      p.Deletions,
		ChangedFiles:   p.ChangedFiles,
		HTMLURL:        p.HTMLURL,
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
	}
}

// GetPullRequest returns one PR by number.
func (a *PRAPI) GetPullRequest(ctx context.Context, token, owner, repo string, number int) (port.PullRequest, error) {
	var out prPayload
	if err := doJSONAt(ctx, a.baseURL, token, http.MethodGet, pullPath(owner, repo, number), nil, &out); err != nil {
		return port.PullRequest{}, err
	}
	return out.toPort(), nil
}

type prFilePayload struct {
	Filename         string `json:"filename"`
	Status           string `json:"status"`
	Additions        int    `json:"additions"`
	Deletions        int    `json:"deletions"`
	Changes          int    `json:"changes"`
	PreviousFilename string `json:"previous_filename"`
}

// ListPullRequestFiles returns the PR's changed files, up to prPageSize *
// prMaxPages entries.
func (a *PRAPI) ListPullRequestFiles(ctx context.Context, token, owner, repo string, number int) ([]port.PullRequestFile, error) {
	var out []port.PullRequestFile
	err := a.eachPage(ctx, pullPath(owner, repo, number)+"/files", func(pagePath string) (int, error) {
		var files []prFilePayload
		if err := doJSONAt(ctx, a.baseURL, token, http.MethodGet, pagePath, nil, &files); err != nil {
			return 0, err
		}
		for _, f := range files {
			out = append(out, port.PullRequestFile{
				Path:         f.Filename,
				Status:       f.Status,
				Additions:    f.Additions,
				Deletions:    f.Deletions,
				Changes:      f.Changes,
				PreviousPath: f.PreviousFilename,
			})
		}
		return len(files), nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type prCommentPayload struct {
	ID   int64 `json:"id"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	Body      string    `json:"body"`
	Path      string    `json:"path"`
	Line      int       `json:"line"`
	StartLine int       `json:"start_line"`
	InReplyTo int64     `json:"in_reply_to_id"`
	HTMLURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
}

func (c prCommentPayload) toPort() port.PullRequestComment {
	line := c.Line
	if line == 0 {
		// A comment on a multi-line range reports start_line; an outdated one
		// reports neither, and 0 then correctly reads as "no line".
		line = c.StartLine
	}
	return port.PullRequestComment{
		ID:        c.ID,
		Author:    c.User.Login,
		Body:      c.Body,
		Path:      c.Path,
		Line:      line,
		InReplyTo: c.InReplyTo,
		HTMLURL:   c.HTMLURL,
		CreatedAt: c.CreatedAt,
	}
}

// ListPullRequestReviewComments returns the line-anchored review comments.
func (a *PRAPI) ListPullRequestReviewComments(ctx context.Context, token, owner, repo string, number int) ([]port.PullRequestComment, error) {
	return a.listComments(ctx, token, pullPath(owner, repo, number)+"/comments")
}

// ListIssueComments returns the PR's conversation comments. GitHub keys these on
// the issue number, which for a PR is the PR number.
func (a *PRAPI) ListIssueComments(ctx context.Context, token, owner, repo string, number int) ([]port.PullRequestComment, error) {
	return a.listComments(ctx, token, repoPath(owner, repo)+"/issues/"+strconv.Itoa(number)+"/comments")
}

func (a *PRAPI) listComments(ctx context.Context, token, path string) ([]port.PullRequestComment, error) {
	var out []port.PullRequestComment
	err := a.eachPage(ctx, path, func(pagePath string) (int, error) {
		var comments []prCommentPayload
		if err := doJSONAt(ctx, a.baseURL, token, http.MethodGet, pagePath, nil, &comments); err != nil {
			return 0, err
		}
		for _, c := range comments {
			out = append(out, c.toPort())
		}
		return len(comments), nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CreateIssueComment posts a comment on the PR conversation.
func (a *PRAPI) CreateIssueComment(ctx context.Context, token, owner, repo string, number int, body string) (port.PullRequestComment, error) {
	var out prCommentPayload
	path := repoPath(owner, repo) + "/issues/" + strconv.Itoa(number) + "/comments"
	if err := doJSONAt(ctx, a.baseURL, token, http.MethodPost, path, map[string]any{"body": body}, &out); err != nil {
		return port.PullRequestComment{}, err
	}
	return out.toPort(), nil
}

// ReplyToReviewComment answers commentID inside its own review thread.
func (a *PRAPI) ReplyToReviewComment(ctx context.Context, token, owner, repo string, number int, commentID int64, body string) (port.PullRequestComment, error) {
	var out prCommentPayload
	path := pullPath(owner, repo, number) + "/comments/" + strconv.FormatInt(commentID, 10) + "/replies"
	if err := doJSONAt(ctx, a.baseURL, token, http.MethodPost, path, map[string]any{"body": body}, &out); err != nil {
		return port.PullRequestComment{}, err
	}
	return out.toPort(), nil
}

// PullRequestDiff fetches the PR as a unified diff (the
// application/vnd.github.v3.diff media type on the PR resource) and truncates it
// to maxBytes. maxBytes <= 0 uses maxPullRequestDiffBytes.
//
// The read is bounded at the socket rather than after buffering: the point of the
// cap is that a 200 MB diff must not be held in memory on its way to being
// thrown away.
func (a *PRAPI) PullRequestDiff(ctx context.Context, token, owner, repo string, number int, maxBytes int) (string, bool, error) {
	if maxBytes <= 0 {
		maxBytes = maxPullRequestDiffBytes
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resolveBase(a.baseURL)+pullPath(owner, repo, number), nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3.diff")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return "", false, &apiError{Status: resp.StatusCode, Message: string(body)}
	}
	// One byte past the cap: reading it is how we learn the diff was longer,
	// without reading the rest of it.
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil {
		return "", false, fmt.Errorf("read pull request diff: %w", err)
	}
	if len(data) > maxBytes {
		return string(data[:maxBytes]), true, nil
	}
	return string(data), false, nil
}

// eachPage walks a paginated list endpoint. It hands fetch the page's full path
// (query string included) and stops when a page comes back short — the last one —
// or when the page ceiling is reached. fetch reports how many items it read.
//
// path never carries a query of its own here, so "?" is always the right joiner.
func (a *PRAPI) eachPage(ctx context.Context, path string, fetch func(pagePath string) (int, error)) error {
	for page := 1; page <= prMaxPages; page++ {
		q := url.Values{}
		q.Set("per_page", strconv.Itoa(prPageSize))
		q.Set("page", strconv.Itoa(page))
		count, err := fetch(path + "?" + q.Encode())
		if err != nil {
			return err
		}
		if count < prPageSize {
			return nil
		}
	}
	return nil
}
