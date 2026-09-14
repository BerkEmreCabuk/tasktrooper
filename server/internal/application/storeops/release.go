// Release methods drive a mobile store app's post-onboarding lifecycle from
// the operations console: submitting an iOS build for review, releasing an
// Apple-approved version, and promoting/dialing/halting/resuming an Android
// staged rollout. Every method follows the same shape: load the repository
// and the app row (a missing row propagates port.ErrNotFound so the handler
// 404s), reject unless the app's state permits the action, check the
// confirm phrase for production-class actions, call the store client, and
// write exactly one ops_audit_log row for the attempt — success or failure
// alike.
package storeops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Google Play tracks this service ever names explicitly. port.PlayTrackInfo
// documents these as the only two tracks the Play Developer API integration
// understands.
const (
	androidTrackInternal   = "internal"
	androidTrackProduction = "production"
)

// ErrConfirmMismatch means a production-class store release action's
// confirm field did not equal the repository's name exactly. storeops
// defines its own copy of this sentinel — identical in spirit to
// deployops.ErrConfirmMismatch — rather than importing application/deployops:
// sibling application packages must not depend on each other.
var ErrConfirmMismatch = errors.New("storeops: confirmation phrase does not match the repository name")

// ErrAppNotReady means a release action was refused because the mobile
// store app is not in the lifecycle state it requires (typically live; see
// each method's doc for the one exception). The row itself is fine — it is
// just not ready for this particular action yet, so the HTTP layer answers
// 409, not 500 or 404.
var ErrAppNotReady = errors.New("storeops: mobile store app is not ready for this action")

// ErrInvalidRolloutFraction means a caller-supplied Android rollout fraction
// fell outside [0,1]. The fraction is user input heading straight for a
// production Play Developer API call, so it is clamped here rather than
// trusting the browser to have done it.
var ErrInvalidRolloutFraction = errors.New("storeops: rollout fraction must be between 0 and 1")

// ErrStoreCredentialUnavailable means a release action could not build the
// store client it needs — the provider's credential was never saved, or it
// failed to load/decrypt. It always wraps the underlying s.asc/s.play
// error, so the detail survives; the HTTP layer maps it to 424 Failed
// Dependency so the console can link straight to the credentials section
// instead of surfacing a generic failure.
var ErrStoreCredentialUnavailable = errors.New("storeops: store credential unavailable for this action")

// SetAuditor wires the ops_audit_log sink for store release actions. Late-set,
// matching the SetTaskCreator/SetStoreOnboarder idiom in
// application/deploy/service.go:53-73 — without it (the pre-wiring default)
// release actions still run, they just don't leave an audit trail.
func (s *Service) SetAuditor(store port.OpsAuditStore) { s.audit = store }

// recordAudit writes one ops_audit_log row for a single release action
// attempt, success or failure. A write failure here is logged and never
// masks the caller's original error or result — failing an otherwise
// successful action just because the audit write didn't land would be worse
// than a gap in the log. A nil auditor (never wired) makes this a no-op.
func (s *Service) recordAudit(ctx context.Context, repositoryID uuid.UUID, action, target, actor string, detail map[string]string, actionErr error) {
	if s.audit == nil {
		return
	}
	entry := domain.OpsAuditEntry{
		RepositoryID: &repositoryID,
		Action:       action,
		Target:       target,
		Actor:        actor,
		Detail:       detail,
		Outcome:      domain.OpsOutcomeOK,
	}
	if actionErr != nil {
		entry.Outcome = domain.OpsOutcomeError
		entry.Error = actionErr.Error()
	}
	if err := s.audit.Log(ctx, entry); err != nil {
		log.Error().Err(err).Str("action", action).Str("target", target).
			Msg("storeops: writing ops audit entry failed")
	}
}

// checkConfirm enforces the production-class confirmation guardrail: confirm
// must equal repoName exactly (whitespace-trimmed), the same brake
// deployops.Dispatch/Rollback use for their own production-class actions.
func checkConfirm(confirm, repoName string) error {
	if strings.TrimSpace(confirm) != repoName {
		return ErrConfirmMismatch
	}
	return nil
}

// loadRepoAndApp loads the repository (its Name anchors the confirm phrase)
// and the platform's registry row. Either's port.ErrNotFound propagates
// wrapped (still matched by errors.Is) so the handler 404s instead of
// auditing an attempt against a repository or app that doesn't exist —
// same ruling deployops.Dispatch documents for its own repository load.
func (s *Service) loadRepoAndApp(ctx context.Context, repositoryID uuid.UUID, platform string) (domain.Repository, domain.MobileStoreApp, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.Repository{}, domain.MobileStoreApp{}, fmt.Errorf("storeops: loading repository: %w", err)
	}
	app, err := s.apps.Get(ctx, repositoryID, platform)
	if err != nil {
		return domain.Repository{}, domain.MobileStoreApp{}, fmt.Errorf("storeops: loading store app: %w", err)
	}
	return repo, app, nil
}

// ascClient builds an App Store Connect client for a release action,
// wrapping any failure (credential never saved, cipher not configured,
// decrypt failure) as ErrStoreCredentialUnavailable so the HTTP layer can
// answer 424 regardless of the precise underlying cause.
func (s *Service) ascClient(ctx context.Context) (port.AppStoreClient, error) {
	client, err := s.asc(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreCredentialUnavailable, err)
	}
	return client, nil
}

// playClient is ascClient's Google Play counterpart.
func (s *Service) playClient(ctx context.Context) (port.GooglePlayClient, error) {
	client, err := s.play(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreCredentialUnavailable, err)
	}
	return client, nil
}

// SubmitIOS submits the repository's live iOS app for App Store review.
// Production-class: it puts the app in front of Apple's reviewers, so
// confirm must equal the repository's name exactly. Only a live app can be
// submitted — a half-onboarded one has no confirmed store app id to submit
// a version against.
func (s *Service) SubmitIOS(ctx context.Context, repositoryID uuid.UUID, confirm, actor string) error {
	const platform = domain.MobileStorePlatformIOS
	repo, app, err := s.loadRepoAndApp(ctx, repositoryID, platform)
	if err != nil {
		return err
	}
	if app.State != domain.MobileStoreStateLive {
		notReady := fmt.Errorf("storeops: iOS app must be live before it can be submitted for review: %w", ErrAppNotReady)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreSubmit, platform, actor, nil, notReady)
		return notReady
	}
	if err := checkConfirm(confirm, repo.Name); err != nil {
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreSubmit, platform, actor, nil, err)
		return err
	}
	client, err := s.ascClient(ctx)
	if err != nil {
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreSubmit, platform, actor, nil, err)
		return err
	}
	version, err := client.LatestVersion(ctx, app.StoreAppID)
	if err != nil {
		wrapped := fmt.Errorf("storeops: loading latest App Store version: %w", err)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreSubmit, platform, actor, nil, wrapped)
		return wrapped
	}
	detail := map[string]string{"version": version.Version}
	if err := client.SubmitForReview(ctx, app.StoreAppID, version.Version); err != nil {
		wrapped := fmt.Errorf("storeops: submitting %s for review: %w", app.Identifier, err)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreSubmit, platform, actor, detail, wrapped)
		return wrapped
	}
	s.recordAudit(ctx, repositoryID, domain.OpsActionStoreSubmit, platform, actor, detail, nil)
	return nil
}

// ReleaseIOS releases the iOS version Apple currently holds in
// PENDING_DEVELOPER_RELEASE. Production-class, same confirm guardrail as
// SubmitIOS.
func (s *Service) ReleaseIOS(ctx context.Context, repositoryID uuid.UUID, confirm, actor string) error {
	const platform = domain.MobileStorePlatformIOS
	repo, app, err := s.loadRepoAndApp(ctx, repositoryID, platform)
	if err != nil {
		return err
	}
	if app.State != domain.MobileStoreStateLive {
		notReady := fmt.Errorf("storeops: iOS app must be live before a pending version can be released: %w", ErrAppNotReady)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreRelease, platform, actor, nil, notReady)
		return notReady
	}
	if err := checkConfirm(confirm, repo.Name); err != nil {
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreRelease, platform, actor, nil, err)
		return err
	}
	client, err := s.ascClient(ctx)
	if err != nil {
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreRelease, platform, actor, nil, err)
		return err
	}
	if err := client.ReleaseVersion(ctx, app.StoreAppID); err != nil {
		wrapped := fmt.Errorf("storeops: releasing %s: %w", app.Identifier, err)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreRelease, platform, actor, nil, wrapped)
		return wrapped
	}
	s.recordAudit(ctx, repositoryID, domain.OpsActionStoreRelease, platform, actor, nil, nil)
	return nil
}

// PromoteAndroid copies the internal track's current release onto toTrack
// at userFraction. Promoting to production is production-class (confirm
// must equal the repository name); promoting to the internal test track is
// not, since nothing reaches real users. A live app permits either; an app
// only at test_ready (its first release has not shipped yet) permits only
// the internal-track promote — production for a first release starts from
// there, but a live app past its first release is not held to that
// restriction.
func (s *Service) PromoteAndroid(ctx context.Context, repositoryID uuid.UUID, toTrack string, userFraction float64, confirm, actor string) error {
	const platform = domain.MobileStorePlatformAndroid
	if !validPlayTrack(toTrack) {
		// Checked before anything else because an unrecognised track slips
		// every gate below it: it is not `production`, so no confirm phrase is
		// demanded, and it then reaches PromoteTrack as a track name Play has
		// never heard of — an empty one included.
		invalid := fmt.Errorf("storeops: %q is not a Google Play track: %w", toTrack, ErrInvalidChannel)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, map[string]string{"to_track": toTrack}, invalid)
		return invalid
	}
	repo, app, err := s.loadRepoAndApp(ctx, repositoryID, platform)
	if err != nil {
		return err
	}
	ready := app.State == domain.MobileStoreStateLive ||
		(toTrack == androidTrackInternal && app.State == domain.MobileStoreStateTestReady)
	if !ready {
		notReady := fmt.Errorf("storeops: android app is not ready to promote to %s: %w", toTrack, ErrAppNotReady)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, map[string]string{"to_track": toTrack}, notReady)
		return notReady
	}
	if toTrack == androidTrackProduction {
		if err := checkConfirm(confirm, repo.Name); err != nil {
			s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, map[string]string{"to_track": toTrack}, err)
			return err
		}
	}
	client, err := s.playClient(ctx)
	if err != nil {
		s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, map[string]string{"to_track": toTrack}, err)
		return err
	}
	detail := map[string]string{"from_track": androidTrackInternal, "to_track": toTrack}
	if err := client.PromoteTrack(ctx, app.Identifier, androidTrackInternal, toTrack, userFraction); err != nil {
		wrapped := fmt.Errorf("storeops: promoting %s to %s: %w", app.Identifier, toTrack, err)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, detail, wrapped)
		return wrapped
	}
	s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, detail, nil)
	return nil
}

// SetAndroidRollout dials the production track's in-flight rollout to
// userFraction. Not production-class — it never exceeds what is already
// live — but the fraction is user input reaching a production API, so it is
// range-checked here rather than trusting the browser to have done it.
func (s *Service) SetAndroidRollout(ctx context.Context, repositoryID uuid.UUID, userFraction float64, actor string) error {
	if userFraction < 0 || userFraction > 1 {
		return fmt.Errorf("storeops: rollout fraction %v is outside [0,1]: %w", userFraction, ErrInvalidRolloutFraction)
	}
	const platform = domain.MobileStorePlatformAndroid
	_, app, err := s.loadRepoAndApp(ctx, repositoryID, platform)
	if err != nil {
		return err
	}
	if app.State != domain.MobileStoreStateLive {
		notReady := fmt.Errorf("storeops: android app must be live to change its rollout: %w", ErrAppNotReady)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreRollout, platform, actor, nil, notReady)
		return notReady
	}
	client, err := s.playClient(ctx)
	if err != nil {
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreRollout, platform, actor, nil, err)
		return err
	}
	detail := map[string]string{"user_fraction": fmt.Sprintf("%v", userFraction)}
	if err := client.SetRolloutFraction(ctx, app.Identifier, androidTrackProduction, userFraction); err != nil {
		wrapped := fmt.Errorf("storeops: setting %s rollout fraction: %w", app.Identifier, err)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreRollout, platform, actor, detail, wrapped)
		return wrapped
	}
	s.recordAudit(ctx, repositoryID, domain.OpsActionStoreRollout, platform, actor, detail, nil)
	return nil
}

// HaltAndroid stops the production track's rollout in place. Production-class:
// it takes the current release out of new users' hands, so confirm must
// equal the repository name.
func (s *Service) HaltAndroid(ctx context.Context, repositoryID uuid.UUID, confirm, actor string) error {
	const platform = domain.MobileStorePlatformAndroid
	repo, app, err := s.loadRepoAndApp(ctx, repositoryID, platform)
	if err != nil {
		return err
	}
	if app.State != domain.MobileStoreStateLive {
		notReady := fmt.Errorf("storeops: android app must be live to halt its rollout: %w", ErrAppNotReady)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreHalt, platform, actor, nil, notReady)
		return notReady
	}
	if err := checkConfirm(confirm, repo.Name); err != nil {
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreHalt, platform, actor, nil, err)
		return err
	}
	client, err := s.playClient(ctx)
	if err != nil {
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreHalt, platform, actor, nil, err)
		return err
	}
	if err := client.HaltRollout(ctx, app.Identifier, androidTrackProduction); err != nil {
		wrapped := fmt.Errorf("storeops: halting %s rollout: %w", app.Identifier, err)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreHalt, platform, actor, nil, wrapped)
		return wrapped
	}
	s.recordAudit(ctx, repositoryID, domain.OpsActionStoreHalt, platform, actor, nil, nil)
	return nil
}

// ResumeAndroid un-halts the production track's rollout at the fraction it
// was halted at. Not production-class: it restores what was already live,
// it does not change what users receive.
func (s *Service) ResumeAndroid(ctx context.Context, repositoryID uuid.UUID, actor string) error {
	const platform = domain.MobileStorePlatformAndroid
	_, app, err := s.loadRepoAndApp(ctx, repositoryID, platform)
	if err != nil {
		return err
	}
	if app.State != domain.MobileStoreStateLive {
		notReady := fmt.Errorf("storeops: android app must be live to resume its rollout: %w", ErrAppNotReady)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreResume, platform, actor, nil, notReady)
		return notReady
	}
	client, err := s.playClient(ctx)
	if err != nil {
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreResume, platform, actor, nil, err)
		return err
	}
	if err := client.ResumeRollout(ctx, app.Identifier, androidTrackProduction); err != nil {
		wrapped := fmt.Errorf("storeops: resuming %s rollout: %w", app.Identifier, err)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreResume, platform, actor, nil, wrapped)
		return wrapped
	}
	s.recordAudit(ctx, repositoryID, domain.OpsActionStoreResume, platform, actor, nil, nil)
	return nil
}

// AllApps returns every registered mobile store app across every
// repository with its repository name joined in — the data behind the
// operations console's cross-repository apps screen. Exactly one
// apps.ListAll call and one repos.List call back this, regardless of how
// many app rows or repositories exist: a per-row repository lookup would
// turn a single page load into an N+1 query storm.
func (s *Service) AllApps(ctx context.Context) ([]AppView, error) {
	apps, err := s.apps.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("storeops: listing all store apps: %w", err)
	}
	repos, err := s.repos.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("storeops: listing repositories: %w", err)
	}
	names := make(map[uuid.UUID]string, len(repos))
	for _, r := range repos {
		names[r.ID] = r.Name
	}
	views := make([]AppView, 0, len(apps))
	for _, app := range apps {
		views = append(views, AppView{MobileStoreApp: app, RepositoryName: names[app.RepositoryID]})
	}
	return views, nil
}

// AppView is one mobile store app row plus the repository name it belongs
// to, embedded rather than referenced so the JSON response reads flat
// (identifier, state, etc. alongside repository_name) instead of nesting a
// whole repository object the console apps screen doesn't need.
type AppView struct {
	domain.MobileStoreApp
	RepositoryName string `json:"repository_name"`
}

// Play's two testing tracks. `external` is this system's fold of them and is
// not a Play track name, which is why it has no constant here — see
// port.GooglePlayClient.Tracks.
const (
	androidTrackAlpha = "alpha"
	androidTrackBeta  = "beta"
)

// validPlayTrack reports whether a caller-supplied Play track is one this
// system will address. The four here are Play's own default tracks; a custom
// closed-testing track is deliberately not accepted, because nothing in the
// product's channel vocabulary can name one.
func validPlayTrack(track string) bool {
	switch track {
	case androidTrackInternal, androidTrackAlpha, androidTrackBeta, androidTrackProduction:
		return true
	}
	return false
}

// ErrInvalidChannel marks a promotion whose (from, to) pair is not the one
// legal step domain.NextChannel allows. Skipping the middle channel is the
// case it exists for: on both stores that would bypass the review or testing
// stage the channel in between is there to hold.
var ErrInvalidChannel = errors.New("storeops: promotion must move one channel forward")

// PromoteChannel moves the build sitting on `from` onto `to` in the product's
// own channel vocabulary (internal → external → production), translating to
// each store's spelling in the adapter call rather than in the caller.
//
// It goes through the same gates the platform-specific actions do and skips
// none of them: production is production-class (confirm must equal the
// repository's name), and only a live app may reach production, while an app
// that is merely test_ready may still be promoted between the test channels.
// Every attempt — refused or not — writes one ops_audit_log row.
func (s *Service) PromoteChannel(ctx context.Context, repositoryID uuid.UUID, platform, from, to, confirm, actor string) error {
	// These three refusals are audited too, before the repository is loaded.
	// A skipped channel — internal straight to production — is precisely the
	// attempt an audit log exists to remember, and returning early without a
	// row made it the one attempt that left no trace.
	pair := map[string]string{"from": from, "to": to}
	if !validPlatform(platform) {
		err := fmt.Errorf("storeops: promoting: unsupported platform %q: %w", platform, ErrInvalidPlatform)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, pair, err)
		return err
	}
	if !domain.ValidStoreChannel(from) || !domain.ValidStoreChannel(to) {
		err := fmt.Errorf("storeops: promoting %q -> %q: unknown channel: %w", from, to, ErrInvalidChannel)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, pair, err)
		return err
	}
	if next, ok := domain.NextChannel(from); !ok || next != to {
		err := fmt.Errorf("storeops: %q does not promote to %q: %w", from, to, ErrInvalidChannel)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, pair, err)
		return err
	}

	repo, app, err := s.loadRepoAndApp(ctx, repositoryID, platform)
	if err != nil {
		return err
	}
	detail := map[string]string{"from": from, "to": to}
	toProduction := to == domain.StoreChannelProduction

	ready := app.State == domain.MobileStoreStateLive ||
		(!toProduction && app.State == domain.MobileStoreStateTestReady)
	if !ready {
		notReady := fmt.Errorf("storeops: %s app is not ready to promote to %s: %w", platform, to, ErrAppNotReady)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, detail, notReady)
		return notReady
	}
	if toProduction {
		if err := checkConfirm(confirm, repo.Name); err != nil {
			s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, detail, err)
			return err
		}
	}

	switch platform {
	case domain.MobileStorePlatformIOS:
		client, err := s.ascClient(ctx)
		if err != nil {
			s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, detail, err)
			return err
		}
		if err := client.PromoteChannel(ctx, app.StoreAppID, from, to); err != nil {
			wrapped := fmt.Errorf("storeops: promoting %s from %s to %s: %w", app.Identifier, from, to, err)
			s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, detail, wrapped)
			return wrapped
		}
	case domain.MobileStorePlatformAndroid:
		client, err := s.playClient(ctx)
		if err != nil {
			s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, detail, err)
			return err
		}
		fromTrack := playTrack(from, app.Tracks)
		toTrack := playTrack(to, app.Tracks)
		detail["from_track"] = fromTrack
		detail["to_track"] = toTrack
		// Promoted at full rollout, not staged: the product's channel
		// vocabulary carries no fraction, and Play's staged rollout is dialled
		// afterwards by SetAndroidRollout (or stopped by HaltAndroid), which is
		// the pair of controls the console already exposes for it.
		if err := client.PromoteTrack(ctx, app.Identifier, fromTrack, toTrack, 1); err != nil {
			wrapped := fmt.Errorf("storeops: promoting %s from %s to %s: %w", app.Identifier, fromTrack, toTrack, err)
			s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, detail, wrapped)
			return wrapped
		}
	}
	s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, detail, nil)
	return nil
}

// playTrack translates a product channel into the Play track name to address.
//
// internal and production map one-to-one. `external` does not: Play splits it
// into `alpha` (closed testing) and `beta` (open testing), and which one an
// app actually uses is only knowable from the console. That is what the cached
// Tracks read answers — the adapter records the track it reported in the
// external channel's Audience (port.GooglePlayClient.Tracks), the one place
// the distinction survives, because domain.TrackRelease deliberately carries
// no store-specific track name. With nothing cached yet, open testing is the
// assumption: it is the stage a build leaving internal is heading for, and the
// closed-testing tenants are the ones whose Tracks read will say so.
func playTrack(channel string, tracks domain.StoreTracks) string {
	switch channel {
	case domain.StoreChannelInternal:
		return androidTrackInternal
	case domain.StoreChannelProduction:
		return androidTrackProduction
	}
	if strings.Contains(strings.ToLower(tracks.External.Audience), "closed") {
		return androidTrackAlpha
	}
	return androidTrackBeta
}
