package googleplay

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// defaultBaseURL is the Play Developer API's production host.
const defaultBaseURL = "https://androidpublisher.googleapis.com"

// defaultReportingBaseURL is the Play Developer Reporting API's production
// host. It is a different service from the Play Developer API, not a
// different path on it: enumerating the account's apps exists only there.
const defaultReportingBaseURL = "https://playdeveloperreporting.googleapis.com"

// defaultTokenURL is Google's OAuth 2.0 token endpoint.
const defaultTokenURL = "https://oauth2.googleapis.com/token"

// tokenRefreshMargin re-fetches the access token this long before it
// actually expires, so a request started just under the wire never races
// an expiring token.
const tokenRefreshMargin = 30 * time.Second

// cachedToken is one scope's access token and the moment it stops being
// usable.
type cachedToken struct {
	token string
	exp   time.Time
}

// Client is a thin Google Play Developer API client. It carries the service
// account's RSA signing key and exchanges it for short-lived OAuth access
// tokens on demand (cached until close to expiry — the exchange is a network
// round trip, no reason to redo it on every call).
type Client struct {
	clientEmail string
	privKey     *rsa.PrivateKey

	baseURL          string
	reportingBaseURL string
	tokenURL         string
	httpClient       *http.Client

	mu     sync.Mutex
	tokens map[string]cachedToken
}

var _ port.GooglePlayClient = (*Client)(nil)

// New builds a Client from a decrypted Google Play store credential.
// cred.Data must carry service_account_json — the full JSON key file
// downloaded for the service account, PKCS8 PEM private key included.
func New(cred domain.StoreCredential) (*Client, error) {
	raw := cred.Data["service_account_json"]
	if raw == "" {
		return nil, errors.New("googleplay: credential missing service_account_json")
	}
	clientEmail, priv, err := parseServiceAccountJSON(raw)
	if err != nil {
		return nil, err
	}
	return &Client{
		clientEmail:      clientEmail,
		privKey:          priv,
		baseURL:          defaultBaseURL,
		reportingBaseURL: defaultReportingBaseURL,
		tokenURL:         defaultTokenURL,
		httpClient:       &http.Client{Timeout: 30 * time.Second},
		tokens:           make(map[string]cachedToken, 2),
	}, nil
}

// SetBaseURL points the client at a different Play Developer API host;
// used by tests.
func (c *Client) SetBaseURL(u string) {
	c.baseURL = u
}

// SetReportingBaseURL points the client at a different Play Developer
// Reporting API host; used by tests.
func (c *Client) SetReportingBaseURL(u string) {
	c.reportingBaseURL = u
}

// SetTokenURL points the client at a different OAuth token endpoint; used
// by tests.
func (c *Client) SetTokenURL(u string) {
	c.tokenURL = u
}

// bearerToken returns a cached access token for scope if it still has life
// left, exchanging a fresh JWT assertion for one otherwise. Never logged —
// callers only ever see it inside the Authorization header.
//
// One token per scope rather than one assertion asking for both: the token
// endpoint refuses an assertion whole (invalid_scope) rather than granting
// the scopes it recognizes, so a single token would put the optional app
// listing in a position to take every deploy — and ValidateAuth with it —
// down. Separately minted, a refused reporting scope costs only ListApps.
func (c *Client) bearerToken(ctx context.Context, scope string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.tokens[scope]; ok && time.Now().Before(cached.exp.Add(-tokenRefreshMargin)) {
		return cached.token, nil
	}
	tok, exp, err := fetchAccessToken(ctx, c.httpClient, c.tokenURL, c.clientEmail, scope, c.privKey)
	if err != nil {
		return "", err
	}
	c.tokens[scope] = cachedToken{token: tok, exp: exp}
	return tok, nil
}

// apiError is a non-2xx Play Developer API response, carrying the status
// and a body snippet for diagnostics.
type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("googleplay api: %d %s", e.Status, e.Body)
}

// do issues a JSON request against the Play Developer API, decoding the
// response body into out (if non-nil) on success and returning an
// *apiError carrying the status and a body snippet on any non-2xx
// response.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	return c.doAt(ctx, c.baseURL, androidPublisherScope, method, path, body, out)
}

// doAt is do against an explicit host and scope, so the Play Developer
// Reporting API can share the error shape and body handling with the Play
// Developer API rather than growing a second HTTP path of its own. Host and
// scope travel together because they always match: each service only accepts
// its own scope's token.
func (c *Client) doAt(ctx context.Context, baseURL, scope, method, path string, body, out any) error {
	tok, err := c.bearerToken(ctx, scope)
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

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
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

// ValidateAuth confirms the service account's credentials actually
// authenticate against Google's OAuth token endpoint. It only performs the
// token exchange — no Play Developer API call — so it doesn't depend on
// any app already existing. Only the publisher scope is asked for: that is
// the one every deploy needs, and a credential must not be rejected at save
// time over a scope only the optional app listing uses.
func (c *Client) ValidateAuth(ctx context.Context) error {
	_, err := c.bearerToken(ctx, androidPublisherScope)
	return err
}

// The two ways edits.insert says "not this app". They are separate sentinels
// because they send an operator to opposite fixes: a 404 means the package
// name is wrong or the app was never created, a 403 means the app is there
// and this service account was never granted (or has lost) access to it in
// the Play Console. Reporting the second as the first has the operator
// re-checking a package name that is already correct.
var (
	errAppNotFound  = errors.New("googleplay: app not found")
	errAppForbidden = errors.New("googleplay: this credential cannot access the app")
)

// insertEdit opens a new edit transaction for packageName — the Play
// Developer API's unit of work: every read or write against an app's
// listing, tracks, or releases happens inside one. A 404 yields
// errAppNotFound and a 403 errAppForbidden; any other failure — notably 401,
// broken auth — is returned as-is so callers never confuse "app absent" with
// "auth broken".
func (c *Client) insertEdit(ctx context.Context, packageName string) (editID string, err error) {
	var resp struct {
		ID string `json:"id"`
	}
	path := "/androidpublisher/v3/applications/" + url.PathEscape(packageName) + "/edits"
	if err := c.do(ctx, http.MethodPost, path, nil, &resp); err != nil {
		var apiErr *apiError
		if errors.As(err, &apiErr) {
			switch apiErr.Status {
			case http.StatusNotFound:
				return "", fmt.Errorf("%w: %q", errAppNotFound, packageName)
			case http.StatusForbidden:
				return "", fmt.Errorf("%w: %q", errAppForbidden, packageName)
			}
		}
		return "", err
	}
	return resp.ID, nil
}

// deleteEdit discards edit editID. Edits opened purely to probe app/track
// state (AppExists, TrackInfo) must never be committed — deleteEdit is
// always invoked via defer immediately after a successful insertEdit, so
// it runs on every subsequent return path, including errors, and a probe
// never leaks a draft edit into the real Play console. Best-effort: a
// delete failure here doesn't change the caller's already-determined
// result, and there is nothing further this package can safely retry it
// with.
func (c *Client) deleteEdit(ctx context.Context, packageName, editID string) {
	path := "/androidpublisher/v3/applications/" + url.PathEscape(packageName) + "/edits/" + url.PathEscape(editID)
	if err := c.do(ctx, http.MethodDelete, path, nil, nil); err != nil {
		// Best-effort by design: the caller's result already stands and there
		// is nothing safe to retry with. Still worth a line — a run of these
		// means abandoned draft edits are piling up in the Play console.
		log.Warn().Err(err).Str("package", packageName).Msg("googleplay: discarding probe edit failed")
	}
}

// AppExists reports whether packageName is registered as an app this
// credential can access. There is no dedicated "does this app exist"
// endpoint on the Play Developer API, so this probes with edits.insert
// (which 404s/403s for an app the credential can't see) and immediately
// discards the edit it opens.
func (c *Client) AppExists(ctx context.Context, packageName string) (bool, error) {
	editID, err := c.insertEdit(ctx, packageName)
	if err != nil {
		// A forbidden app answers "no" here on purpose, unlike the paths that
		// go on to read or write it: the question this answers is whether
		// this credential can work with the app, and one it cannot reach is
		// no more usable than one that does not exist.
		if errors.Is(err, errAppNotFound) || errors.Is(err, errAppForbidden) {
			return false, nil
		}
		return false, err
	}
	defer c.deleteEdit(ctx, packageName, editID)
	return true, nil
}

// TrackInfo returns track's newest release, or the zero value (HasRelease
// false) if the track has none. Like AppExists, this opens an edit purely
// to read from it and always discards it before returning, including when
// the tracks.get call itself fails.
func (c *Client) TrackInfo(ctx context.Context, packageName, track string) (port.PlayTrackInfo, error) {
	editID, err := c.insertEdit(ctx, packageName)
	if err != nil {
		return port.PlayTrackInfo{}, err
	}
	defer c.deleteEdit(ctx, packageName, editID)

	var resp struct {
		Releases []struct {
			Name         string  `json:"name"`
			Status       string  `json:"status"`
			UserFraction float64 `json:"userFraction"`
		} `json:"releases"`
	}
	path := "/androidpublisher/v3/applications/" + url.PathEscape(packageName) + "/edits/" + url.PathEscape(editID) + "/tracks/" + url.PathEscape(track)
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return port.PlayTrackInfo{}, err
	}
	if len(resp.Releases) == 0 {
		return port.PlayTrackInfo{}, nil
	}
	r := resp.Releases[0]
	return port.PlayTrackInfo{
		HasRelease:   true,
		VersionName:  r.Name,
		Status:       r.Status,
		UserFraction: r.UserFraction,
	}, nil
}

// withEdit runs fn inside a Play edit transaction and COMMITS it. Unlike the
// probe path (AppExists, TrackInfo) which always discards, a write must
// commit or it never happened. The edit is discarded only when fn fails, so
// a half-applied mutation never reaches the console.
func (c *Client) withEdit(ctx context.Context, packageName string, fn func(editID string) error) error {
	editID, err := c.insertEdit(ctx, packageName)
	if err != nil {
		return err
	}
	if err := fn(editID); err != nil {
		c.deleteEdit(ctx, packageName, editID)
		return err
	}
	path := "/androidpublisher/v3/applications/" + url.PathEscape(packageName) + "/edits/" + url.PathEscape(editID) + ":commit"
	return c.do(ctx, http.MethodPost, path, nil, nil)
}

// releasePayload builds a tracks.update release. Play rejects a completed
// release that carries a userFraction, and treats a partial release without
// inProgress as a full rollout — so the status and the fraction have to be
// decided together, in one place.
func releasePayload(name string, versionCodes []string, fraction float64, halted bool) map[string]any {
	rel := map[string]any{"name": name, "versionCodes": versionCodes}
	switch {
	case halted:
		rel["status"] = "halted"
		if fraction > 0 && fraction < 1 {
			rel["userFraction"] = fraction
		}
	case fraction >= 1 || fraction <= 0:
		rel["status"] = "completed"
	default:
		rel["status"] = "inProgress"
		rel["userFraction"] = fraction
	}
	return map[string]any{"releases": []any{rel}}
}

// trackRelease is a track's newest release as read inside an edit, carrying
// the fields a write needs to rebuild the release (versionCodes included) —
// port.PlayTrackInfo deliberately omits versionCodes since read-only callers
// have no use for them.
type trackRelease struct {
	Name         string
	VersionCodes []string
	Status       string
	UserFraction float64
}

// getTrackRelease reads track's newest release inside the already-open edit
// editID. It returns nil (no error) when the track has no release.
func (c *Client) getTrackRelease(ctx context.Context, packageName, editID, track string) (*trackRelease, error) {
	var resp struct {
		Releases []struct {
			Name         string   `json:"name"`
			Status       string   `json:"status"`
			UserFraction float64  `json:"userFraction"`
			VersionCodes []string `json:"versionCodes"`
		} `json:"releases"`
	}
	path := "/androidpublisher/v3/applications/" + url.PathEscape(packageName) + "/edits/" + url.PathEscape(editID) + "/tracks/" + url.PathEscape(track)
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	if len(resp.Releases) == 0 {
		return nil, nil
	}
	r := resp.Releases[0]
	return &trackRelease{Name: r.Name, VersionCodes: r.VersionCodes, Status: r.Status, UserFraction: r.UserFraction}, nil
}

// putTrack writes payload (built by releasePayload) to track inside the
// already-open edit editID.
func (c *Client) putTrack(ctx context.Context, packageName, editID, track string, payload map[string]any) error {
	path := "/androidpublisher/v3/applications/" + url.PathEscape(packageName) + "/edits/" + url.PathEscape(editID) + "/tracks/" + url.PathEscape(track)
	return c.do(ctx, http.MethodPut, path, payload, nil)
}

// PromoteTrack copies fromTrack's current release onto toTrack, staged at
// userFraction, and commits the edit. It errors — rather than silently
// no-op'ing — when fromTrack has no release, since that would otherwise look
// like a successful promotion.
func (c *Client) PromoteTrack(ctx context.Context, packageName, fromTrack, toTrack string, userFraction float64) error {
	return c.withEdit(ctx, packageName, func(editID string) error {
		src, err := c.getTrackRelease(ctx, packageName, editID, fromTrack)
		if err != nil {
			return err
		}
		if src == nil {
			return fmt.Errorf("googleplay: track %q has no release to promote", fromTrack)
		}
		payload := releasePayload(src.Name, src.VersionCodes, userFraction, false)
		return c.putTrack(ctx, packageName, editID, toTrack, payload)
	})
}

// SetRolloutFraction dials track's in-flight release to userFraction and
// commits the edit.
func (c *Client) SetRolloutFraction(ctx context.Context, packageName, track string, userFraction float64) error {
	return c.withEdit(ctx, packageName, func(editID string) error {
		rel, err := c.getTrackRelease(ctx, packageName, editID, track)
		if err != nil {
			return err
		}
		if rel == nil {
			return fmt.Errorf("googleplay: track %q has no release", track)
		}
		payload := releasePayload(rel.Name, rel.VersionCodes, userFraction, false)
		return c.putTrack(ctx, packageName, editID, track, payload)
	})
}

// HaltRollout stops track's release in place, preserving its current
// userFraction so ResumeRollout can restore exactly what was rolling out.
func (c *Client) HaltRollout(ctx context.Context, packageName, track string) error {
	return c.withEdit(ctx, packageName, func(editID string) error {
		rel, err := c.getTrackRelease(ctx, packageName, editID, track)
		if err != nil {
			return err
		}
		if rel == nil {
			return fmt.Errorf("googleplay: track %q has no release to halt", track)
		}
		payload := releasePayload(rel.Name, rel.VersionCodes, rel.UserFraction, true)
		return c.putTrack(ctx, packageName, editID, track, payload)
	})
}

// ResumeRollout un-halts track's release at the fraction it was halted at.
func (c *Client) ResumeRollout(ctx context.Context, packageName, track string) error {
	return c.withEdit(ctx, packageName, func(editID string) error {
		rel, err := c.getTrackRelease(ctx, packageName, editID, track)
		if err != nil {
			return err
		}
		if rel == nil {
			return fmt.Errorf("googleplay: track %q has no release to resume", track)
		}
		payload := releasePayload(rel.Name, rel.VersionCodes, rel.UserFraction, false)
		return c.putTrack(ctx, packageName, editID, track, payload)
	})
}

// Play's own track names. External is not one of them: it is this package's
// fold of the two testing tracks, which is why it has no constant here.
const (
	trackInternal   = "internal"
	trackAlpha      = "alpha"
	trackBeta       = "beta"
	trackProduction = "production"
)

// Wording for TrackRelease.Audience on the external channel. Play's two
// testing tracks are one product channel, so the channel has to say which
// of them it is actually reporting.
const (
	audienceOpenTesting   = "Open testing"
	audienceClosedTesting = "Closed testing"
)

// listAppsPageSize is the reporting API's maximum page size for apps.search.
const listAppsPageSize = 100

// listAppsMaxPages bounds the apps.search walk. At listAppsPageSize apps a
// page this is 5000 apps, well past what any real developer account holds, so
// reaching it does not mean "a big account", it means a cursor that stopped
// advancing. The "same token twice" check below cannot stand alone: a token
// that alternates A→B→A never repeats consecutively and would page forever.
const listAppsMaxPages = 50

// ListApps enumerates the apps this service account can see, via the Play
// Developer Reporting API — the Play Developer API has no listing endpoint
// at all. StoreAppRef.StoreAppID stays empty on purpose: Play keys apps by
// package name and has no second identifier to report.
//
// A 403 (reporting API not enabled on the project, or the service account
// not granted access) or 404 is not a failure but the answer "this
// credential cannot enumerate", returned as port.ErrAppListingUnavailable so
// callers fall back to asking for the package name by hand. A 401 is real
// broken auth and stays an error.
func (c *Client) ListApps(ctx context.Context) ([]port.StoreAppRef, error) {
	// Minted up front so a reporting scope Google will not grant reads as
	// "cannot enumerate" rather than as a broken credential: the publisher
	// token every deploy runs on is a separate exchange and is untouched by
	// this failing, so the honest answer here is the manual-entry fallback.
	if _, err := c.bearerToken(ctx, playReportingScope); err != nil {
		return nil, fmt.Errorf("googleplay: play developer reporting scope: %v: %w", err, port.ErrAppListingUnavailable)
	}

	var apps []port.StoreAppRef
	pageToken := ""
	for page := 0; ; page++ {
		if page >= listAppsMaxPages {
			return nil, fmt.Errorf("googleplay: apps:search: still paging after %d pages, refusing to follow the cursor further", listAppsMaxPages)
		}

		// apps.search is a GET despite the `:search` custom-method spelling —
		// it takes no request body, only pageSize/pageToken.
		path := fmt.Sprintf("/v1beta1/apps:search?pageSize=%d", listAppsPageSize)
		if pageToken != "" {
			path += "&pageToken=" + url.QueryEscape(pageToken)
		}

		var resp struct {
			Apps []struct {
				PackageName string `json:"packageName"`
				DisplayName string `json:"displayName"`
			} `json:"apps"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := c.doAt(ctx, c.reportingBaseURL, playReportingScope, http.MethodGet, path, nil, &resp); err != nil {
			var apiErr *apiError
			if errors.As(err, &apiErr) && (apiErr.Status == http.StatusForbidden || apiErr.Status == http.StatusNotFound) {
				return nil, fmt.Errorf("googleplay: play developer reporting api: %w", port.ErrAppListingUnavailable)
			}
			return nil, err
		}

		for _, a := range resp.Apps {
			if a.PackageName == "" {
				continue
			}
			apps = append(apps, port.StoreAppRef{Identifier: a.PackageName, Name: a.DisplayName})
		}

		// A repeated token would loop forever inside the page ceiling; the
		// normal exit has to come from the token actually advancing.
		if resp.NextPageToken == "" || resp.NextPageToken == pageToken {
			return apps, nil
		}
		pageToken = resp.NextPageToken
	}
}

// playRelease is one release of one track as tracks.list reports it.
type playRelease struct {
	Name         string   `json:"name"`
	Status       string   `json:"status"`
	UserFraction float64  `json:"userFraction"`
	VersionCodes []string `json:"versionCodes"`
}

// Tracks reads all of an app's tracks in ONE edit. Play's edit quota is
// narrow, so this uses tracks.list rather than a tracks.get per track: one
// request covers internal, alpha, beta and production, and a track the app
// has never used is simply absent instead of 404ing. Like the other probes
// the edit is always discarded, never committed.
func (c *Client) Tracks(ctx context.Context, packageName string) (domain.StoreTracks, error) {
	editID, err := c.insertEdit(ctx, packageName)
	if err != nil {
		return domain.StoreTracks{}, err
	}
	defer c.deleteEdit(ctx, packageName, editID)

	var resp struct {
		Tracks []struct {
			Track    string        `json:"track"`
			Releases []playRelease `json:"releases"`
		} `json:"tracks"`
	}
	path := "/androidpublisher/v3/applications/" + url.PathEscape(packageName) + "/edits/" + url.PathEscape(editID) + "/tracks"
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return domain.StoreTracks{}, err
	}

	// Keyed on the exact track name so form-factor tracks (`wear:production`,
	// `automotive:production`) never stand in for the phone track.
	newest := make(map[string]playRelease, len(resp.Tracks))
	for _, t := range resp.Tracks {
		if rel, ok := newestRelease(t.Releases); ok {
			newest[t.Track] = rel
		}
	}

	tracks := domain.StoreTracks{
		Internal:   emptyChannel(),
		External:   externalChannel(newest),
		Production: emptyChannel(),
	}
	if rel, ok := newest[trackInternal]; ok {
		tracks.Internal = channelFrom(rel, "")
	}
	if rel, ok := newest[trackProduction]; ok {
		tracks.Production = channelFrom(rel, "")
	}
	return tracks, nil
}

// externalChannel folds Play's two testing tracks into the single external
// channel. With a release on both, the newer one wins — newer meaning the
// higher version code, since Play reports no timestamp on a track release —
// and beta takes a tie because open testing is the later stage.
func externalChannel(newest map[string]playRelease) domain.TrackRelease {
	beta, hasBeta := newest[trackBeta]
	alpha, hasAlpha := newest[trackAlpha]
	switch {
	case hasBeta && hasAlpha:
		_, betaCode := highestVersionCode(beta.VersionCodes)
		_, alphaCode := highestVersionCode(alpha.VersionCodes)
		if alphaCode > betaCode {
			return channelFrom(alpha, audienceClosedTesting)
		}
		return channelFrom(beta, audienceOpenTesting)
	case hasBeta:
		return channelFrom(beta, audienceOpenTesting)
	case hasAlpha:
		return channelFrom(alpha, audienceClosedTesting)
	}
	return emptyChannel()
}

// emptyChannel is a channel nothing has ever reached. TrackStatusNone is
// stated rather than left zero — "" would read as a status we failed to map.
func emptyChannel() domain.TrackRelease {
	return domain.TrackRelease{Status: domain.TrackStatusNone}
}

// channelFrom converts a Play release into the product's channel view.
// Version is the release name because the Play Developer API exposes no
// versionName anywhere on a track, an APK or a bundle — Play itself derives
// this name from the APK's version_name when the release does not set one.
func channelFrom(rel playRelease, audience string) domain.TrackRelease {
	build, _ := highestVersionCode(rel.VersionCodes)
	return domain.TrackRelease{
		HasRelease:   true,
		Version:      rel.Name,
		Build:        build,
		Status:       normalizeTrackStatus(rel.Status),
		UserFraction: rel.UserFraction,
		Audience:     audience,
	}
}

// newestRelease picks the release carrying the highest version code. A track
// holds every ACTIVE release, so a completed one commonly sits alongside a
// staged rollout — and the channel is about what is going out now.
func newestRelease(releases []playRelease) (playRelease, bool) {
	if len(releases) == 0 {
		return playRelease{}, false
	}
	best := releases[0]
	_, bestCode := highestVersionCode(best.VersionCodes)
	for _, rel := range releases[1:] {
		if _, code := highestVersionCode(rel.VersionCodes); code > bestCode {
			best, bestCode = rel, code
		}
	}
	return best, true
}

// highestVersionCode returns the largest version code in codes, both as the
// string Play sent and as a number for comparing two releases. Play types
// version codes as int64-formatted strings; an unparseable one is skipped,
// and -1 means the release carried no usable code at all.
func highestVersionCode(codes []string) (string, int64) {
	best, bestCode := "", int64(-1)
	for _, c := range codes {
		code, err := strconv.ParseInt(c, 10, 64)
		if err != nil || code <= bestCode {
			continue
		}
		best, bestCode = c, code
	}
	return best, bestCode
}

// normalizeTrackStatus maps Play's release status onto the product's
// vocabulary. An unrecognized status yields TrackStatusUnknown rather than
// TrackStatusNone: the release does exist, and calling the channel empty
// would be a worse answer than admitting the state is unknown. It must not
// be "" either — TrackRelease.Status is omitempty, so an empty string never
// reaches the client and the channel renders as the empty one anyway.
func normalizeTrackStatus(status string) string {
	switch status {
	case "completed":
		return domain.TrackStatusLive
	case "inProgress":
		return domain.TrackStatusRollingOut
	case "halted":
		return domain.TrackStatusHalted
	case "draft":
		return domain.TrackStatusDraft
	}
	return domain.TrackStatusUnknown
}
