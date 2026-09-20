package vercel

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Is makes errors.Is(err, port.ErrVercelUnauthorized) answer true for a 401 or
// a 403, so the application layer can tell a refused token from an outage
// without importing this package for IsUnauthorized. Same test as
// IsUnauthorized, reachable through the standard errors machinery.
func (e *APIError) Is(target error) bool {
	if target != port.ErrVercelUnauthorized {
		return false
	}
	return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
}

// deploymentPageLimit bounds one Deployments call. Ten is enough for the
// details view's two questions — what shipped last, and what failed last —
// without paging a project with thousands of builds.
const deploymentPageLimit = 10

// rawDeployment is the subset of Vercel's deployment object this package
// reads. Field names and their units are from the /v7/deployments reference:
// `created`, `createdAt`, `ready` and `buildingAt` are JavaScript
// milliseconds, and `meta` is documented only as "Metadata information from
// the Git provider" — the reference enumerates none of its keys, so they are
// read defensively below rather than assumed.
type rawDeployment struct {
	UID          string         `json:"uid"`
	Name         string         `json:"name"`
	URL          string         `json:"url"`
	State        string         `json:"state"`
	ReadyState   string         `json:"readyState"`
	Target       *string        `json:"target"`
	Created      int64          `json:"created"`
	CreatedAt    int64          `json:"createdAt"`
	Ready        int64          `json:"ready"`
	BuildingAt   int64          `json:"buildingAt"`
	InspectorURL *string        `json:"inspectorUrl"`
	ErrorCode    string         `json:"errorCode"`
	ErrorMessage *string        `json:"errorMessage"`
	Meta         map[string]any `json:"meta"`
}

// metaString reads one key out of the git metadata, accepting only a string.
// The reference types `meta` as a bare object, so a non-string value is
// possible and must not become the literal text of a Go %v rendering.
func metaString(meta map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := meta[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func msTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

func (r rawDeployment) toDomain() domain.VercelDeployment {
	d := domain.VercelDeployment{
		ID:        r.UID,
		ErrorCode: r.ErrorCode,
	}
	// readyState is required by the schema and state is not, so it is the one
	// that always carries an answer; state wins when both are present because
	// it is the field the dashboard shows.
	d.State = r.State
	if d.State == "" {
		d.State = r.ReadyState
	}
	if r.Target != nil {
		d.Target = *r.Target
	}
	if r.URL != "" {
		d.URL = httpsOf(r.URL)
	}
	if r.InspectorURL != nil {
		d.InspectorURL = *r.InspectorURL
	}
	if r.ErrorMessage != nil {
		d.ErrorMessage = *r.ErrorMessage
	}
	d.CreatedAt = msTime(r.CreatedAt)
	if d.CreatedAt.IsZero() {
		d.CreatedAt = msTime(r.Created)
	}
	d.ReadyAt = msTime(r.Ready)
	// One key per git provider, in the order the `link.type` values appear.
	d.CommitSHA = metaString(r.Meta, "githubCommitSha", "gitlabCommitSha", "bitbucketCommitSha")
	d.CommitRef = metaString(r.Meta, "githubCommitRef", "gitlabCommitRef", "bitbucketCommitRef")
	d.CommitMessage = metaString(r.Meta, "githubCommitMessage", "gitlabCommitMessage", "bitbucketCommitMessage")
	d.CommitAuthor = metaString(r.Meta,
		"githubCommitAuthorName", "gitlabCommitAuthorName", "bitbucketCommitAuthorName")
	return d
}

// Deployments — GET /v7/deployments?projectId=&target=&limit=, newest first.
//
// One page and no pagination: the caller wants the head of the list, and the
// error fields it reads (errorCode / errorMessage) are already on the list
// object, so the per-deployment GET adds nothing this view needs.
func (c *Client) Deployments(ctx context.Context, token, teamID, projectID, target string, limit int) ([]domain.VercelDeployment, error) {
	if limit <= 0 || limit > deploymentPageLimit {
		limit = deploymentPageLimit
	}
	q := withTeam(url.Values{}, teamID)
	q.Set("projectId", projectID)
	q.Set("limit", strconv.Itoa(limit))
	if target != "" {
		q.Set("target", target)
	}
	var out struct {
		Deployments []rawDeployment `json:"deployments"`
	}
	if err := c.getJSON(ctx, token, "/v7/deployments", q, &out); err != nil {
		return nil, err
	}
	deployments := make([]domain.VercelDeployment, 0, len(out.Deployments))
	for _, r := range out.Deployments {
		deployments = append(deployments, r.toDomain())
	}
	return deployments, nil
}

var _ port.VercelDeploymentsAPI = (*Client)(nil)
