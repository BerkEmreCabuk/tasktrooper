package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/github"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

// defaultPushReindexInterval is the per-repo floor between webhook-triggered
// reindex passes. Every pass embeds every changed chunk, so push spam without
// this floor converts directly into an embedding bill.
const defaultPushReindexInterval = 2 * time.Minute

// pushDeliveryTTL is how long a GitHub delivery id is remembered. GitHub
// retries failed deliveries within minutes; anything older is a new event.
const pushDeliveryTTL = 15 * time.Minute

// pushRepoState is the per-repo debounce ledger. At most one webhook-triggered
// pass runs at a time; triggers landing during a pass collapse into a single
// "pending" rerun, and triggers landing inside the interval collapse into a
// single scheduled timer.
type pushRepoState struct {
	running   bool
	pending   bool
	lastStart time.Time
	timer     *time.Timer
	// actor is the identity the reindex this state schedules must run as.
	//
	// Stored for the same reason board.RunJob carries one: the delivery that
	// proved which tenant this push belongs to is long gone by the time the
	// debounce timer fires, and everything the pass then does — reading the
	// repository row, restoring the mirror, writing index status — is scoped by
	// the identity on its context. Without it the pass runs on a bare
	// context.Background() and every store call answers tenant.ErrNoTenant, so
	// a push-triggered reindex silently does nothing at all.
	//
	// The identity, not a context: a stored context would carry a dead deadline
	// and a cancellation belonging to a request that has already been answered.
	// Last delivery wins, which is safe because a repository row belongs to
	// exactly one tenant, so consecutive pushes to it are all the same one.
	actor tenant.Identity
}

// pushContext rebuilds the context a debounced reindex runs on: no deadline (a
// pass over a large repository is long) and the tenant the delivery arrived
// for.
func (st *pushRepoState) pushContext() context.Context {
	ctx := context.Background()
	if st != nil && st.actor.TenantID != uuid.Nil {
		ctx = tenant.With(ctx, st.actor)
	}
	return ctx
}

// SetPublicBaseURL wires the externally reachable base URL GitHub webhooks
// must target (e.g. "https://bridge.example.com"). Empty leaves webhook setup
// disabled with an explanatory error.
func (s *Service) SetPublicBaseURL(u string) {
	s.publicBaseURL = strings.TrimSuffix(strings.TrimSpace(u), "/")
}

// webhookTargetURL is the delivery URL registered with GitHub.
//
// The ?t=<tenant> query parameter is how the gateway knows which tenant a
// delivery belongs to: GitHub signs the body with the per-repository secret and
// carries no identity of ours, and /v1/github/webhook is a public path, so the
// URL registered at import time is the only place the tenant can be recorded.
//
// It is read off the REQUEST that registers the hook rather than from a
// pod-wide value. It used to come from TENANT_UID, which was correct while a
// pod served one tenant; one shared Deployment has no such value, and the
// tenant importing the repository is right there on the context.
func (s *Service) webhookTargetURL(ctx context.Context) string {
	target := s.publicBaseURL + "/v1/github/webhook"
	if id, ok := tenant.ID(ctx); ok {
		target += "?t=" + url.QueryEscape(id.String())
	}
	return target
}

func (s *Service) pushInterval() time.Duration {
	if s.pushReindexInterval > 0 {
		return s.pushReindexInterval
	}
	return defaultPushReindexInterval
}

// SetupWebhook generates a fresh secret, installs (or refreshes) the repository
// webhook on GitHub, and stores the secret encrypted. Safe to call again: the
// GitHub side is updated in place and the secret is rotated atomically with it.
//
// The hook covers githubapi.WebhookEvents — push AND the Actions events the
// code-review gate needs. It used to be push only, which is why a green CI run
// had no way of reaching the board at all.
func (s *Service) SetupWebhook(ctx context.Context, repositoryID uuid.UUID) (domain.Repository, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.Repository{}, err
	}
	repo = s.syncRemoteURL(ctx, repo, "")
	owner, name, ok := githubapi.ParseOwnerRepo(repo.RemoteURL)
	if !ok {
		return domain.Repository{}, fmt.Errorf("repository has no GitHub remote to attach a webhook to")
	}
	if s.githubToken == nil {
		return domain.Repository{}, fmt.Errorf("GitHub is not connected — connect it from Settings")
	}
	token, err := s.githubToken(ctx)
	if err != nil {
		return domain.Repository{}, err
	}
	if token == "" {
		return domain.Repository{}, fmt.Errorf("GitHub is not connected — connect it from Settings")
	}
	if s.publicBaseURL == "" {
		return domain.Repository{}, fmt.Errorf("public base URL is not configured (server.public_base_url), GitHub cannot reach this instance")
	}
	secret, err := generateWebhookSecret()
	if err != nil {
		return domain.Repository{}, fmt.Errorf("generate webhook secret: %w", err)
	}
	hookID, err := githubapi.EnsureRepoWebhookAt(ctx, s.githubAPIBase, token, owner, name, s.webhookTargetURL(ctx), secret)
	if err != nil {
		return domain.Repository{}, fmt.Errorf("github webhook setup: %w", err)
	}
	if err := s.repos.SetWebhook(ctx, repositoryID, secret, hookID); err != nil {
		// GitHub now signs with a secret we failed to store; the next setup run
		// rotates both sides back into agreement.
		return domain.Repository{}, fmt.Errorf("webhook created on GitHub but its secret could not be stored, run setup again: %w", err)
	}
	return s.repos.Get(ctx, repositoryID)
}

func generateWebhookSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// setupWebhookAsync is the best-effort install at repo registration time. A
// failure only costs the automation: the UI shows a warning with a retry
// button as long as the repo has no webhook.
// It takes the caller's context for its IDENTITY only — see tenant.Detach. The
// install reads the repository row, resolves the tenant's GitHub token and
// stores the webhook secret, all policy-protected, and it stamps the delivery
// URL with ?t=<tenant> (webhookTargetURL). On a bare context.Background() every
// one of those failed, so a live stack logged "github push webhook setup
// failed: tenant: no tenant in context" on every repository import and no
// tenant ever got a push webhook.
func (s *Service) setupWebhookAsync(ctx context.Context, repositoryID uuid.UUID) {
	if s.githubToken == nil || s.publicBaseURL == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(tenant.Detach(ctx), 30*time.Second)
		defer cancel()
		if _, err := s.SetupWebhook(ctx, repositoryID); err != nil {
			log.Warn().Err(err).Str("repository_id", repositoryID.String()).
				Msg("github push webhook setup failed; retry from repository settings")
		}
	}()
}

// ReconcileWebhooksAsync is the boot-time convergence pass over every
// registered repository's GitHub webhook. It does two different jobs, and the
// split matters:
//
//	NO HOOK — install one (SetupWebhook). This is the original backfill, for
//	  repositories created before webhook support existed or whose setup failed.
//	  It mints a fresh secret, because there is no hook to keep one for.
//	HOOK, WRONG EVENTS — repair it in place (ReconcileRepoWebhookEvents),
//	  WITHOUT rotating the secret. Every repository registered before the
//	  Actions events were added is subscribed to `push` and nothing else, which
//	  is exactly why the code-review gate had no signal. Fixing them through
//	  SetupWebhook would work, but it would rotate every secret on every boot
//	  forever — and each rotation has a window where in-flight deliveries are
//	  signed with the old secret and rejected — to fix a list of strings.
//
// Converged repositories cost one GET each and nothing else, so this is safe to
// run on every boot. Failures cost only the automation; the UI's warning with
// its retry button stays until a setup succeeds.
//
// It takes a context and runs ONE tenant, because everything it does is
// per-tenant: the repository list, the GitHub token, and — decisively — the
// ?t=<tenant> the delivery URL carries, which is the only place a webhook
// records who it belongs to (webhookTargetURL). It used to build its own
// context.Background(), so every read raised tenant.ErrNoTenant and the pass
// logged "could not list repositories" once per boot and converged nothing.
// The fan-out over tenants and the goroutine are the caller's, the same way
// they are for every other sweep.
func (s *Service) ReconcileWebhooks(ctx context.Context) {
	if s.githubToken == nil || s.publicBaseURL == "" {
		return
	}
	func() {
		token, err := s.githubToken(ctx)
		if err != nil || token == "" {
			// GitHub not connected: nothing can be installed, nothing to log.
			return
		}
		repos, err := s.repos.List(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("webhook reconcile: could not list repositories")
			return
		}
		target := s.webhookTargetURL(ctx)
		for _, repo := range repos {
			owner, name, ok := githubapi.ParseOwnerRepo(repo.RemoteURL)
			if !ok {
				continue
			}
			if !repo.WebhookInstalled {
				if _, err := s.SetupWebhook(ctx, repo.ID); err != nil {
					log.Warn().Err(err).Str("repository_id", repo.ID.String()).Str("name", repo.Name).
						Msg("webhook backfill failed; retry from repository settings")
					continue
				}
				log.Info().Str("repository_id", repo.ID.String()).Str("name", repo.Name).
					Msg("webhook backfill: repository webhook installed")
				continue
			}
			_, changed, err := githubapi.ReconcileRepoWebhookEvents(ctx, token, owner, name, target)
			if err != nil {
				log.Warn().Err(err).Str("repository_id", repo.ID.String()).Str("name", repo.Name).
					Msg("webhook reconcile: could not converge the hook's event list")
				continue
			}
			if changed {
				log.Info().Str("repository_id", repo.ID.String()).Str("name", repo.Name).
					Strs("events", githubapi.WebhookEvents).
					Msg("webhook reconcile: existing hook widened to the events the board needs")
			}
		}
	}()
}

// ResolvePushTarget maps a webhook payload's repository full_name
// ("owner/repo") onto a registered repository via its stored remote URL, and
// returns the repo together with its decrypted webhook secret. ok=false means
// the push belongs to nobody we know — the caller answers 204 and does nothing.
func (s *Service) ResolvePushTarget(ctx context.Context, fullName string) (domain.Repository, string, bool) {
	fullName = strings.TrimSuffix(strings.TrimSpace(fullName), ".git")
	if fullName == "" {
		return domain.Repository{}, "", false
	}
	repos, err := s.repos.List(ctx)
	if err != nil {
		log.Error().Err(err).Msg("webhook: could not list repositories")
		return domain.Repository{}, "", false
	}
	for _, repo := range repos {
		owner, name, ok := githubapi.ParseOwnerRepo(repo.RemoteURL)
		if !ok {
			continue
		}
		if strings.EqualFold(owner+"/"+name, fullName) {
			secret, err := s.repos.WebhookSecret(ctx, repo.ID)
			if err != nil {
				// Fail closed: no secret means the signature check rejects.
				log.Error().Err(err).Str("repository_id", repo.ID.String()).Msg("webhook: could not read secret")
				return repo, "", true
			}
			return repo, secret, true
		}
	}
	return domain.Repository{}, "", false
}

// HandleGitHubPush is called after the HTTP layer has verified the payload
// signature. It decides whether the push warrants a reindex and, if so, feeds
// it through the per-repo debounce. The returned reason is diagnostic only.
func (s *Service) HandleGitHubPush(ctx context.Context, repositoryID uuid.UUID, deliveryID, ref, defaultBranch, afterSHA string) (bool, string) {
	// Task-branch pushes churn constantly and never feed the index: the index
	// walks the default-branch working tree.
	if defaultBranch == "" || ref != "refs/heads/"+defaultBranch {
		return false, "ref is not the default branch"
	}
	if s.seenDelivery(ctx, deliveryID, time.Now()) {
		return false, "duplicate delivery"
	}
	// A completed index already at the pushed head has nothing to do. A failed
	// or stale pass at the same SHA still reruns.
	if s.indexer != nil && afterSHA != "" {
		if idx, err := s.indexer.GetProjectStatus(ctx, repositoryID); err == nil &&
			idx.Status == domain.IndexStatusCompleted && idx.CommitSHA == afterSHA {
			return false, "index already at pushed commit"
		}
	}
	if s.reindexHasNoMac(ctx) {
		return false, s.deferReindexToAHuman(repositoryID)
	}
	return true, s.schedulePushReindex(ctx, repositoryID)
}

// HandleGitHubWorkflowEvent is called after the HTTP layer has verified the
// signature of a workflow_run or check_suite delivery. It turns "GitHub
// finished a run on commit X" into the task_pipelines write and the board move
// that implies, by handing the commit to the pipeline runner's resolver.
//
// It acts only on a COMPLETED event. An in-progress one carries no conclusion,
// so resolving on it would spend a round-trip to learn nothing; the run's own
// completion is moments away and carries the answer.
//
// The reason is diagnostic only, echoed back to GitHub's delivery log so a
// human debugging a hook can see what the endpoint made of it.
func (s *Service) HandleGitHubWorkflowEvent(ctx context.Context, repositoryID uuid.UUID, deliveryID, action, status, headSHA string) (bool, string) {
	if strings.TrimSpace(headSHA) == "" {
		return false, "no head sha in payload"
	}
	// workflow_run reports both: `action` is requested/in_progress/completed and
	// `status` is queued/in_progress/completed. check_suite reports only
	// `action`. Either saying "completed" is enough.
	if action != "completed" && status != "completed" {
		return false, "run is not completed yet"
	}
	if s.seenDelivery(ctx, deliveryID, time.Now()) {
		return false, "duplicate delivery"
	}
	if s.pipelines == nil {
		return false, "pipeline runner is not configured"
	}
	n, err := s.pipelines.ResolveByHeadSHA(ctx, repositoryID, headSHA)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Str("head_sha", headSHA).
			Msg("webhook: resolving pipelines for a finished workflow run failed")
		return false, "resolving pipelines failed: " + err.Error()
	}
	if n == 0 {
		// Every run on the default branch lands here, and so does every run for
		// a task whose pipeline already settled. Not an error — most deliveries
		// are legitimately about nothing the board is waiting for.
		return false, "no pipeline is waiting on this commit"
	}
	return true, fmt.Sprintf("resolved %d pipeline(s)", n)
}

// DeliveryLedger records a GitHub delivery id and reports whether this caller
// was the first to record it. Satisfied by postgres.WebhookDeliveryStore.
type DeliveryLedger interface {
	MarkSeen(ctx context.Context, deliveryID string, retain time.Duration) (bool, error)
}

// SetDeliveryLedger wires the durable dedupe. Without it the in-memory map
// below is the whole mechanism, which is correct on a single-process install
// and is what a self-hosted deployment gets.
func (s *Service) SetDeliveryLedger(l DeliveryLedger) {
	s.deliveries = l
}

// seenDelivery reports whether this delivery has already been handled.
//
// Two ledgers, and the order matters. The DATABASE is asked first and is
// authoritative: GitHub retries to whichever pod the load balancer picks, so
// the only ledger that can answer for the fleet is the shared one. The
// in-memory map stays underneath it as the answer for a deployment with no
// Postgres and as the answer when the database cannot be reached — because
// failing CLOSED here would drop a delivery, and a dropped push is a card that
// never moves, while a duplicated one costs a reindex.
func (s *Service) seenDelivery(ctx context.Context, deliveryID string, now time.Time) bool {
	if deliveryID == "" {
		return false
	}
	if s.deliveries != nil {
		first, err := s.deliveries.MarkSeen(ctx, deliveryID, pushDeliveryTTL)
		if err == nil {
			return !first
		}
		log.Warn().Err(err).Str("delivery_id", deliveryID).
			Msg("webhook: delivery ledger unreachable, falling back to this process's own memory")
	}
	s.pushMu.Lock()
	defer s.pushMu.Unlock()
	if s.pushDeliveries == nil {
		s.pushDeliveries = make(map[string]time.Time)
	}
	for id, at := range s.pushDeliveries {
		if now.Sub(at) > pushDeliveryTTL {
			delete(s.pushDeliveries, id)
		}
	}
	if _, ok := s.pushDeliveries[deliveryID]; ok {
		return true
	}
	s.pushDeliveries[deliveryID] = now
	return false
}

func (s *Service) pushStateLocked(repositoryID uuid.UUID) *pushRepoState {
	if s.pushRepos == nil {
		s.pushRepos = make(map[uuid.UUID]*pushRepoState)
	}
	st, ok := s.pushRepos[repositoryID]
	if !ok {
		st = &pushRepoState{}
		s.pushRepos[repositoryID] = st
	}
	return st
}

// schedulePushReindex is the debounce gate: at most one pass per repo per
// interval, one pass in flight, and at most one queued rerun behind it.
func (s *Service) schedulePushReindex(ctx context.Context, repositoryID uuid.UUID) string {
	s.pushMu.Lock()
	st := s.pushStateLocked(repositoryID)
	// Recorded on every delivery, including the ones that are only queued or
	// debounced: whichever of them eventually fires the pass has to know who it
	// is acting for, and this delivery is the last moment that is on a context.
	if id, ok := tenant.From(ctx); ok {
		st.actor = id
	}
	if st.running {
		st.pending = true
		s.pushMu.Unlock()
		return "queued behind the running reindex"
	}
	if st.timer != nil {
		s.pushMu.Unlock()
		return "reindex already scheduled"
	}
	if wait := s.pushInterval() - time.Since(st.lastStart); !st.lastStart.IsZero() && wait > 0 {
		st.timer = time.AfterFunc(wait, func() { s.firePushReindex(repositoryID) })
		s.pushMu.Unlock()
		return fmt.Sprintf("debounced, reindex in %s", wait.Round(time.Second))
	}
	st.running = true
	st.lastStart = time.Now()
	s.pushMu.Unlock()
	go s.launchPushReindex(repositoryID)
	return "reindex started"
}

func (s *Service) launchPushReindex(repositoryID uuid.UUID) {
	if s.pushRunFn != nil {
		s.pushRunFn(repositoryID)
		return
	}
	s.startPushReindex(repositoryID)
}

// firePushReindex is the timer callback for a debounced trigger.
func (s *Service) firePushReindex(repositoryID uuid.UUID) {
	s.pushMu.Lock()
	st := s.pushStateLocked(repositoryID)
	st.timer = nil
	if st.running {
		st.pending = true
		s.pushMu.Unlock()
		return
	}
	st.running = true
	st.lastStart = time.Now()
	s.pushMu.Unlock()
	s.launchPushReindex(repositoryID)
}

// startPushReindex runs one pass through the same pull-then-reindex path the
// manual reindex button uses, and reports back to the debounce ledger when the
// pass (or its failure) is over. Once the pass completes, the project profile
// is refreshed too — scoped to the sections whose source files the push
// touched, so push spam cannot turn into an LLM bill while a commit that does
// change the deploy workflow or the build command is picked up immediately.
func (s *Service) startPushReindex(repositoryID uuid.UUID) {
	s.pushMu.Lock()
	tenantCtx := s.pushStateLocked(repositoryID).pushContext()
	s.pushMu.Unlock()

	ctx, cancel := context.WithTimeout(tenantCtx, 30*time.Second)
	repo, err := s.repos.Get(ctx, repositoryID)
	cancel()
	if err != nil {
		log.Error().Err(err).Str("repository_id", repositoryID.String()).Msg("webhook reindex: repository lookup failed")
		s.pushReindexDone(repositoryID)
		return
	}
	s.pullAndRestartIndexNotify(tenantCtx, repo.ID, repo.RootPath, func() {
		s.pushReindexDone(repositoryID)
		if s.profiles != nil {
			pctx, pcancel := context.WithTimeout(tenantCtx, 30*time.Second)
			defer pcancel()
			s.profiles.RefreshAfterPush(pctx, repositoryID, "push")
		}
	})
}

// pushReindexDone closes out a pass and, when pushes arrived mid-pass,
// schedules exactly one rerun on the other side of the interval.
func (s *Service) pushReindexDone(repositoryID uuid.UUID) {
	s.pushMu.Lock()
	defer s.pushMu.Unlock()
	st := s.pushStateLocked(repositoryID)
	st.running = false
	if !st.pending {
		return
	}
	st.pending = false
	if st.timer != nil {
		// A scheduled run already covers the queued pushes.
		return
	}
	wait := s.pushInterval() - time.Since(st.lastStart)
	// Never rerun back-to-back, but never wait longer than the interval either.
	if minWait := min(time.Second, s.pushInterval()); wait < minWait {
		wait = minWait
	}
	st.timer = time.AfterFunc(wait, func() { s.firePushReindex(repositoryID) })
}
