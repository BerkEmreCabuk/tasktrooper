package gcloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// The two API hosts this client reads. Cloud Run is addressed through its
// GLOBAL host rather than a regional one (<region>-run.googleapis.com): the
// regional hosts exist for the v1 Knative-shaped API, while v2 routes by
// resource name from the global host, which is what makes a cross-region
// listing one request instead of thirty-odd.
const (
	defaultRunBaseURL       = "https://run.googleapis.com"
	defaultContainerBaseURL = "https://container.googleapis.com"
)

// tokenRefreshMargin re-fetches the access token this long before it actually
// expires, so a request started just under the wire never races an expiring
// token.
const tokenRefreshMargin = 30 * time.Second

// maxListPages bounds every paged listing. A project with more Cloud Run
// services than this has a problem no console can render anyway, and an
// unbounded cursor loop against a third party is how one bad response becomes
// an outage here.
const maxListPages = 50

// maxLocationFanOut is how many regions the Cloud Run fallback queries at
// once. Cloud Run has roughly forty regions; serially that is a visible stall,
// and unbounded it is forty simultaneous connections to one host for a page
// load.
const maxLocationFanOut = 8

// cachedToken is one scope's access token and the moment it stops being usable.
type cachedToken struct {
	token string
	exp   time.Time
}

// Client is a read-only Google Cloud client for Cloud Run and GKE. It carries
// the service account's RSA signing key and exchanges it for short-lived OAuth
// access tokens on demand, cached until close to expiry.
type Client struct {
	sa        parsedServiceAccount
	projectID string

	runBaseURL       string
	containerBaseURL string
	tokenURL         string
	httpClient       *http.Client

	mu     sync.Mutex
	tokens map[string]cachedToken
}

var _ port.GCloudClient = (*Client)(nil)

// New builds a Client from a decrypted Google Cloud credential. cred.Data must
// carry service_account_json — the full JSON key file, PKCS8 PEM private key
// included.
//
// cred.ProjectID wins over the key file's own project_id when both are set: a
// service account issued in one project and granted roles on another is the
// normal shape of a shared deploy identity, and the key file cannot know which
// project the operator meant.
func New(cred domain.GCloudCredential) (*Client, error) {
	raw := cred.Data["service_account_json"]
	if raw == "" {
		return nil, errors.New("gcloud: credential missing service_account_json")
	}
	sa, err := parseServiceAccountJSON(raw)
	if err != nil {
		return nil, err
	}
	projectID := strings.TrimSpace(cred.ProjectID)
	if projectID == "" {
		projectID = sa.ProjectID
	}
	if projectID == "" {
		return nil, errors.New("gcloud: no project id — the key file names none and none was supplied")
	}
	return &Client{
		sa:               sa,
		projectID:        projectID,
		runBaseURL:       defaultRunBaseURL,
		containerBaseURL: defaultContainerBaseURL,
		tokenURL:         sa.TokenURL,
		httpClient:       &http.Client{Timeout: 30 * time.Second},
		tokens:           make(map[string]cachedToken, 1),
	}, nil
}

// Identity is the project every call is scoped to and the service account
// making it — the address an operator grants roles to.
func (c *Client) Identity() domain.GCloudIdentity {
	return domain.GCloudIdentity{ProjectID: c.projectID, ClientEmail: c.sa.ClientEmail}
}

// SetRunBaseURL, SetContainerBaseURL and SetTokenURL point the client at
// different hosts; used by tests.
func (c *Client) SetRunBaseURL(u string)       { c.runBaseURL = u }
func (c *Client) SetContainerBaseURL(u string) { c.containerBaseURL = u }
func (c *Client) SetTokenURL(u string)         { c.tokenURL = u }

// bearerToken returns a cached access token for scope if it still has life
// left, exchanging a fresh assertion for one otherwise. Never logged — callers
// only ever see it inside the Authorization header.
func (c *Client) bearerToken(ctx context.Context, scope string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.tokens[scope]; ok && time.Now().Before(cached.exp.Add(-tokenRefreshMargin)) {
		return cached.token, nil
	}
	tok, exp, err := fetchAccessToken(ctx, c.httpClient, c.tokenURL, c.sa.ClientEmail, scope, c.sa.PrivateKey)
	if err != nil {
		return "", err
	}
	c.tokens[scope] = cachedToken{token: tok, exp: exp}
	return tok, nil
}

// apiError is a non-2xx Google Cloud response, carrying the status and a body
// snippet for diagnostics. Google's error bodies describe the request's target
// and the missing permission; they never echo the credential.
type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("gcloud api: %d %s", e.Status, e.Body)
}

// get issues an authorized GET against baseURL+path and decodes the response
// into out. Only GET: this package reads and nothing more.
func (c *Client) get(ctx context.Context, baseURL, path string, out any) error {
	tok, err := c.bearerToken(ctx, cloudPlatformScope)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &apiError{Status: resp.StatusCode, Body: domain.TruncateHead(string(data), 500)}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// classifyListing turns a listing failure into the sentinel that says what the
// operator must do next.
//
// 403 is the ungranted-role case and is NOT a failure of this system: the
// credential authenticates, Google simply refuses the project. 401 is left
// alone — that one really is broken auth, and reporting it as "cannot
// enumerate" would send someone to the IAM console over an expired key.
func classifyListing(err error) error {
	var apiErr *apiError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden {
		return fmt.Errorf("gcloud: %v: %w", apiErr, port.ErrGCloudListingUnavailable)
	}
	return err
}

// classifyGet is classifyListing for a single-resource read, with the one
// answer a listing cannot produce: a 404 means the binding points at something
// that no longer exists, which the console must show as a broken binding
// rather than as a permissions problem.
func classifyGet(err error) error {
	var apiErr *apiError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return fmt.Errorf("gcloud: %v: %w", apiErr, port.ErrNotFound)
	}
	return classifyListing(err)
}

// ValidateAuth confirms the service account authenticates against Google's
// OAuth token endpoint. Token exchange only — no API call — so it does not
// depend on any IAM role having been granted yet. A credential that signs but
// cannot list is still worth saving: the fix for the second problem is a role
// binding in the customer's console, and refusing the save would leave them
// nothing to grant the role TO.
func (c *Client) ValidateAuth(ctx context.Context) error {
	_, err := c.bearerToken(ctx, cloudPlatformScope)
	return err
}

// ---------------------------------------------------------------- Cloud Run

// runService is the slice of a Cloud Run v2 Service this client reads.
type runService struct {
	Name                  string `json:"name"`
	URI                   string `json:"uri"`
	LatestReadyRevision   string `json:"latestReadyRevision"`
	LatestCreatedRevision string `json:"latestCreatedRevision"`
	UpdateTime            string `json:"updateTime"`
	Template              struct {
		Containers []struct {
			Image string `json:"image"`
		} `json:"containers"`
	} `json:"template"`
	TrafficStatuses []struct {
		Type     string `json:"type"`
		Revision string `json:"revision"`
		Percent  int    `json:"percent"`
		Tag      string `json:"tag"`
		URI      string `json:"uri"`
	} `json:"trafficStatuses"`
	TerminalCondition struct {
		Type    string `json:"type"`
		State   string `json:"state"`
		Reason  string `json:"reason"`
		Message string `json:"message"`
	} `json:"terminalCondition"`
}

// ListCloudRunServices enumerates every Cloud Run service in the project.
//
// Region traversal, and why it is written twice: Cloud Run v2's ListServices
// takes a parent of projects/<p>/locations/<loc>, and whether "-" is accepted
// there as an all-locations wildcard is not something this code can assume —
// it is accepted on some Google APIs and rejected as a malformed parent on
// others. So the aggregated call is TRIED first (one request, the whole
// project) and a rejection of the wildcard specifically — 400 or 404, meaning
// "that is not a location" — falls back to enumerating the project's Cloud Run
// locations and querying each one, at most maxLocationFanOut at a time.
//
// A 403 is never a wildcard rejection and short-circuits both paths: the
// credential cannot read Cloud Run in this project at all, and fanning out
// would turn one refusal into forty.
//
// Regions that fail individually in the fallback are reported in
// UnreachableLocations rather than dropped, because a silently missing region
// reads as "you have no services there" — which is exactly the answer that
// makes someone bind the wrong resource.
func (c *Client) ListCloudRunServices(ctx context.Context) (domain.GCloudResourceList, error) {
	refs, err := c.listCloudRunIn(ctx, "-")
	if err == nil {
		sortRefs(refs)
		return domain.GCloudResourceList{Resources: refs}, nil
	}
	var apiErr *apiError
	if !errors.As(err, &apiErr) || !wildcardRejected(apiErr.Status) {
		return domain.GCloudResourceList{}, classifyListing(err)
	}

	locations, err := c.cloudRunLocations(ctx)
	if err != nil {
		return domain.GCloudResourceList{}, classifyListing(err)
	}
	if len(locations) == 0 {
		return domain.GCloudResourceList{Resources: []domain.GCloudResourceRef{}}, nil
	}

	var (
		mu          sync.Mutex
		all         []domain.GCloudResourceRef
		unreachable []string
		forbidden   int
		wg          sync.WaitGroup
	)
	sem := make(chan struct{}, maxLocationFanOut)
	for _, loc := range locations {
		wg.Add(1)
		go func(loc string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			found, err := c.listCloudRunIn(ctx, loc)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				unreachable = append(unreachable, loc)
				var apiErr *apiError
				if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden {
					forbidden++
				}
				return
			}
			all = append(all, found...)
		}(loc)
	}
	wg.Wait()

	// Every region refusing is the same answer the aggregated 403 would have
	// given: the credential cannot enumerate Cloud Run here. Reporting it as a
	// partial success with forty unreachable regions would bury the one fact
	// the operator needs.
	if forbidden == len(locations) {
		return domain.GCloudResourceList{}, fmt.Errorf("gcloud: cloud run listing refused in every location: %w", port.ErrGCloudListingUnavailable)
	}

	sortRefs(all)
	sort.Strings(unreachable)
	return domain.GCloudResourceList{Resources: all, UnreachableLocations: unreachable}, nil
}

// wildcardRejected reports whether status is Google saying "-" is not a
// location, as opposed to saying anything about permissions or the project.
func wildcardRejected(status int) bool {
	return status == http.StatusBadRequest || status == http.StatusNotFound
}

// listCloudRunIn lists one location's services; location "-" asks for the
// aggregated form.
func (c *Client) listCloudRunIn(ctx context.Context, location string) ([]domain.GCloudResourceRef, error) {
	var out []domain.GCloudResourceRef
	pageToken := ""
	for page := 0; ; page++ {
		if page >= maxListPages {
			return nil, fmt.Errorf("gcloud: cloud run services in %q: still paging after %d pages, refusing to follow the cursor further", location, maxListPages)
		}
		path := "/v2/projects/" + url.PathEscape(c.projectID) + "/locations/" + url.PathEscape(location) + "/services?pageSize=100"
		if pageToken != "" {
			path += "&pageToken=" + url.QueryEscape(pageToken)
		}
		var resp struct {
			Services      []runService `json:"services"`
			NextPageToken string       `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.runBaseURL, path, &resp); err != nil {
			return nil, err
		}
		for _, svc := range resp.Services {
			out = append(out, runServiceRef(svc))
		}
		if resp.NextPageToken == "" {
			return out, nil
		}
		pageToken = resp.NextPageToken
	}
}

// cloudRunLocations enumerates the regions Cloud Run is offered in for this
// project — the fallback's input, and the reason the fallback is not a
// hardcoded region list that would go stale the next time Google opens one.
func (c *Client) cloudRunLocations(ctx context.Context) ([]string, error) {
	var out []string
	pageToken := ""
	for page := 0; ; page++ {
		if page >= maxListPages {
			return nil, fmt.Errorf("gcloud: cloud run locations: still paging after %d pages, refusing to follow the cursor further", maxListPages)
		}
		path := "/v2/projects/" + url.PathEscape(c.projectID) + "/locations?pageSize=100"
		if pageToken != "" {
			path += "&pageToken=" + url.QueryEscape(pageToken)
		}
		var resp struct {
			Locations []struct {
				LocationID string `json:"locationId"`
			} `json:"locations"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.runBaseURL, path, &resp); err != nil {
			return nil, err
		}
		for _, loc := range resp.Locations {
			if loc.LocationID != "" {
				out = append(out, loc.LocationID)
			}
		}
		if resp.NextPageToken == "" {
			return out, nil
		}
		pageToken = resp.NextPageToken
	}
}

// runServiceRef projects a v2 Service onto the listing shape. Name is already
// fully qualified in v2, so it is the binding key verbatim.
func runServiceRef(svc runService) domain.GCloudResourceRef {
	parsed, err := domain.ParseGCloudResourceName(svc.Name)
	ref := domain.GCloudResourceRef{
		Type:  domain.GCloudResourceCloudRun,
		Name:  svc.Name,
		URI:   svc.URI,
		State: svc.TerminalCondition.State,
	}
	if err == nil {
		ref.ProjectID = parsed.ProjectID
		ref.Location = parsed.Location
		ref.DisplayName = parsed.ShortName
	} else {
		// A name Google returned that this code cannot parse is still worth
		// listing — it just cannot be addressed by parts. Dropping it would
		// hide a real service.
		ref.DisplayName = shortName(svc.Name)
	}
	return ref
}

// CloudRunService reads one service by its fully qualified name.
func (c *Client) CloudRunService(ctx context.Context, name string) (domain.CloudRunServiceDetail, error) {
	parsed, err := domain.ParseGCloudResourceName(name)
	if err != nil {
		return domain.CloudRunServiceDetail{}, err
	}
	if parsed.Type != domain.GCloudResourceCloudRun {
		return domain.CloudRunServiceDetail{}, fmt.Errorf("%w: %q is not a cloud run service", domain.ErrInvalidGCloudResource, name)
	}

	var svc runService
	if err := c.get(ctx, c.runBaseURL, "/v2/"+name, &svc); err != nil {
		return domain.CloudRunServiceDetail{}, classifyGet(err)
	}
	// v2 omits `name` from some responses' bodies only in error shapes, but a
	// service read that came back without one would produce a ref with no
	// identity — so the requested name stands in.
	if svc.Name == "" {
		svc.Name = name
	}

	detail := domain.CloudRunServiceDetail{
		Ref:                   runServiceRef(svc),
		LatestReadyRevision:   shortName(svc.LatestReadyRevision),
		LatestCreatedRevision: shortName(svc.LatestCreatedRevision),
		Ready:                 svc.TerminalCondition.State,
		ReadyReason:           svc.TerminalCondition.Reason,
		ReadyMessage:          svc.TerminalCondition.Message,
	}
	if len(svc.Template.Containers) > 0 {
		detail.Image = svc.Template.Containers[0].Image
	}
	if t, err := time.Parse(time.RFC3339, svc.UpdateTime); err == nil {
		detail.UpdateTime = t
	}
	for _, t := range svc.TrafficStatuses {
		revision := shortName(t.Revision)
		// A LATEST target names no revision — it means "whatever is newest",
		// which at read time IS latestReadyRevision. Leaving it blank would
		// render a traffic row pointing at nothing.
		if revision == "" && strings.HasSuffix(t.Type, "_LATEST") {
			revision = detail.LatestReadyRevision
		}
		detail.Traffic = append(detail.Traffic, domain.CloudRunTrafficTarget{
			Revision: revision,
			Percent:  t.Percent,
			Tag:      t.Tag,
			URI:      t.URI,
		})
	}
	return detail, nil
}

// --------------------------------------------------------------------- GKE

// gkeCluster is the slice of a Container v1 Cluster this client reads.
type gkeCluster struct {
	Name                 string `json:"name"`
	Location             string `json:"location"`
	Status               string `json:"status"`
	StatusMessage        string `json:"statusMessage"`
	CurrentMasterVersion string `json:"currentMasterVersion"`
	CurrentNodeCount     int    `json:"currentNodeCount"`
	Endpoint             string `json:"endpoint"`
	NodePools            []struct {
		Name             string `json:"name"`
		Status           string `json:"status"`
		InitialNodeCount int    `json:"initialNodeCount"`
		Version          string `json:"version"`
		Config           struct {
			MachineType string `json:"machineType"`
		} `json:"config"`
	} `json:"nodePools"`
	Autopilot struct {
		Enabled bool `json:"enabled"`
	} `json:"autopilot"`
	PrivateClusterConfig struct {
		EnablePrivateEndpoint bool `json:"enablePrivateEndpoint"`
	} `json:"privateClusterConfig"`
}

// ListGKEClusters enumerates every GKE cluster in the project.
//
// No fan-out here: the Container v1 API documents "-" as an all-locations
// parent and answers it with a missingZones list for the ones it could not
// reach, which is the same information the Cloud Run fallback has to assemble
// by hand.
//
// This lists CLUSTERS, not workloads. See gkeWorkloadsReason.
func (c *Client) ListGKEClusters(ctx context.Context) (domain.GCloudResourceList, error) {
	var resp struct {
		Clusters     []gkeCluster `json:"clusters"`
		MissingZones []string     `json:"missingZones"`
	}
	path := "/v1/projects/" + url.PathEscape(c.projectID) + "/locations/-/clusters"
	if err := c.get(ctx, c.containerBaseURL, path, &resp); err != nil {
		return domain.GCloudResourceList{}, classifyListing(err)
	}

	out := make([]domain.GCloudResourceRef, 0, len(resp.Clusters))
	for _, cluster := range resp.Clusters {
		out = append(out, c.gkeClusterRef(cluster))
	}
	sortRefs(out)
	sort.Strings(resp.MissingZones)
	return domain.GCloudResourceList{Resources: out, UnreachableLocations: resp.MissingZones}, nil
}

// gkeClusterRef projects a Cluster onto the listing shape. Unlike Cloud Run's,
// the Container API's `name` is the SHORT name, so the fully qualified one —
// the binding key — has to be built here.
func (c *Client) gkeClusterRef(cluster gkeCluster) domain.GCloudResourceRef {
	return domain.GCloudResourceRef{
		Type:        domain.GCloudResourceGKECluster,
		Name:        domain.GCloudResourceName(c.projectID, cluster.Location, "clusters", cluster.Name),
		DisplayName: cluster.Name,
		ProjectID:   c.projectID,
		Location:    cluster.Location,
		URI:         cluster.Endpoint,
		State:       cluster.Status,
	}
}

// GKECluster reads one cluster by its fully qualified name.
func (c *Client) GKECluster(ctx context.Context, name string) (domain.GKEClusterDetail, error) {
	parsed, err := domain.ParseGCloudResourceName(name)
	if err != nil {
		return domain.GKEClusterDetail{}, err
	}
	if parsed.Type != domain.GCloudResourceGKECluster {
		return domain.GKEClusterDetail{}, fmt.Errorf("%w: %q is not a gke cluster", domain.ErrInvalidGCloudResource, name)
	}

	var cluster gkeCluster
	if err := c.get(ctx, c.containerBaseURL, "/v1/"+name, &cluster); err != nil {
		return domain.GKEClusterDetail{}, classifyGet(err)
	}
	if cluster.Name == "" {
		cluster.Name = parsed.ShortName
	}
	if cluster.Location == "" {
		cluster.Location = parsed.Location
	}

	detail := domain.GKEClusterDetail{
		Ref:             c.gkeClusterRef(cluster),
		Status:          cluster.Status,
		StatusMessage:   cluster.StatusMessage,
		MasterVersion:   cluster.CurrentMasterVersion,
		NodeCount:       cluster.CurrentNodeCount,
		Autopilot:       cluster.Autopilot.Enabled,
		PrivateEndpoint: cluster.PrivateClusterConfig.EnablePrivateEndpoint,
	}
	for _, np := range cluster.NodePools {
		detail.NodePools = append(detail.NodePools, domain.GKENodePool{
			Name:        np.Name,
			Status:      np.Status,
			NodeCount:   np.InitialNodeCount,
			Version:     np.Version,
			MachineType: np.Config.MachineType,
		})
	}
	detail.WorkloadsAvailable = false
	detail.WorkloadsNote = gkeWorkloadsNote(detail)
	return detail, nil
}

// gkeWorkloadsNote states, per cluster, why the Deployments inside it are
// not listed.
//
// This integration stops at the Container API on purpose. Reading a cluster's
// workloads means calling that cluster's own Kubernetes API server — a second
// authorization layer with three walls in front of it, none of which this
// server can clear on its own:
//
//   - reachability: a private-endpoint cluster has no address routable from a
//     shared agent-server outside the customer's VPC, so no credential of any
//     kind would help;
//   - trust: the connection must be pinned to the cluster's own CA from
//     masterAuth.clusterCaCertificate, a per-cluster TLS config;
//   - authorization: GKE maps IAM to in-cluster RBAC, so the service account
//     additionally needs a role binding INSIDE the cluster before
//     apis/apps/v1/deployments answers anything but 403.
//
// Shipping that path untested against a real cluster is what "leave a route
// that does not work looking like it works" means, so it is not shipped. The
// field says so rather than an empty workload list saying "this cluster runs
// nothing".
func gkeWorkloadsNote(detail domain.GKEClusterDetail) string {
	if detail.PrivateEndpoint {
		return "the cluster's control plane has no public endpoint, so its Kubernetes API is not reachable from this server"
	}
	return "workload listing requires calling the cluster's own Kubernetes API, which this integration does not do"
}

// ------------------------------------------------------------------ shared

// shortName returns the last segment of a slash-separated GCP resource name.
func shortName(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// sortRefs orders a listing by fully qualified name so the picker is stable
// across calls — the fan-out above completes regions in whatever order they
// answer, which would otherwise reshuffle the list on every page load.
func sortRefs(refs []domain.GCloudResourceRef) {
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
}
