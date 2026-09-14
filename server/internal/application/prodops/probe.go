package prodops

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	defaultProbeInterval = time.Minute
	// probeFailureThreshold is how many consecutive failed probes open an
	// incident. One failure is noise (a deploy restart, a dropped packet); two
	// in a row is an outage.
	probeFailureThreshold = 2
	probeTimeout          = 10 * time.Second
)

// IncidentIngester is the Ingest side of the service, injected so the monitor
// can be tested without a database.
type IncidentIngester interface {
	Ingest(ctx context.Context, in domain.IncidentInput) (domain.Incident, error)
}

// Monitor polls every deploy target's health URL. It is the part that watches
// production directly: when an environment stops answering, an incident opens
// without waiting for an external alerting stack to be wired up, and when it
// answers again the incident closes itself.
type Monitor struct {
	targets  port.DeployTargetStore
	ingester IncidentIngester
	// policy vets every health URL before it is dialled. A target's health_url
	// is agent-writable, and this loop GETs it every minute forever — an
	// unguarded one is a scheduled internal port scanner whose findings are
	// folded into an incident and rendered into agent-facing text by remedy.go.
	policy urlguard.Policy

	mu       sync.Mutex
	failures map[uuid.UUID]int
	opened   map[uuid.UUID]bool

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewMonitor(targets port.DeployTargetStore, ingester IncidentIngester) *Monitor {
	return &Monitor{
		targets:  targets,
		ingester: ingester,
		policy:   urlguard.Default(),
		failures: map[uuid.UUID]int{},
		opened:   map[uuid.UUID]bool{},
	}
}

// SetURLPolicy overrides what counts as a dialable health URL. It replaces an
// earlier SetClient: handing the monitor a bare *http.Client let a caller
// (including a test) opt out of the guard entirely, which is exactly the shape
// that has to stop being possible.
func (m *Monitor) SetURLPolicy(p urlguard.Policy) { m.policy = p }

// Start runs a sweep every interval until the context is cancelled. Calling
// Start on a running monitor is a no-op.
func (m *Monitor) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = defaultProbeInterval
	}
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return
	}
	ctx, m.cancel = context.WithCancel(ctx)
	m.mu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		// Sweep immediately — waiting a full interval before the first probe
		// leaves a fresh deploy unwatched for no reason.
		tenant.Sweep(ctx, "health_probe", m.Sweep)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tenant.Sweep(ctx, "health_probe", m.Sweep)
			}
		}
	}()
	log.Info().Dur("interval", interval).Msg("production health monitor started")
}

func (m *Monitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
}

// Sweep probes every target that declares a health URL, once.
func (m *Monitor) Sweep(ctx context.Context) {
	targets, err := m.targets.ListAll(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("list deploy targets for health probe failed")
		return
	}
	for _, target := range targets {
		if target.HealthURL == "" {
			continue
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
		m.check(ctx, target)
	}
}

// check probes one target and drives its open/close state machine.
func (m *Monitor) check(ctx context.Context, target domain.DeployTarget) {
	ok, detail := m.probe(ctx, target.HealthURL)

	m.mu.Lock()
	if ok {
		wasOpen := m.opened[target.ID]
		m.failures[target.ID] = 0
		m.mu.Unlock()
		if wasOpen {
			// Close the open flag only after the resolve ingest succeeds; a
			// transient store error here must leave the flag set so the next
			// healthy sweep retries the close instead of losing it forever.
			if err := m.ingest(ctx, target, "recovered", true, detail); err == nil {
				m.mu.Lock()
				m.opened[target.ID] = false
				m.mu.Unlock()
			}
		}
		return
	}
	m.failures[target.ID]++
	failures := m.failures[target.ID]
	if failures >= probeFailureThreshold {
		m.opened[target.ID] = true
	}
	m.mu.Unlock()

	// Re-ingest on every failed sweep past the threshold: the occurrence count
	// is how long the outage has been going on, which the remedy engine reads.
	if failures >= probeFailureThreshold {
		m.ingest(ctx, target, detail, false, detail)
	}
}

func (m *Monitor) ingest(ctx context.Context, target domain.DeployTarget, title string, resolved bool, detail string) error {
	severity := domain.IncidentSeverityHigh
	if target.Env == domain.DeployEnvProd {
		severity = domain.IncidentSeverityCritical
	}
	// The incident text is rendered into agent-facing remedy steps, so the URL
	// goes in without its query string: a health endpoint routinely carries a
	// token there, and this is the one field that reliably ends up in a model's
	// context.
	safeURL := urlguard.LogRaw(target.HealthURL)
	in := domain.IncidentInput{
		RepositoryID: target.RepositoryID,
		Env:          target.Env,
		Source:       domain.IncidentSourceProbe,
		Severity:     severity,
		Title:        fmt.Sprintf("%s health check failing: %s", target.Env, title),
		Detail:       fmt.Sprintf("Health URL %s did not answer successfully.\n%s", safeURL, detail),
		// Fingerprint is per (env, URL) so every failed probe of the same
		// endpoint folds into one incident instead of one per sweep. It keeps the
		// raw URL because it is hashed, never rendered, and changing the input
		// would orphan every open incident.
		Fingerprint: domain.IncidentFingerprint("health-probe", target.Env, target.HealthURL),
		Payload: map[string]any{
			"health_url": safeURL,
			"provider":   target.Provider,
			"env":        target.Env,
		},
		Resolved: resolved,
	}
	if _, err := m.ingester.Ingest(ctx, in); err != nil {
		log.Warn().Err(err).Str("health_url", safeURL).Msg("probe incident ingest failed")
		return err
	}
	return nil
}

// probe performs one health request. Any non-2xx or transport error counts as
// a failure, and the reason becomes the incident's signature — which is what
// the remedy engine classifies on.
func (m *Monitor) probe(ctx context.Context, url string) (bool, string) {
	reqCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	// Guard here and not only in SaveTarget: the write-time check is offline and
	// cannot resolve, rows predating it are still in the table, and this dial
	// happens every minute for as long as the target exists — the name that
	// answered a public address at save time is free to answer 127.0.0.1 now.
	target, err := m.policy.Validate(reqCtx, url)
	if err != nil {
		log.Warn().Err(err).Str("health_url", urlguard.LogRaw(url)).Msg("health probe destination refused")
		// Deliberately says nothing about which range or why. The detail becomes
		// the incident text an agent reads, and "loopback" versus "private
		// address" versus "no such host" is a free network map.
		return false, "health URL is not an allowed destination"
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target.URL.String(), nil)
	if err != nil {
		return false, "health URL is not an allowed destination"
	}
	started := time.Now()
	resp, err := m.policy.ClientFor(target, probeTimeout).Do(req)
	if err != nil {
		if errors.Is(err, urlguard.ErrBlocked) {
			// A destination that passed Validate and was refused at dial or on a
			// redirect: same reasoning as above, the reason stays in the log.
			log.Warn().Err(err).Str("health_url", urlguard.LogValue(target.URL)).Msg("health probe destination refused")
			return false, "health URL is not an allowed destination"
		}
		return false, err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Sprintf("HTTP %d after %s", resp.StatusCode, time.Since(started).Round(time.Millisecond))
	}
	return true, fmt.Sprintf("HTTP %d in %s", resp.StatusCode, time.Since(started).Round(time.Millisecond))
}
