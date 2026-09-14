package appstore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// appListingPage is one page of /v1/apps?include=appStoreVersions.
//
// It is decoded here instead of through listAll because listAll keeps only
// `data`, and the JSON:API `included` member is the whole point: Apple returns
// the related appStoreVersions once per page there, linked back to each app
// through relationships.appStoreVersions.data. Any other way of reading an
// app's review state costs a collection walk per app, on a listing whose job
// is to render a whole account catalogue.
type appListingPage struct {
	Data []struct {
		ID         string `json:"id"`
		Attributes struct {
			Name     string `json:"name"`
			BundleID string `json:"bundleId"`
		} `json:"attributes"`
		Relationships struct {
			AppStoreVersions struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
				Meta struct {
					Paging struct {
						Total *int `json:"total"`
					} `json:"paging"`
				} `json:"meta"`
			} `json:"appStoreVersions"`
		} `json:"relationships"`
	} `json:"data"`
	Included []struct {
		Type       string `json:"type"`
		ID         string `json:"id"`
		Attributes struct {
			VersionString string `json:"versionString"`
			AppStoreState string `json:"appStoreState"`
		} `json:"attributes"`
	} `json:"included"`
	Links struct {
		Next string `json:"next"`
	} `json:"links"`
}

// ascIncludedVersionsLimit caps the appStoreVersions ASC inlines per app row.
// 50 is the ceiling ASC accepts on a relationship limit; left unset, ASC
// applies a smaller undocumented one and says nothing about it.
const ascIncludedVersionsLimit = 50

// ListApps enumerates every app the API key can see.
func (c *Client) ListApps(ctx context.Context) ([]port.StoreAppRef, error) {
	var refs []port.StoreAppRef
	path := "/v1/apps?include=appStoreVersions&fields[appStoreVersions]=versionString,appStoreState" +
		"&limit[appStoreVersions]=" + strconv.Itoa(ascIncludedVersionsLimit)
	for page := 0; path != ""; page++ {
		if page >= ascMaxCollectionPages {
			return nil, fmt.Errorf("appstore: %s: still paging after %d pages, refusing to follow the cursor further", path, ascMaxCollectionPages)
		}
		var body appListingPage
		if err := c.do(ctx, http.MethodGet, path, nil, &body); err != nil {
			return nil, err
		}

		included := make(map[string]appStoreVersion, len(body.Included))
		for _, inc := range body.Included {
			if inc.Type != "appStoreVersions" {
				continue
			}
			var v appStoreVersion
			v.ID = inc.ID
			v.Attributes.VersionString = inc.Attributes.VersionString
			v.Attributes.AppStoreState = inc.Attributes.AppStoreState
			included[inc.ID] = v
		}

		for _, row := range body.Data {
			rel := row.Relationships.AppStoreVersions
			related := make([]appStoreVersion, 0, len(rel.Data))
			for _, ref := range rel.Data {
				if v, ok := included[ref.ID]; ok {
					related = append(related, v)
				}
			}
			// No linkage, or an `included` that does not carry the versions
			// this app points at, leaves State empty. There is nothing to read
			// it from, and on a picker a guessed state is indistinguishable
			// from a known one.
			//
			// A total above what the relationship actually listed is the same
			// situation: ASC documents no ordering for this relationship, so
			// the rows that did come back are an arbitrary slice of the app's
			// history and the newest of them can be months stale.
			state := ""
			truncated := rel.Meta.Paging.Total != nil && *rel.Meta.Paging.Total > len(rel.Data)
			if newest, found := newestVersion(related); found && !truncated {
				state = newest.Attributes.AppStoreState
			}
			refs = append(refs, port.StoreAppRef{
				StoreAppID: row.ID,
				Identifier: row.Attributes.BundleID,
				Name:       row.Attributes.Name,
				State:      state,
			})
		}

		next, err := nextPagePath(body.Links.Next)
		if err != nil {
			return nil, err
		}
		path = next
	}
	return refs, nil
}

type betaGroup struct {
	ID         string `json:"id"`
	Attributes struct {
		IsInternalGroup bool `json:"isInternalGroup"`
	} `json:"attributes"`
}

// buildRow is one /v1/builds row. Attributes.Version is the BUILD number
// (CFBundleVersion), not the marketing version a person recognises — that one
// only exists on the build's preReleaseVersion relationship.
type buildRow struct {
	ID         string `json:"id"`
	Attributes struct {
		Version      string `json:"version"`
		UploadedDate string `json:"uploadedDate"`
	} `json:"attributes"`
}

// Tracks reads the three channels for appID.
func (c *Client) Tracks(ctx context.Context, appID string) (domain.StoreTracks, error) {
	internalGroups, externalGroups, err := c.betaGroups(ctx, appID)
	if err != nil {
		return domain.StoreTracks{}, err
	}

	internal, err := c.betaTrack(ctx, appID, internalGroups, domain.StoreChannelInternal)
	if err != nil {
		return domain.StoreTracks{}, err
	}
	external, err := c.betaTrack(ctx, appID, externalGroups, domain.StoreChannelExternal)
	if err != nil {
		return domain.StoreTracks{}, err
	}
	production, err := c.productionTrack(ctx, appID)
	if err != nil {
		return domain.StoreTracks{}, err
	}
	return domain.StoreTracks{Internal: internal, External: external, Production: production}, nil
}

func (c *Client) betaGroups(ctx context.Context, appID string) (internal, external []betaGroup, err error) {
	groups, err := listAll[betaGroup](ctx, c, "/v1/apps/"+url.PathEscape(appID)+"/betaGroups")
	if err != nil {
		return nil, nil, err
	}
	for _, g := range groups {
		if g.Attributes.IsInternalGroup {
			internal = append(internal, g)
			continue
		}
		external = append(external, g)
	}
	return internal, external, nil
}

// emptyChannel is what a channel nothing has ever reached looks like. Stated
// rather than left as the zero TrackRelease so the status is never "".
func emptyChannel() domain.TrackRelease {
	return domain.TrackRelease{Status: domain.TrackStatusNone}
}

func (c *Client) betaTrack(ctx context.Context, appID string, groups []betaGroup, channel string) (domain.TrackRelease, error) {
	if len(groups) == 0 {
		return emptyChannel(), nil
	}
	build, found, err := c.newestGroupBuild(ctx, appID, groups)
	if err != nil {
		return domain.TrackRelease{}, err
	}
	if !found {
		return emptyChannel(), nil
	}
	version, err := c.buildMarketingVersion(ctx, build.ID)
	if err != nil {
		return domain.TrackRelease{}, err
	}

	// An internal group receives a build the moment it is processed; only the
	// external channel goes through Beta App Review.
	status := domain.TrackStatusLive
	if channel == domain.StoreChannelExternal {
		status, err = c.betaReviewStatus(ctx, build.ID)
		if err != nil {
			return domain.TrackRelease{}, err
		}
	}

	rel := domain.TrackRelease{
		HasRelease: true,
		Version:    version,
		Build:      build.Attributes.Version,
		Status:     status,
		Audience:   c.betaAudience(ctx, groups, channel),
	}
	if build.Attributes.UploadedDate != "" {
		uploaded, err := parseASCTime(build.Attributes.UploadedDate)
		if err != nil {
			return domain.TrackRelease{}, err
		}
		rel.UpdatedAt = &uploaded
	}
	return rel, nil
}

// newestGroupBuild returns the newest build assigned to any of groups.
//
// Unlike appStoreVersions, /v1/builds documents both filter[betaGroups] and
// sort=-uploadedDate, so the newest row genuinely is row 0 and limit=1 is one
// request instead of a walk over the app's entire build history. The group
// filter is what keeps the two TestFlight channels apart: without it an
// internal-only build would be reported as sitting on external too.
func (c *Client) newestGroupBuild(ctx context.Context, appID string, groups []betaGroup) (buildRow, bool, error) {
	if len(groups) == 0 {
		return buildRow{}, false, nil
	}
	path := "/v1/builds?filter[app]=" + url.QueryEscape(appID) +
		"&filter[betaGroups]=" + url.QueryEscape(groupIDs(groups)) +
		"&sort=-uploadedDate&limit=1"
	var page jsonAPIPage[buildRow]
	if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
		return buildRow{}, false, err
	}
	if len(page.Data) == 0 {
		return buildRow{}, false, nil
	}
	return page.Data[0], true, nil
}

func groupIDs(groups []betaGroup) string {
	ids := make([]string, 0, len(groups))
	for _, g := range groups {
		ids = append(ids, g.ID)
	}
	return strings.Join(ids, ",")
}

// buildMarketingVersion resolves the build's preReleaseVersion, which is where
// ASC keeps the marketing version ("1.4.0"); the build's own attributes.version
// is the build number ("142"). A build with no preReleaseVersion yields "",
// never the build number — reporting a build number as a version is exactly the
// confusion this lookup exists to avoid.
func (c *Client) buildMarketingVersion(ctx context.Context, buildID string) (string, error) {
	var resp struct {
		Data struct {
			Attributes struct {
				Version string `json:"version"`
			} `json:"attributes"`
		} `json:"data"`
	}
	path := "/v1/builds/" + url.PathEscape(buildID) + "/preReleaseVersion"
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		if isNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return resp.Data.Attributes.Version, nil
}

func (c *Client) betaReviewStatus(ctx context.Context, buildID string) (string, error) {
	var resp struct {
		Data struct {
			Attributes struct {
				BetaReviewState string `json:"betaReviewState"`
			} `json:"attributes"`
		} `json:"data"`
	}
	path := "/v1/builds/" + url.PathEscape(buildID) + "/betaAppReviewSubmission"
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		// No submission resource at all means the build was never handed to
		// Beta App Review, so external testers cannot have it yet.
		if isNotFound(err) {
			return domain.TrackStatusDraft, nil
		}
		return "", err
	}
	switch resp.Data.Attributes.BetaReviewState {
	case "WAITING_FOR_REVIEW", "IN_REVIEW":
		return domain.TrackStatusInReview, nil
	case "APPROVED":
		return domain.TrackStatusLive, nil
	}
	return domain.TrackStatusDraft, nil
}

// betaAudience labels the channel with its tester count, falling back to the
// number of groups. The count is decoration on a panel; a credential that
// cannot read betaTesters (or an ASC response without paging totals) must not
// turn a readable track into an error, and the group count is still true.
func (c *Client) betaAudience(ctx context.Context, groups []betaGroup, channel string) string {
	if total, ok := c.betaTesterCount(ctx, groups); ok {
		return pluralize(total, channel+" tester")
	}
	return pluralize(len(groups), "group")
}

func (c *Client) betaTesterCount(ctx context.Context, groups []betaGroup) (int, bool) {
	var resp struct {
		Meta struct {
			Paging struct {
				Total *int `json:"total"`
			} `json:"paging"`
		} `json:"meta"`
	}
	path := "/v1/betaTesters?limit=1&filter[betaGroups]=" + url.QueryEscape(groupIDs(groups))
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return 0, false
	}
	if resp.Meta.Paging.Total == nil {
		return 0, false
	}
	return *resp.Meta.Paging.Total, true
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// liveAppStoreStates are the states in which a version is what users can
// actually get: on sale, or approved with only the release itself outstanding.
var liveAppStoreStates = map[string]bool{
	"READY_FOR_SALE":            true,
	"PENDING_APPLE_RELEASE":     true,
	"PENDING_DEVELOPER_RELEASE": true,
}

// inReviewAppStoreStates are the states of a version Apple is still looking at.
// Such a version is NOT on production, so it speaks for the channel only when
// nothing has ever gone live — on an app awaiting its first approval that is
// the whole story, and it outranks the draft behind it.
var inReviewAppStoreStates = map[string]bool{
	"IN_REVIEW":          true,
	"WAITING_FOR_REVIEW": true,
}

// productionVersion picks the version the production channel is about, in
// tiers: what users have, then what Apple is reviewing, then the newest draft.
//
// It is not simply the highest versionString. This system mints an
// appStoreVersion per merged PR, so a draft — and often a submission — numbered
// above the live one is the normal state, and reporting either of those tells
// an operator nothing is out while the app is on sale.
func productionVersion(versions []appStoreVersion) (appStoreVersion, bool) {
	for _, tier := range []map[string]bool{liveAppStoreStates, inReviewAppStoreStates} {
		matching := make([]appStoreVersion, 0, len(versions))
		for _, v := range versions {
			if tier[v.Attributes.AppStoreState] {
				matching = append(matching, v)
			}
		}
		if v, found := newestVersion(matching); found {
			return v, true
		}
	}
	return newestVersion(versions)
}

func (c *Client) productionTrack(ctx context.Context, appID string) (domain.TrackRelease, error) {
	versions, err := c.listAppStoreVersions(ctx, appID)
	if err != nil {
		return domain.TrackRelease{}, err
	}
	version, found := productionVersion(versions)
	if !found {
		return emptyChannel(), nil
	}

	rel := domain.TrackRelease{
		HasRelease: true,
		Version:    version.Attributes.VersionString,
		Build:      c.versionBuildNumber(ctx, version.ID),
		Status:     normalizeAppStoreState(version.Attributes.AppStoreState),
	}
	if version.Attributes.CreatedDate != "" {
		created, err := parseASCTime(version.Attributes.CreatedDate)
		if err != nil {
			return domain.TrackRelease{}, err
		}
		rel.UpdatedAt = &created
	}

	phased, err := c.phasedRelease(ctx, version.ID)
	if err != nil {
		return domain.TrackRelease{}, err
	}
	// appStoreState says READY_FOR_SALE from the moment a phased release
	// starts, so it alone cannot tell "everyone has it" from "1% of users have
	// it" — only the phased release resource can, and while it runs the
	// version is staged, not fully out.
	switch phased.State {
	case "ACTIVE":
		rel.Status = domain.TrackStatusRollingOut
		rel.UserFraction = phasedFraction(phased.Day)
	case "PAUSED":
		rel.Status = domain.TrackStatusHalted
		rel.UserFraction = phasedFraction(phased.Day)
	}
	return rel, nil
}

// versionBuildNumber is the build number the App Store version ships. Failures
// are swallowed for the same reason as the tester count: the panel shows iOS
// beside Android, where Play always supplies a versionCode, so an unreadable
// build number leaves that one cell blank instead of failing the channel.
func (c *Client) versionBuildNumber(ctx context.Context, versionID string) string {
	var resp struct {
		Data struct {
			Attributes struct {
				Version string `json:"version"`
			} `json:"attributes"`
		} `json:"data"`
	}
	path := "/v1/appStoreVersions/" + url.PathEscape(versionID) + "/build"
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return ""
	}
	return resp.Data.Attributes.Version
}

func normalizeAppStoreState(state string) string {
	switch state {
	case "READY_FOR_SALE":
		return domain.TrackStatusLive
	case "WAITING_FOR_REVIEW", "IN_REVIEW", "PENDING_APPLE_RELEASE":
		return domain.TrackStatusInReview
	case "PREPARE_FOR_SUBMISSION", "DEVELOPER_REJECTED", "PENDING_DEVELOPER_RELEASE":
		return domain.TrackStatusDraft
	}
	return domain.TrackStatusDraft
}

// phasedReleaseDayFractions is Apple's fixed phased-release schedule: the API
// reports which day the rollout is on, never the percentage, so the percentage
// can only come from this table.
var phasedReleaseDayFractions = []float64{0.01, 0.02, 0.05, 0.10, 0.20, 0.50, 1.00}

// phasedReleaseInfo is ASC's staged rollout for one version. An empty State
// means the version has none: it went to everyone at once.
type phasedReleaseInfo struct {
	State string
	Day   int
}

func (c *Client) phasedRelease(ctx context.Context, versionID string) (phasedReleaseInfo, error) {
	var resp struct {
		Data struct {
			Attributes struct {
				PhasedReleaseState string `json:"phasedReleaseState"`
				CurrentDayNumber   int    `json:"currentDayNumber"`
			} `json:"attributes"`
		} `json:"data"`
	}
	path := "/v1/appStoreVersions/" + url.PathEscape(versionID) + "/appStoreVersionPhasedRelease"
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		// A version released to everyone at once has no phased release
		// resource; ASC answers that with a 404 or a null data member.
		if isNotFound(err) {
			return phasedReleaseInfo{}, nil
		}
		return phasedReleaseInfo{}, err
	}
	return phasedReleaseInfo{
		State: resp.Data.Attributes.PhasedReleaseState,
		Day:   resp.Data.Attributes.CurrentDayNumber,
	}, nil
}

func phasedFraction(day int) float64 {
	if day < 1 {
		return 0
	}
	if day > len(phasedReleaseDayFractions) {
		return 1
	}
	return phasedReleaseDayFractions[day-1]
}

// PromoteChannel moves the build on `from` onto `to`.
func (c *Client) PromoteChannel(ctx context.Context, appID, from, to string) error {
	if next, ok := domain.NextChannel(from); !ok || next != to {
		return fmt.Errorf("appstore: cannot promote %q -> %q: the only promotions are internal -> external and external -> production", from, to)
	}

	internalGroups, externalGroups, err := c.betaGroups(ctx, appID)
	if err != nil {
		return err
	}

	if from == domain.StoreChannelInternal {
		if len(externalGroups) == 0 {
			return fmt.Errorf("appstore: app %s has no external TestFlight group to promote into", appID)
		}
		build, found, err := c.newestGroupBuild(ctx, appID, internalGroups)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("appstore: app %s has no build on the internal channel to promote", appID)
		}
		// Adding the build to an external group is what queues Beta App
		// Review. The review takes hours to days, so this returns once the
		// build is queued; nothing here waits for Apple.
		body := map[string]any{
			"data": []map[string]any{
				{"type": "builds", "id": build.ID},
			},
		}
		for _, g := range externalGroups {
			path := "/v1/betaGroups/" + url.PathEscape(g.ID) + "/relationships/builds"
			if err := c.do(ctx, http.MethodPost, path, body, nil); err != nil {
				return err
			}
		}
		return nil
	}

	build, found, err := c.newestGroupBuild(ctx, appID, externalGroups)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("appstore: app %s has no build on the external channel to promote", appID)
	}
	version, err := c.buildMarketingVersion(ctx, build.ID)
	if err != nil {
		return err
	}
	if version == "" {
		return fmt.Errorf("appstore: build %s carries no marketing version to submit for review", build.ID)
	}
	return c.submitVersionForReview(ctx, appID, version, build.ID)
}

func isNotFound(err error) bool {
	var apiErr *apiError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}
