package github

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

const (
	prPageSize = 100
	prMaxPages = 5
)

const maxPullRequestDiffBytes = 60000

type PRAPI struct {
	baseURL string
}

func NewPRAPI() *PRAPI { return &PRAPI{} }

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

func (a *PRAPI) ListPullRequestReviewComments(ctx context.Context, token, owner, repo string, number int) ([]port.PullRequestComment, error) {
	return a.listComments(ctx, token, pullPath(owner, repo, number)+"/comments")
}

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

func (a *PRAPI) CreateIssueComment(ctx context.Context, token, owner, repo string, number int, body string) (port.PullRequestComment, error) {
	var out prCommentPayload
	path := repoPath(owner, repo) + "/issues/" + strconv.Itoa(number) + "/comments"
	if err := doJSONAt(ctx, a.baseURL, token, http.MethodPost, path, map[string]any{"body": body}, &out); err != nil {
		return port.PullRequestComment{}, err
	}
	return out.toPort(), nil
}

func (a *PRAPI) ReplyToReviewComment(ctx context.Context, token, owner, repo string, number int, commentID int64, body string) (port.PullRequestComment, error) {
	var out prCommentPayload
	path := pullPath(owner, repo, number) + "/comments/" + strconv.FormatInt(commentID, 10) + "/replies"
	if err := doJSONAt(ctx, a.baseURL, token, http.MethodPost, path, map[string]any{"body": body}, &out); err != nil {
		return port.PullRequestComment{}, err
	}
	return out.toPort(), nil
}

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

	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil {
		return "", false, fmt.Errorf("read pull request diff: %w", err)
	}
	if len(data) > maxBytes {
		return string(data[:maxBytes]), true, nil
	}
	return string(data), false, nil
}

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
