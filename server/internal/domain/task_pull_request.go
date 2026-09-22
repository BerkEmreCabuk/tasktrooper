package domain

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

var ErrBoardTaskNotFound = errors.New("board task not found")

func TaskBranchName(task BoardTask) string {
	name := strings.ToLower(strings.TrimSpace(task.Key))
	if name == "" {
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

func ParsePullRequestNumber(prURL string) (int, bool) {
	s := strings.TrimSpace(prURL)
	if s == "" {
		return 0, false
	}
	if i := strings.IndexAny(s, "#?"); i >= 0 {
		s = s[:i]
	}
	segments := strings.Split(strings.Trim(s, "/"), "/")
	for i := 0; i < len(segments)-1; i++ {
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

type TaskPullRequest struct {
	Known          bool                 `json:"known"`
	Number         int                  `json:"number,omitempty"`
	URL            string               `json:"url,omitempty"`
	Title          string               `json:"title,omitempty"`
	State          string               `json:"state,omitempty"`
	Draft          bool                 `json:"draft"`
	Merged         bool                 `json:"merged"`
	Mergeable      string               `json:"mergeable_state,omitempty"`
	HeadRef        string               `json:"head_ref,omitempty"`
	BaseRef        string               `json:"base_ref,omitempty"`
	HeadSHA        string               `json:"head_sha,omitempty"`
	Additions      int                  `json:"additions"`
	Deletions      int                  `json:"deletions"`
	ChangedFiles   int                  `json:"changed_files"`
	Files          []PullRequestFile    `json:"files,omitempty"`
	ReviewComments []PullRequestComment `json:"review_comments,omitempty"`
	Comments       []PullRequestComment `json:"comments,omitempty"`
	Diff           string               `json:"diff,omitempty"`
	DiffTruncated  bool                 `json:"diff_truncated,omitempty"`
	Note           string               `json:"note,omitempty"`
}

type PullRequestFile struct {
	Path         string `json:"path"`
	Status       string `json:"status"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	PreviousPath string `json:"previous_path,omitempty"`
}

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

type TaskCommitResult struct {
	Committed    bool     `json:"committed"`
	Branch       string   `json:"branch,omitempty"`
	SHA          string   `json:"sha,omitempty"`
	ChangedFiles []string `json:"changed_files,omitempty"`
	PRURL        string   `json:"pr_url,omitempty"`
	PRNumber     int      `json:"pr_number,omitempty"`
	Message      string   `json:"message,omitempty"`
}
