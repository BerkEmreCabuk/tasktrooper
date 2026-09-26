package github

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// failingCheckConclusions are the Checks API conclusions that read as red;
// "success", "neutral", "skipped" and an empty (still running) conclusion do
// not.
var failingCheckConclusions = map[string]bool{
	"failure":         true,
	"timed_out":       true,
	"cancelled":       true,
	"action_required": true,
	"startup_failure": true,
}

// failingStatusStates are the commit-status states that read as red.
var failingStatusStates = map[string]bool{
	"failure": true,
	"error":   true,
}

type checkRunPayload struct {
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
}

// FailingChecks implements port.PreMergeChecksReader.
func (a *PRAPI) FailingChecks(ctx context.Context, token, owner, repo, ref string) ([]string, error) {
	var out []string
	runsPath := repoPath(owner, repo) + "/commits/" + url.PathEscape(ref) + "/check-runs"
	err := a.eachPage(ctx, runsPath, func(pagePath string) (int, error) {
		var page struct {
			CheckRuns []checkRunPayload `json:"check_runs"`
		}
		if err := doJSONAt(ctx, a.baseURL, token, http.MethodGet, pagePath, nil, &page); err != nil {
			return 0, err
		}
		for _, r := range page.CheckRuns {
			if failingCheckConclusions[strings.ToLower(r.Conclusion)] {
				out = append(out, r.Name)
			}
		}
		return len(page.CheckRuns), nil
	})
	if err != nil {
		return nil, err
	}

	status, err := getCombinedStatusAt(ctx, a.baseURL, token, owner, repo, ref)
	if err != nil {
		return nil, err
	}
	for _, s := range status.Statuses {
		if failingStatusStates[strings.ToLower(s.State)] {
			out = append(out, s.Context)
		}
	}
	return out, nil
}

// BranchHeadSHA implements port.PreMergeChecksReader.
func (a *PRAPI) BranchHeadSHA(ctx context.Context, token, owner, repo, branch string) (string, error) {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	path := repoPath(owner, repo) + "/git/ref/heads/" + refSegments(branch)
	if err := doJSONAt(ctx, a.baseURL, token, http.MethodGet, path, nil, &ref); err != nil {
		return "", err
	}
	return ref.Object.SHA, nil
}
