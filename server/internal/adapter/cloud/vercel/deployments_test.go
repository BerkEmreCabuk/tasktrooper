package vercel

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// deploymentsBody is a trimmed but field-for-field faithful /v7/deployments
// answer: the shapes that actually bite are all here — timestamps as
// JavaScript milliseconds, target/inspectorUrl/errorMessage nullable, and meta
// as a free-form object whose keys the API reference does not enumerate.
const deploymentsBody = `{
  "pagination": {"count": 2, "next": null, "prev": null},
  "deployments": [
    {
      "uid": "dpl_new",
      "name": "acme-web",
      "url": "acme-web-abc.vercel.app",
      "created": 1756713600000,
      "createdAt": 1756713600000,
      "ready": 1756713720000,
      "buildingAt": 1756713605000,
      "state": "READY",
      "readyState": "READY",
      "target": "production",
      "inspectorUrl": "https://vercel.com/acme/acme-web/abc",
      "meta": {
        "githubCommitSha": "0f1e2d3",
        "githubCommitRef": "main",
        "githubCommitMessage": "ship it",
        "githubCommitAuthorName": "akif"
      }
    },
    {
      "uid": "dpl_old",
      "name": "acme-web",
      "url": "acme-web-xyz.vercel.app",
      "created": 1756627200000,
      "createdAt": 1756627200000,
      "state": "ERROR",
      "readyState": "ERROR",
      "target": null,
      "inspectorUrl": null,
      "errorCode": "BUILD_FAILED",
      "errorMessage": "Command \"npm run build\" exited with 1",
      "meta": {"githubCommitSha": "9a8b7c6", "gitlabCommitSha": 12345}
    }
  ]
}`

// TestDeploymentsReadsTheVerifiedFields pins every field name this package
// depends on against the documented /v7/deployments shape. A rename upstream
// has to fail here rather than silently emptying the details panel.
func TestDeploymentsReadsTheVerifiedFields(t *testing.T) {
	var gotPath, gotQuery string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(deploymentsBody))
	}))

	deployments, err := c.Deployments(context.Background(), "tok", "team_1", "prj_1", domain.VercelTargetProduction, 10)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v7/deployments" {
		t.Fatalf("path = %s, want /v7/deployments", gotPath)
	}
	for _, want := range []string{"projectId=prj_1", "teamId=team_1", "target=production", "limit=10"} {
		if !strings.Contains(gotQuery, want) {
			t.Fatalf("query %q is missing %q", gotQuery, want)
		}
	}
	if len(deployments) != 2 {
		t.Fatalf("got %d deployments, want 2", len(deployments))
	}

	latest := deployments[0]
	if latest.ID != "dpl_new" || latest.State != domain.VercelDeploymentReady {
		t.Fatalf("latest = %+v", latest)
	}
	// Milliseconds, not seconds: reading these as Unix seconds would date the
	// deployment to 57644 AD and the panel would render it without complaint.
	if want := time.UnixMilli(1756713600000).UTC(); !latest.CreatedAt.Equal(want) {
		t.Fatalf("CreatedAt = %s, want %s", latest.CreatedAt, want)
	}
	if want := time.UnixMilli(1756713720000).UTC(); !latest.ReadyAt.Equal(want) {
		t.Fatalf("ReadyAt = %s, want %s", latest.ReadyAt, want)
	}
	if latest.CommitSHA != "0f1e2d3" || latest.CommitRef != "main" ||
		latest.CommitMessage != "ship it" || latest.CommitAuthor != "akif" {
		t.Fatalf("commit metadata lost: %+v", latest)
	}
	if latest.URL != "https://acme-web-abc.vercel.app" {
		t.Fatalf("URL = %q, want an https address", latest.URL)
	}
	if latest.Target != domain.VercelTargetProduction {
		t.Fatalf("Target = %q", latest.Target)
	}

	failed := deployments[1]
	if !failed.Failed() {
		t.Fatalf("dpl_old must report as failed: %+v", failed)
	}
	if failed.ErrorCode != "BUILD_FAILED" || failed.ErrorMessage == "" {
		t.Fatalf("the build error must survive: %+v", failed)
	}
	// A null target and a null inspectorUrl are legal and mean "preview" and
	// "none" — they must decode to empty strings, not fail the whole page.
	if failed.Target != "" || failed.InspectorURL != "" {
		t.Fatalf("nullable fields must decode to empty: %+v", failed)
	}
	// meta is typed as a bare object upstream, so a non-string value is
	// possible; taking it would put a Go rendering of a number in the commit
	// field. The github key wins and the numeric gitlab one is ignored.
	if failed.CommitSHA != "9a8b7c6" {
		t.Fatalf("CommitSHA = %q, want the string-valued key", failed.CommitSHA)
	}
}

// TestDeploymentsOmitsTheTargetFilterWhenBlank: the details view drops the
// production filter to find a preview-only project's last build.
func TestDeploymentsOmitsTheTargetFilterWhenBlank(t *testing.T) {
	var gotQuery string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"deployments":[]}`))
	}))

	if _, err := c.Deployments(context.Background(), "tok", "", "prj_1", "", 0); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gotQuery, "target=") {
		t.Fatalf("query %q must carry no target filter", gotQuery)
	}
	// teamId is likewise absent for the personal account, which is a real
	// scope: sending teamId= would address a team named "".
	if strings.Contains(gotQuery, "teamId=") {
		t.Fatalf("query %q must carry no teamId for the personal account", gotQuery)
	}
	if !strings.Contains(gotQuery, "limit=10") {
		t.Fatalf("query %q must fall back to the page limit", gotQuery)
	}
}

// TestAPIErrorMatchesTheUnauthorizedSentinel is the seam the application layer
// stands on: it must be able to tell a refused token from an outage WITHOUT
// importing this package, which is what makes vercelops.ErrListingUnavailable
// a 200-with-a-reason instead of a 500.
func TestAPIErrorMatchesTheUnauthorizedSentinel(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"Not authorized"}}`))
		}))
		_, err := c.Deployments(context.Background(), "bad", "", "prj_1", "", 10)
		if !errors.Is(err, port.ErrVercelUnauthorized) {
			t.Fatalf("status %d: errors.Is(err, port.ErrVercelUnauthorized) = false for %v", status, err)
		}
	}

	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"code":"bad_gateway","message":"upstream"}}`))
	}))
	_, err := c.Deployments(context.Background(), "tok", "", "prj_1", "", 10)
	if errors.Is(err, port.ErrVercelUnauthorized) {
		t.Fatalf("a 502 is an outage, not a refused token: %v", err)
	}
	if err == nil {
		t.Fatal("a 502 must still be an error")
	}
}
