package appstore

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// defaultBaseURL is App Store Connect's production API host.
const defaultBaseURL = "https://api.appstoreconnect.apple.com"

// tokenRefreshMargin re-mints the JWT this long before it actually expires,
// so a request started just under the wire never races an expiring token.
const tokenRefreshMargin = 30 * time.Second

// Client is a thin App Store Connect API client. It carries the team's ES256
// signing credential and mints short-lived JWTs on demand (cached until
// close to expiry — minting is cheap but there is no reason to redo it on
// every call).
type Client struct {
	keyID    string
	issuerID string
	privKey  *ecdsa.PrivateKey

	baseURL    string
	httpClient *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

var _ port.AppStoreClient = (*Client)(nil)

// New builds a Client from a decrypted ASC store credential. cred.Data must
// carry key_id, issuer_id, and p8 (a PEM-encoded PKCS8 EC private key).
func New(cred domain.StoreCredential) (*Client, error) {
	keyID := cred.Data["key_id"]
	issuerID := cred.Data["issuer_id"]
	p8 := cred.Data["p8"]
	if keyID == "" || issuerID == "" || p8 == "" {
		return nil, fmt.Errorf("appstore: credential missing key_id, issuer_id, or p8")
	}
	priv, err := parseP8PrivateKey([]byte(p8))
	if err != nil {
		return nil, err
	}
	return &Client{
		keyID:      keyID,
		issuerID:   issuerID,
		privKey:    priv,
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// SetBaseURL points the client at a different host; used by tests.
func (c *Client) SetBaseURL(u string) {
	c.baseURL = u
}

// bearerToken returns a cached JWT if it still has life left, minting a
// fresh one otherwise. Never logged — callers only ever see it inside the
// Authorization header.
func (c *Client) bearerToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExp.Add(-tokenRefreshMargin)) {
		return c.token, nil
	}
	tok, exp, err := mintToken(c.keyID, c.issuerID, c.privKey)
	if err != nil {
		return "", err
	}
	c.token = tok
	c.tokenExp = exp
	return c.token, nil
}

// apiError is a non-2xx ASC response, carrying the status and a body
// snippet for diagnostics.
type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("appstore api: %d %s", e.Status, e.Body)
}

// do issues a JSON:API request against ASC, decoding the response body into
// out (if non-nil) on success and returning an *apiError carrying the
// status and a body snippet on any non-2xx response.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	tok, err := c.bearerToken()
	if err != nil {
		return err
	}

	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("appstore: building %s %s request: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// A bare *url.Error says nothing about which ASC call died, and a
		// monitor sweep touches half a dozen endpoints per app — name the
		// method and path (never the token, which lives only in the header).
		return fmt.Errorf("appstore: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := domain.TruncateHead(string(data), 500)
		return &apiError{Status: resp.StatusCode, Body: snippet}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// ascTimeLayout matches the format ASC actually emits for date attributes,
// e.g. "2021-05-28T18:11:03.000+0000" — millisecond precision, timezone
// offset without a colon. time.RFC3339 (colon offset) is kept as a fallback
// in case a future ASC response uses it instead.
const ascTimeLayout = "2006-01-02T15:04:05.000-0700"

func parseASCTime(s string) (time.Time, error) {
	if t, err := time.Parse(ascTimeLayout, s); err == nil {
		return t, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("appstore: parsing timestamp %q: %w", s, err)
	}
	return t, nil
}

// jsonAPIRefList is the shape shared by every ASC "list of resource
// references" response we only need the id from (apps, bundleIds, builds).
type jsonAPIRefList struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// jsonAPIPage is one page of an ASC collection: the rows, plus the cursor
// link to the next page (absent on the last one).
type jsonAPIPage[T any] struct {
	Data  []T `json:"data"`
	Links struct {
		Next string `json:"next"`
	} `json:"links"`
}

// ascMaxCollectionPages bounds a single collection walk. ASC pages at 50 rows
// by default, so this is 2000 rows — an order of magnitude past anything one
// app produces, which means reaching it does not mean "a big app", it means a
// cursor that stopped advancing. The walk fails there rather than returning
// what it has: a silently truncated list is precisely the failure this paging
// exists to remove, and hiding it again behind a partial result would put the
// release pipeline back where it started.
const ascMaxCollectionPages = 40

// listAll walks an ASC collection starting at path, following links.next
// until the cursor runs out, and returns every row it saw.
//
// ASC pages every collection at a fixed default size and documents no ordering
// for these endpoints, so "page 1" is an arbitrary subset. Every caller here
// searches the collection for one particular row — the newest version, the one
// pending developer release, the editable one to reuse — and a search over
// page 1 alone starts reporting "not there" the moment the collection outgrows
// a page, which it does on its own: this system mints an appStoreVersion per
// merged PR.
//
// A free function rather than a method because Go methods cannot take type
// parameters.
func listAll[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	var all []T
	for page := 0; path != ""; page++ {
		if page >= ascMaxCollectionPages {
			return nil, fmt.Errorf("appstore: %s: still paging after %d pages, refusing to follow the cursor further", path, ascMaxCollectionPages)
		}
		var body jsonAPIPage[T]
		if err := c.do(ctx, http.MethodGet, path, nil, &body); err != nil {
			return nil, err
		}
		all = append(all, body.Data...)
		next, err := nextPagePath(body.Links.Next)
		if err != nil {
			return nil, err
		}
		path = next
	}
	return all, nil
}

// nextPagePath reduces an ASC links.next value to the path+query this client
// will request next.
//
// ASC returns next as an absolute URL, and following it verbatim would send
// the Authorization header wherever that URL points — a host swapped in
// upstream would be handed a live ASC token. It would also bypass SetBaseURL,
// which is the only way tests (and any non-production host) redirect this
// client. Only the path and query are kept; the host always stays the
// configured one.
func nextPagePath(next string) (string, error) {
	if next == "" {
		return "", nil
	}
	u, err := url.Parse(next)
	if err != nil {
		return "", fmt.Errorf("appstore: parsing next page link %q: %w", next, err)
	}
	return u.RequestURI(), nil
}

// ValidateAuth confirms the signing credential actually authenticates
// against ASC, without depending on any app-specific state existing yet.
func (c *Client) ValidateAuth(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/v1/apps?limit=1", nil, nil)
}

// AppByBundleID looks up the ASC app resource id for a bundle identifier.
//
// Not paged, unlike the appStoreVersions walks below: filter[bundleId] is an
// exact match on an identifier that is unique across the App Store, so the
// result is one row or none and page 1 is the whole answer. Paging it would
// only add a round trip to a lookup that already takes resp.Data[0].
func (c *Client) AppByBundleID(ctx context.Context, bundleID string) (appID string, found bool, err error) {
	var resp jsonAPIRefList
	path := "/v1/apps?filter[bundleId]=" + url.QueryEscape(bundleID)
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return "", false, err
	}
	if len(resp.Data) == 0 {
		return "", false, nil
	}
	return resp.Data[0].ID, true, nil
}

// findBundleID resolves the bundleIds resource id for an identifier
// string, used both to check registration (EnsureBundleID) and to build the
// profiles relationship (CreateProfile) — the ASC profiles API requires
// this opaque resource id, not the identifier string itself.
//
// Unpaged for the same reason as AppByBundleID: filter[identifier] is an exact
// match on a value that is unique within the team, so there is no page 2 to
// find anything on.
func (c *Client) findBundleID(ctx context.Context, bundleID string) (resourceID string, found bool, err error) {
	var resp jsonAPIRefList
	path := "/v1/bundleIds?filter[identifier]=" + url.QueryEscape(bundleID)
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return "", false, err
	}
	if len(resp.Data) == 0 {
		return "", false, nil
	}
	return resp.Data[0].ID, true, nil
}

// EnsureBundleID registers bundleID with ASC if it isn't already, under the
// given display name. Idempotent: a bundle id that's already registered is
// left untouched.
func (c *Client) EnsureBundleID(ctx context.Context, bundleID, name string) error {
	_, found, err := c.findBundleID(ctx, bundleID)
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	body := map[string]any{
		"data": map[string]any{
			"type": "bundleIds",
			"attributes": map[string]any{
				"identifier": bundleID,
				"name":       name,
				"platform":   "IOS",
			},
		},
	}
	return c.do(ctx, http.MethodPost, "/v1/bundleIds", body, nil)
}

// CreateCertificate submits csrPEM (a PEM-encoded certificate signing
// request) to ASC and returns the resulting iOS distribution certificate.
func (c *Client) CreateCertificate(ctx context.Context, csrPEM []byte) (port.StoreCert, error) {
	body := map[string]any{
		"data": map[string]any{
			"type": "certificates",
			"attributes": map[string]any{
				"certificateType": "IOS_DISTRIBUTION",
				"csrContent":      base64.StdEncoding.EncodeToString(csrPEM),
			},
		},
	}
	var resp struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				SerialNumber       string `json:"serialNumber"`
				CertificateContent string `json:"certificateContent"`
				ExpirationDate     string `json:"expirationDate"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/certificates", body, &resp); err != nil {
		return port.StoreCert{}, err
	}

	der, err := base64.StdEncoding.DecodeString(resp.Data.Attributes.CertificateContent)
	if err != nil {
		return port.StoreCert{}, fmt.Errorf("appstore: decoding certificateContent: %w", err)
	}
	expiresAt, err := parseASCTime(resp.Data.Attributes.ExpirationDate)
	if err != nil {
		return port.StoreCert{}, err
	}
	return port.StoreCert{
		ID:        resp.Data.ID,
		Serial:    resp.Data.Attributes.SerialNumber,
		DER:       der,
		ExpiresAt: expiresAt,
	}, nil
}

// CreateProfile creates an App Store distribution provisioning profile for
// bundleID, signed by certID.
func (c *Client) CreateProfile(ctx context.Context, bundleID, certID, name string) (port.StoreProfile, error) {
	bundleResourceID, found, err := c.findBundleID(ctx, bundleID)
	if err != nil {
		return port.StoreProfile{}, err
	}
	if !found {
		return port.StoreProfile{}, fmt.Errorf("appstore: bundle id %q is not registered with ASC", bundleID)
	}

	body := map[string]any{
		"data": map[string]any{
			"type": "profiles",
			"attributes": map[string]any{
				"name":        name,
				"profileType": "IOS_APP_STORE",
			},
			"relationships": map[string]any{
				"bundleId": map[string]any{
					"data": map[string]any{"type": "bundleIds", "id": bundleResourceID},
				},
				"certificates": map[string]any{
					"data": []map[string]any{
						{"type": "certificates", "id": certID},
					},
				},
			},
		},
	}
	var resp struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				Name           string `json:"name"`
				ProfileContent string `json:"profileContent"`
				ExpirationDate string `json:"expirationDate"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/profiles", body, &resp); err != nil {
		return port.StoreProfile{}, err
	}

	content, err := base64.StdEncoding.DecodeString(resp.Data.Attributes.ProfileContent)
	if err != nil {
		return port.StoreProfile{}, fmt.Errorf("appstore: decoding profileContent: %w", err)
	}
	expiresAt, err := parseASCTime(resp.Data.Attributes.ExpirationDate)
	if err != nil {
		return port.StoreProfile{}, err
	}
	return port.StoreProfile{
		ID:        resp.Data.ID,
		Name:      resp.Data.Attributes.Name,
		Content:   content,
		ExpiresAt: expiresAt,
	}, nil
}

// LatestVersion returns the newest App Store version's number and review
// state. Returns the zero value if the app has no versions yet.
//
// /v1/apps/{id}/appStoreVersions accepts no `sort` parameter and documents no
// ordering, so the collection is walked whole (every page, see listAll) and the
// highest versionString picked here — the same client-side sort fastlane's
// spaceship applies. Asking for `?limit=1` and trusting row 0 was a real
// hazard, and so was reading only page 1: either way an oldest-first response
// hides READY_FOR_SALE forever, and that single signal is the only thing that
// flips a mobile app to `live`, which is the only thing that opens the
// production deploy gate.
func (c *Client) LatestVersion(ctx context.Context, appID string) (port.AppStoreVersionInfo, error) {
	versions, err := c.listAppStoreVersions(ctx, appID)
	if err != nil {
		return port.AppStoreVersionInfo{}, err
	}
	newest, found := newestVersion(versions)
	if !found {
		return port.AppStoreVersionInfo{}, nil
	}
	return port.AppStoreVersionInfo{
		Version: newest.Attributes.VersionString,
		State:   newest.Attributes.AppStoreState,
	}, nil
}

// newestVersion picks the highest versionString, the client-side sort the
// unordered appStoreVersions collection forces on every reader of it.
func newestVersion(versions []appStoreVersion) (appStoreVersion, bool) {
	best := -1
	for i := range versions {
		if best == -1 || versionLess(versions[best].Attributes.VersionString, versions[i].Attributes.VersionString) {
			best = i
		}
	}
	if best == -1 {
		return appStoreVersion{}, false
	}
	return versions[best], true
}

// appStoreVersion is one row of the /v1/apps/{id}/appStoreVersions response —
// the same shape LatestVersion decodes, plus the resource id SubmitForReview
// and ReleaseVersion need to build relationships against.
type appStoreVersion struct {
	ID         string `json:"id"`
	Attributes struct {
		VersionString string `json:"versionString"`
		AppStoreState string `json:"appStoreState"`
		CreatedDate   string `json:"createdDate"`
	} `json:"attributes"`
}

// listAppStoreVersions fetches every appStoreVersions row for appID, following
// ASC's paging cursor to the end of the collection.
//
// Reading only the first page was a slow-fuse failure: this system auto-
// releases on every merged PR, so the version count only grows, and once it
// passed one page ReleaseVersion stopped finding the PENDING_DEVELOPER_RELEASE
// version (blocking every further release with "no version is pending
// developer release") and SubmitForReview stopped finding the editable version
// to reuse, creating a second one with a duplicate versionString that ASC
// rejects outright.
func (c *Client) listAppStoreVersions(ctx context.Context, appID string) ([]appStoreVersion, error) {
	return listAll[appStoreVersion](ctx, c, "/v1/apps/"+url.PathEscape(appID)+"/appStoreVersions")
}

// editableAppStoreStates are the appStoreState values ASC allows a version's
// metadata (and submission) to still be edited in — reusing a version in one
// of these states avoids ASC's rejection of a second version with the same
// versionString.
var editableAppStoreStates = map[string]bool{
	"PREPARE_FOR_SUBMISSION": true,
	"DEVELOPER_REJECTED":     true,
	"REJECTED":               true,
	"METADATA_REJECTED":      true,
}

// SubmitForReview submits version for App Store review. ASC has no "submit
// the app" operation, only "submit a version": an editable version already
// carrying versionString == version is reused (ASC rejects creating a second
// version with the same string), otherwise a new version is created first.
func (c *Client) SubmitForReview(ctx context.Context, appID, version string) error {
	return c.submitVersionForReview(ctx, appID, version, "")
}

// submitVersionForReview is SubmitForReview with the build to ship named
// explicitly. An empty buildID leaves whatever build the version already
// carries alone, which is what the port's SubmitForReview means: the caller
// knows a version string and nothing about builds.
//
// The exported method cannot take the build id because port.AppStoreClient
// fixes its signature.
func (c *Client) submitVersionForReview(ctx context.Context, appID, version, buildID string) error {
	versions, err := c.listAppStoreVersions(ctx, appID)
	if err != nil {
		return err
	}

	versionID := ""
	for _, v := range versions {
		if v.Attributes.VersionString == version && editableAppStoreStates[v.Attributes.AppStoreState] {
			versionID = v.ID
			break
		}
	}

	if versionID == "" {
		body := map[string]any{
			"data": map[string]any{
				"type": "appStoreVersions",
				"attributes": map[string]any{
					"versionString": version,
					"platform":      "IOS",
				},
				"relationships": map[string]any{
					"app": map[string]any{
						"data": map[string]any{"type": "apps", "id": appID},
					},
				},
			},
		}
		var resp struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := c.do(ctx, http.MethodPost, "/v1/appStoreVersions", body, &resp); err != nil {
			return err
		}
		versionID = resp.Data.ID
	}

	if buildID != "" {
		if err := c.attachBuild(ctx, versionID, buildID); err != nil {
			return err
		}
	}

	body := map[string]any{
		"data": map[string]any{
			"type": "appStoreVersionSubmissions",
			"relationships": map[string]any{
				"appStoreVersion": map[string]any{
					"data": map[string]any{"type": "appStoreVersions", "id": versionID},
				},
			},
		},
	}
	return c.do(ctx, http.MethodPost, "/v1/appStoreVersionSubmissions", body, nil)
}

// attachBuild points versionID's build relationship at buildID.
//
// ASC rejects an appStoreVersionSubmissions POST for a version with no build
// attached, and neither the appStoreVersions create body nor the submission
// itself can carry one — the relationship is only settable through this PATCH.
// Being a to-one relationship, the PATCH replaces rather than appends, so
// re-attaching the build a version already carries is a no-op.
func (c *Client) attachBuild(ctx context.Context, versionID, buildID string) error {
	body := map[string]any{
		"data": map[string]any{"type": "builds", "id": buildID},
	}
	path := "/v1/appStoreVersions/" + url.PathEscape(versionID) + "/relationships/build"
	if err := c.do(ctx, http.MethodPatch, path, body, nil); err != nil {
		return fmt.Errorf("appstore: attaching build %s to version %s (ASC will not accept a submission without one): %w", buildID, versionID, err)
	}
	return nil
}

// ReleaseVersion releases the version Apple is holding in
// PENDING_DEVELOPER_RELEASE. Releasing a version Apple has not approved is
// not a thing ASC supports, so the absence of one is reported directly rather
// than left to surface as an opaque API failure.
func (c *Client) ReleaseVersion(ctx context.Context, appID string) error {
	versions, err := c.listAppStoreVersions(ctx, appID)
	if err != nil {
		return err
	}

	versionID := ""
	for _, v := range versions {
		if v.Attributes.AppStoreState == "PENDING_DEVELOPER_RELEASE" {
			versionID = v.ID
			break
		}
	}
	if versionID == "" {
		return fmt.Errorf("appstore: no version is pending developer release for app %s", appID)
	}

	body := map[string]any{
		"data": map[string]any{
			"type": "appStoreVersionReleaseRequests",
			"relationships": map[string]any{
				"appStoreVersion": map[string]any{
					"data": map[string]any{"type": "appStoreVersions", "id": versionID},
				},
			},
		},
	}
	return c.do(ctx, http.MethodPost, "/v1/appStoreVersionReleaseRequests", body, nil)
}

// versionLess reports whether marketing version a sorts before b. Dotted
// components compare numerically when both sides parse as integers (so 1.10
// beats 1.9, which a plain string compare gets backwards) and
// lexicographically otherwise, which also handles a missing trailing
// component ("1.2" < "1.2.4").
func versionLess(a, b string) bool {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var ap, bp string
		if i < len(as) {
			ap = as[i]
		}
		if i < len(bs) {
			bp = bs[i]
		}
		an, aerr := strconv.Atoi(ap)
		bn, berr := strconv.Atoi(bp)
		if aerr == nil && berr == nil {
			if an != bn {
				return an < bn
			}
			continue
		}
		if ap != bp {
			return ap < bp
		}
	}
	return false
}
