package storeops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Checklist item keys. Fixed contract: Task 9's Monitor keys off these
// exact strings, so they never get renamed without updating that consumer.
const (
	checklistIOSAppRecord    = "ios_app_record"
	checklistPlayAppRecord   = "play_app_record"
	checklistPlayFirstUpload = "play_first_upload"
)

// playFirstUploadTitle is the manual step title for play_first_upload — it
// does not depend on the package identifier, unlike the other checklist
// items.
const playFirstUploadTitle = "Download the signed AAB artifact from the mobile-stage-google-play workflow run (dispatch it with upload=false) and upload it to the Internal testing track"

// checklistTaskDoneNote is appended to every onboarding task's description:
// the assignee never has to tick a box, verification is automatic.
const checklistTaskDoneNote = "the system verifies each step automatically — no need to tick anything"

// Onboard is called when a store deploy target is saved. It runs every
// automatable first-publish step, opens the guided checklist task for the
// rest, and returns the (possibly already test_ready) app row.
func (s *Service) Onboard(ctx context.Context, repositoryID uuid.UUID, provider, identifier, appName string) (domain.MobileStoreApp, error) {
	platform := domain.StoreProviderPlatform(provider)
	if platform == "" {
		return domain.MobileStoreApp{}, fmt.Errorf("storeops: onboarding: unsupported store provider %q", provider)
	}

	app, err := s.apps.Get(ctx, repositoryID, platform)
	switch {
	case err == nil:
		// Existing row: refresh it below rather than starting over.
	case errors.Is(err, port.ErrNotFound):
		app = domain.MobileStoreApp{
			RepositoryID: repositoryID,
			Platform:     platform,
			State:        domain.MobileStoreStateUnregistered,
		}
	default:
		return domain.MobileStoreApp{}, fmt.Errorf("storeops: loading store app: %w", err)
	}

	if err := checkIdentifierChange(app, platform, identifier); err != nil {
		return domain.MobileStoreApp{}, err
	}
	app.Identifier = identifier

	var verified map[string]bool
	switch platform {
	case domain.MobileStorePlatformIOS:
		if err := s.provisionIOS(ctx, repositoryID, identifier, appName); err != nil {
			return domain.MobileStoreApp{}, err
		}
		v, storeAppID, err := s.verifyIOS(ctx, identifier)
		if err != nil {
			return domain.MobileStoreApp{}, err
		}
		verified = v
		if storeAppID != "" {
			app.StoreAppID = storeAppID
		}
	case domain.MobileStorePlatformAndroid:
		if err := s.provisionAndroid(ctx, repositoryID, identifier); err != nil {
			return domain.MobileStoreApp{}, err
		}
		v, err := s.verifyAndroid(ctx, identifier)
		if err != nil {
			return domain.MobileStoreApp{}, err
		}
		verified = v
	}

	// A checklist not yet established (brand new row, or a row that never
	// needed one) is built fresh, dropping whatever the store already
	// confirms — those never become a manual step. An existing checklist is
	// only ever refreshed in place: items already offered to a human stay
	// offered until verified, never silently added to or removed.
	if len(app.Checklist) == 0 && app.OnboardingTaskID == nil {
		app.Checklist = buildChecklist(platform, identifier, verified)
	} else {
		applyVerification(app.Checklist, verified, time.Now())
	}

	if app.State == domain.MobileStoreStateUnregistered && app.CanTransition(domain.MobileStoreStateOnboarding) {
		app.State = domain.MobileStoreStateOnboarding
	}

	if app.ChecklistDone() {
		if app.CanTransition(domain.MobileStoreStateTestReady) {
			app.State = domain.MobileStoreStateTestReady
		}
	} else {
		// The checklist is open again — typically because the target was
		// re-saved against a different identifier, so the freshly built list
		// is unverified. A row left sitting at test_ready here would let
		// mobileStoreGate green-light stage deploys against an app record
		// nobody has confirmed exists. Take the lifecycle's one backward edge
		// (test_ready -> onboarding) instead; it exists for exactly this.
		if app.CanTransition(domain.MobileStoreStateOnboarding) {
			app.State = domain.MobileStoreStateOnboarding
		}
		if app.OnboardingTaskID == nil {
			task, err := s.tasks.CreateTask(ctx, repositoryID, domain.CreateBoardTaskRequest{
				Title:       fmt.Sprintf("Store onboarding: %s (%s)", identifier, platform),
				Description: checklistTaskDescription(app.Checklist),
				Priority:    domain.TaskPriorityHigh,
				Column:      domain.TaskColumnTodo,
				CreatedBy:   "system",
			})
			if err != nil {
				return domain.MobileStoreApp{}, fmt.Errorf("storeops: creating onboarding task: %w", err)
			}
			app.OnboardingTaskID = &task.ID
		}
	}

	stored, err := s.apps.Upsert(ctx, app)
	if err != nil {
		return domain.MobileStoreApp{}, fmt.Errorf("storeops: persisting store app: %w", err)
	}
	return stored, nil
}

// EnsureIdentifierAllowed vets an identifier BEFORE a store deploy target
// carrying it is persisted. It is the pre-save half of the rule Onboard
// enforces on the registry row, and it exists because deploy.SaveTarget
// saves the target first and treats onboarding as a best-effort follow-up:
// a refusal discovered only inside Onboard would be logged and swallowed,
// leaving the deploy target pointing at the new identifier while the store
// row stayed `live` under the old one — a divergence worse than the
// overwrite it was meant to prevent.
//
// A repository with no registry row for the provider's platform has nothing
// to protect and is allowed.
func (s *Service) EnsureIdentifierAllowed(ctx context.Context, repositoryID uuid.UUID, provider, identifier string) error {
	platform := domain.StoreProviderPlatform(provider)
	if platform == "" {
		return fmt.Errorf("storeops: unsupported store provider %q: %w", provider, ErrInvalidPlatform)
	}
	app, err := s.apps.Get(ctx, repositoryID, platform)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return nil
		}
		// Fail closed: this is a gate, and "the lookup broke" is not
		// "nothing to protect".
		return fmt.Errorf("storeops: loading store app: %w", err)
	}
	return checkIdentifierChange(app, platform, identifier)
}

// checkIdentifierChange refuses to re-point a published app at a different
// bundle ID / package name. That is not an edit, it is a different app: the
// lifecycle has no backward edge out of live, so overwriting the identifier
// would leave the row claiming `live` — and mobileStoreGate green-lighting
// production releases — for something that was never registered, let alone
// published. The operator has to delete the target instead.
func checkIdentifierChange(app domain.MobileStoreApp, platform, identifier string) error {
	if app.State != domain.MobileStoreStateLive || app.Identifier == "" || app.Identifier == identifier {
		return nil
	}
	return fmt.Errorf(
		"this repository is already live on %s as %q; delete the deploy target before pointing it at %q: %w",
		platform, app.Identifier, identifier, ErrIdentifierLocked)
}

// VerifyOnboarding re-checks every open checklist item against the store
// API, marks verified ones done (with a system comment on the checklist
// task), and advances state to test_ready when the list is complete.
func (s *Service) VerifyOnboarding(ctx context.Context, repositoryID uuid.UUID, platform string) (domain.MobileStoreApp, error) {
	// Validate before the load. The platform is caller-supplied (it comes
	// straight off a URL segment), and s.apps.Get would otherwise fail first
	// with an unhelpful "not found" for a typo — the caller's own mistake
	// reported as if the repository had no store app.
	if !validPlatform(platform) {
		return domain.MobileStoreApp{}, fmt.Errorf("storeops: verifying onboarding: unsupported platform %q: %w", platform, ErrInvalidPlatform)
	}

	app, err := s.apps.Get(ctx, repositoryID, platform)
	if err != nil {
		return domain.MobileStoreApp{}, fmt.Errorf("storeops: loading store app: %w", err)
	}

	var verified map[string]bool
	switch platform {
	case domain.MobileStorePlatformIOS:
		v, storeAppID, err := s.verifyIOS(ctx, app.Identifier)
		if err != nil {
			return domain.MobileStoreApp{}, err
		}
		verified = v
		if storeAppID != "" {
			app.StoreAppID = storeAppID
		}
	case domain.MobileStorePlatformAndroid:
		v, err := s.verifyAndroid(ctx, app.Identifier)
		if err != nil {
			return domain.MobileStoreApp{}, err
		}
		verified = v
	}

	newlyDone := applyVerification(app.Checklist, verified, time.Now())

	if app.ChecklistDone() && app.CanTransition(domain.MobileStoreStateTestReady) {
		app.State = domain.MobileStoreStateTestReady
	}

	stored, err := s.apps.Upsert(ctx, app)
	if err != nil {
		return domain.MobileStoreApp{}, fmt.Errorf("storeops: persisting store app: %w", err)
	}

	var commentErrs []error
	if app.OnboardingTaskID != nil && s.comments != nil {
		for _, item := range newlyDone {
			if _, err := s.comments.AddComment(ctx, repositoryID, *app.OnboardingTaskID, domain.CreateTaskCommentRequest{
				Content:    fmt.Sprintf("Verified: %s", item.Title),
				AuthorType: "system",
			}); err != nil {
				commentErrs = append(commentErrs, fmt.Errorf("storeops: posting verification comment for %s: %w", item.Key, err))
			}
		}
	}

	return stored, errors.Join(commentErrs...)
}

// provisionIOS runs every automatable iOS first-publish step: registering
// the bundle ID with Apple, minting or renewing the distribution signing
// assets, and pushing every resulting secret to the repository's GitHub
// Actions vault.
func (s *Service) provisionIOS(ctx context.Context, repositoryID uuid.UUID, identifier, appName string) error {
	client, err := s.asc(ctx)
	if err != nil {
		return err
	}
	name, err := s.resolveAppName(ctx, repositoryID, appName)
	if err != nil {
		return err
	}
	if err := client.EnsureBundleID(ctx, identifier, name); err != nil {
		return fmt.Errorf("storeops: registering bundle id %s: %w", identifier, err)
	}
	values, err := s.EnsureIOSSigning(ctx, identifier)
	if err != nil {
		return err
	}
	return s.pushSecrets(ctx, repositoryID, values)
}

// provisionAndroid runs every automatable Android first-publish step:
// confirming the Google Play credential still works, minting or renewing
// the upload keystore, and pushing every resulting secret.
func (s *Service) provisionAndroid(ctx context.Context, repositoryID uuid.UUID, identifier string) error {
	client, err := s.play(ctx)
	if err != nil {
		return err
	}
	if err := client.ValidateAuth(ctx); err != nil {
		return fmt.Errorf("storeops: validating google play credential: %w", err)
	}
	values, err := s.EnsureAndroidKeystore(ctx, identifier)
	if err != nil {
		return err
	}
	return s.pushSecrets(ctx, repositoryID, values)
}

// resolveAppName is the app name EnsureBundleID registers under: the
// caller-supplied appName, or the repository's own name when none was
// given.
func (s *Service) resolveAppName(ctx context.Context, repositoryID uuid.UUID, appName string) (string, error) {
	if appName != "" {
		return appName, nil
	}
	if s.repos == nil {
		return "", errors.New("storeops: repository resolver not configured")
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return "", fmt.Errorf("storeops: resolving repository name: %w", err)
	}
	return repo.Name, nil
}

// verifyIOS checks the iOS checklist items against App Store Connect. It
// returns which keys are currently satisfied and the store's app ID when
// the app record exists (empty otherwise).
func (s *Service) verifyIOS(ctx context.Context, identifier string) (map[string]bool, string, error) {
	client, err := s.asc(ctx)
	if err != nil {
		return nil, "", err
	}
	appID, found, err := client.AppByBundleID(ctx, identifier)
	if err != nil {
		return nil, "", fmt.Errorf("storeops: checking app store connect record: %w", err)
	}
	return map[string]bool{checklistIOSAppRecord: found}, appID, nil
}

// verifyAndroid checks the Android checklist items against Google Play: the
// app record and whether the internal testing track already has a release.
// The track can only be queried once the app record itself exists — the
// real Play Developer API errors with "app not found" for
// TrackInfo(unregistered package) (see googleplay.Client.TrackInfo), so
// that call is skipped entirely until AppExists reports true, and
// play_first_upload is simply reported not-yet-verified until then.
func (s *Service) verifyAndroid(ctx context.Context, identifier string) (map[string]bool, error) {
	client, err := s.play(ctx)
	if err != nil {
		return nil, err
	}
	exists, err := client.AppExists(ctx, identifier)
	if err != nil {
		return nil, fmt.Errorf("storeops: checking play console app record: %w", err)
	}
	verified := map[string]bool{
		checklistPlayAppRecord:   exists,
		checklistPlayFirstUpload: false,
	}
	if !exists {
		return verified, nil
	}
	track, err := client.TrackInfo(ctx, identifier, "internal")
	if err != nil {
		return nil, fmt.Errorf("storeops: checking play internal track: %w", err)
	}
	verified[checklistPlayFirstUpload] = track.HasRelease
	return verified, nil
}

// buildChecklist is the fresh checklist for a platform: one entry per
// manual step, skipping ("dropping") whatever the store already confirms —
// an already-satisfied step never becomes a task item in the first place.
func buildChecklist(platform, identifier string, verified map[string]bool) []domain.ChecklistItem {
	var candidates []domain.ChecklistItem
	switch platform {
	case domain.MobileStorePlatformIOS:
		candidates = []domain.ChecklistItem{
			{Key: checklistIOSAppRecord, Title: fmt.Sprintf("App Store Connect → My Apps → New App → bundle ID %s", identifier)},
		}
	case domain.MobileStorePlatformAndroid:
		candidates = []domain.ChecklistItem{
			{Key: checklistPlayAppRecord, Title: fmt.Sprintf("Play Console → Create app (package %s)", identifier)},
			{Key: checklistPlayFirstUpload, Title: playFirstUploadTitle},
		}
	}
	var items []domain.ChecklistItem
	for _, c := range candidates {
		if verified[c.Key] {
			continue
		}
		items = append(items, c)
	}
	return items
}

// applyVerification marks any open checklist item the store now confirms as
// done, stamping VerifiedAt. It mutates checklist in place and returns the
// items that flipped from not-done to done on this call — the ones that
// deserve a fresh comment, never a re-announcement of old news.
func applyVerification(checklist []domain.ChecklistItem, verified map[string]bool, now time.Time) []domain.ChecklistItem {
	var newlyDone []domain.ChecklistItem
	for i := range checklist {
		if checklist[i].Done || !verified[checklist[i].Key] {
			continue
		}
		verifiedAt := now
		checklist[i].Done = true
		checklist[i].VerifiedAt = &verifiedAt
		newlyDone = append(newlyDone, checklist[i])
	}
	return newlyDone
}

// checklistTaskDescription renders the onboarding task body: the numbered
// manual steps plus the standing note that the system, not the assignee,
// closes each item out.
func checklistTaskDescription(items []domain.ChecklistItem) string {
	var b strings.Builder
	for i, item := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item.Title)
	}
	b.WriteString("\n")
	b.WriteString(checklistTaskDoneNote)
	return b.String()
}
