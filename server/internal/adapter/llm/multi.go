package llm

import (
	"context"
	"fmt"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type ProviderHealth struct {
	ProviderType domain.LLMProviderType `json:"provider_type"`
	Label        string                 `json:"label"`
	Configured   bool                   `json:"configured"`
	Active       bool                   `json:"active"`
	Status       string                 `json:"status"`
	Message      string                 `json:"message,omitempty"`
}

// ProviderSet is the stored LLM configuration, resolved for one call: the
// clients the saved credentials build, the active provider, and the pinned
// embedding provider and model.
//
// It is a value, handed to the call that asked for it and then dropped, so a
// settings save never mutates a client another call is still using.
type ProviderSet struct {
	// Clients is keyed by provider type, or by a named endpoint's uuid. Every
	// client in it was built from the stored credentials.
	Clients map[domain.LLMProviderType]port.LLMClient
	// Default is the active provider (app_settings.active_llm_provider).
	Default domain.LLMProviderType
	// EmbeddingProvider is the pinned embedding provider; "" is auto.
	EmbeddingProvider domain.LLMProviderType
	// EmbeddingModel overrides the model a caller passes, so one configured
	// model is used everywhere.
	EmbeddingModel string
}

// ProviderResolver answers "which providers are configured, and with which
// credentials", per call, from the stored settings.
type ProviderResolver interface {
	ResolveProviders(ctx context.Context) (ProviderSet, error)
}

// ResolverFunc adapts a plain function, and is what tests and the bootstrap use.
type ResolverFunc func(context.Context) (ProviderSet, error)

func (f ResolverFunc) ResolveProviders(ctx context.Context) (ProviderSet, error) { return f(ctx) }

// StaticResolver serves one fixed set to every caller: a test, or a bootstrap
// before the database is reachable.
func StaticResolver(set ProviderSet) ProviderResolver {
	return ResolverFunc(func(context.Context) (ProviderSet, error) { return set, nil })
}

type MultiProviderClient struct {
	mu sync.RWMutex
	// resolver supplies the stored ProviderSet. Nil until wired, which is the
	// boot window: until then every call falls back to the client built from
	// config.yml.
	resolver ProviderResolver
	// fallback is the client built from config.yml. It stays a field while the
	// stored clients do not, because nothing a request saves can change it.
	fallback port.LLMClient
	// limits holds one limiter per provider. Chat and embedding calls to the
	// same provider share theirs, because they share its quota.
	limits   map[domain.LLMProviderType]*accountLimiter
	embedCfg domain.EmbeddingConfig
}

// embeddingCapable reports whether a provider can produce embeddings. Groq's
// OpenAI-compatible API has no /embeddings endpoint, so it is chat-only.
func embeddingCapable(pt domain.LLMProviderType) bool {
	return pt != domain.LLMProviderGroq
}

func NewMultiProviderClient(fallback port.LLMClient, resolver ProviderResolver) *MultiProviderClient {
	return &MultiProviderClient{
		limits:   make(map[domain.LLMProviderType]*accountLimiter),
		fallback: fallback,
		resolver: resolver,
	}
}

// SetResolver installs the stored-settings resolution, after construction.
//
// Late-wired because the resolver needs the database and this client is built
// long before it — the same reason the board runner's executor arrives through
// a setter. Until it is installed every call uses the config.yml fallback,
// which is what a process that has not read its settings yet should do.
func (m *MultiProviderClient) SetResolver(r ProviderResolver) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resolver = r
}

// providers resolves the stored set.
//
// A resolution failure is an EMPTY set rather than an error, and the call then
// falls through to the config.yml fallback or to "no client configured" — the
// same two outcomes an install with nothing connected already gets. Returning
// the error instead would make every caller tell "no providers" apart from "the
// settings table did not answer".
func (m *MultiProviderClient) providers(ctx context.Context) ProviderSet {
	m.mu.RLock()
	resolver := m.resolver
	m.mu.RUnlock()
	if resolver == nil {
		return ProviderSet{}
	}
	set, err := resolver.ResolveProviders(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("llm: could not resolve the stored providers; falling back to the configured default")
		return ProviderSet{}
	}
	return set
}

// Prune, SetProvider, SetDefault, SetEmbeddingProvider and SetEmbeddingModel
// are deliberately absent.
//
// Each wrote a process-wide field from a request path — llmprovider.Service
// called all of them on every save. What replaced them is ProviderResolver: the
// same four values, resolved from the stored rows, per call.

// EmbeddingProvider reports which provider serves embeddings ("" = auto). It is
// resolved per call from the stored settings — see usage.CachingEmbedder, which
// uses it to partition its cache.
func (m *MultiProviderClient) EmbeddingProvider(ctx context.Context) domain.LLMProviderType {
	return m.providers(ctx).EmbeddingProvider
}

// SetEmbeddingLimits installs the pacing and retry policy for the provider
// account that serves embeddings. Every caller shares one MultiProviderClient,
// so the quota is respected across the indexer, RAG uploads, query rewriting —
// and chat, which spends the same account's window.
func (m *MultiProviderClient) SetEmbeddingLimits(cfg domain.EmbeddingConfig) {
	m.mu.Lock()
	m.embedCfg = cfg
	m.mu.Unlock()
}

// embeddingTarget is the provider whose account serves embeddings: the pinned
// one when set, otherwise the default.
func embeddingTarget(set ProviderSet) domain.LLMProviderType {
	if set.EmbeddingProvider != "" {
		return set.EmbeddingProvider
	}
	return set.Default
}

// limiterFor returns the limiter guarding one provider account, creating it on
// first use.
//
// The configured requests-per-minute describes the account that serves
// embeddings, so only that account gets steady-state spacing. A chat-only
// provider gets a limiter with no spacing — it still exists, because it is what
// remembers a 429 and holds the next calls back by the Retry-After the provider
// asked for.
func (m *MultiProviderClient) limiterFor(set ProviderSet, pt domain.LLMProviderType) *accountLimiter {
	m.mu.Lock()
	if m.limits == nil {
		m.limits = make(map[domain.LLMProviderType]*accountLimiter)
	}
	lim, ok := m.limits[pt]
	if !ok {
		lim = &accountLimiter{}
		m.limits[pt] = lim
	}
	cfg := m.embedCfg
	m.mu.Unlock()

	embedTarget := set.EmbeddingProvider
	if embedTarget == "" {
		embedTarget = set.Default
	}
	if pt != embedTarget {
		cfg.RequestsPerMinute = 0
	}
	lim.configure(cfg)
	return lim
}

// DefaultProvider is the active provider.
func (m *MultiProviderClient) DefaultProvider(ctx context.Context) domain.LLMProviderType {
	return m.providers(ctx).Default
}

// ClientFor returns the stored client for a provider. An empty providerType
// means the active one.
func (m *MultiProviderClient) ClientFor(ctx context.Context, providerType domain.LLMProviderType) (port.LLMClient, bool) {
	return m.clientFrom(m.providers(ctx), providerType)
}

func (m *MultiProviderClient) clientFrom(set ProviderSet, providerType domain.LLMProviderType) (port.LLMClient, bool) {
	if providerType != "" {
		if client, ok := set.Clients[providerType]; ok {
			return client, true
		}
		return nil, false
	}
	if client, ok := set.Clients[set.Default]; ok {
		return client, true
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.fallback != nil {
		return m.fallback, true
	}
	return nil, false
}

// resolve returns the client for a request together with the account key it
// spends, so the caller can charge the call to the right limiter.
func (m *MultiProviderClient) resolve(set ProviderSet, req domain.AgentRequest) (port.LLMClient, domain.LLMProviderType) {
	key := req.ProviderType
	if key == "" {
		key = set.Default
	}
	client, ok := m.clientFrom(set, req.ProviderType)
	if ok {
		return client, key
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.fallback != nil {
		return m.fallback, key
	}
	return nil, key
}

// guardHostExecuted refuses a request naming a provider that is a local process
// rather than an endpoint. Every such request is refused; none is rerouted.
//
// # What used to happen here, and why it is gone
//
// This function used to split its input in two. An AGENTIC request (one
// carrying Tools) was refused, because sending it to another provider's
// endpoint would run the user's task on an engine they did not choose, under
// another vendor's key. A UTILITY request (a JSON-schema extraction, a summary,
// a commit message, a judge's verdict) was REROUTED to the active
// default HTTP provider with the model blanked, on the reasoning that the CLI
// could not serve it anyway so a refusal only deleted the feature.
//
// The reasoning was sound and the conclusion was wrong, for a reason no amount
// of care inside this function could fix: the fallback provider is a provider
// the operator did not choose FOR THIS AGENT, and its health is unrelated to
// the health of anything they did choose. The install this was written for had a
// dead `gemini-2.0-flash` as its default and an unpaid Mistral before that, so
// every reroute converted "this agent cannot serve this step" — true, specific,
// fixable — into a 404 or a 402 from a provider the operator was not thinking
// about and could not connect to the step that failed. The fallback did not
// save a single call. It made every failure harder to read.
//
// So the split is gone and the answer is the same for both shapes: no. What
// differs is only the sentence.
//
//	AGENTIC — domain.ErrHostExecutedProvider, unchanged. It names the real
//	  problem: the run should have gone to the CLI through agent.Router and did
//	  not, and the fix is a local runner or a different provider.
//	UTILITY — errHostExecutedUtility, below. It names the step that could not
//	  run, says the agent is on Claude Code and why that cannot answer, and
//	  gives the operator the two things they can actually do about it.
//
// Callers are then responsible for making that refusal legible: a load-bearing
// step (intake, planner, the golden judge) fails its run with this message
// attached, an optional one (a commit-message rewrite, a summary) degrades and
// LOGS what it skipped and why. What none of them may do is continue as if the
// step had succeeded — which is exactly what the old fallback made possible
// when the provider it picked also failed.
func (m *MultiProviderClient) guardHostExecuted(req domain.AgentRequest) error {
	if !domain.RequiresHostExecutor(req.ProviderType) {
		return nil
	}
	if len(req.Tools) > 0 {
		return domain.ErrHostExecutedProvider(req.ProviderType)
	}
	err := errHostExecutedUtility(req)
	log.Warn().
		Str("provider", string(req.ProviderType)).
		Str("model", req.Model).
		Str("site", callerSite()).
		Str("call", utilityCallName(req)).
		Err(err).
		Msg("host-executed provider cannot serve this utility call, and nothing else will be asked to")
	return err
}

// errHostExecutedUtility is the refusal for a toolless utility call on a
// host-executed provider. It is written to be read by the operator whose board
// just stopped, so it carries all three things they need: which step, why this
// agent cannot run it, and what to change.
func errHostExecutedUtility(req domain.AgentRequest) error {
	label := string(req.ProviderType)
	if def, ok := domain.LLMProviderDefinitionFor(req.ProviderType); ok && def.Label != "" {
		label = def.Label
	}
	return fmt.Errorf("%s could not run: this agent runs on %s, which cannot serve it%s. "+
		"Configure an API-backed HTTP provider for this agent in LLM settings, or turn this step off: %w",
		utilityCallName(req), label, schemaClause(req), domain.ErrHostExecutedUnservable)
}

// utilityCallName names the step that could not run.
//
// A JSON-schema call names itself: the schema name is already required to be
// "stable and descriptive" (see domain.JSONSchemaResponseFormat), and it is
// exactly the pipeline stage — goal_intake, planner_output,
// verification_result, replan_output, golden_gate_verdict, agent_reflection,
// memory_promotion. A plain-text call has no such name, so it falls back to the
// source location, which is at least unambiguous. Both are wrapped by their
// caller with a sentence of their own, so this never has to carry the whole
// explanation on its own.
func utilityCallName(req domain.AgentRequest) string {
	if req.ResponseFormat != nil && strings.TrimSpace(req.ResponseFormat.Name) != "" {
		return "the `" + strings.TrimSpace(req.ResponseFormat.Name) + "` step"
	}
	return "the model call at " + callerSite()
}

// schemaClause states the CONCRETE reason, where there is one.
//
// For the seven JSON-schema stages there is no ambiguity and no workaround: the
// step demands a constrained-decoding response format, and the Claude Code CLI
// exposes no equivalent — it returns prose, and a prose answer to a schema
// request is a parse failure however many times it is retried. Saying so is
// what stops an operator from concluding the model is at fault and switching to
// a different one on the same CLI.
//
// For a plain-text call the reason is the plainer one already in the first half
// of the sentence, so this adds nothing.
func schemaClause(req domain.AgentRequest) string {
	if req.ResponseFormat == nil || req.ResponseFormat.Type != domain.ResponseFormatJSONSchema {
		return ""
	}
	return " (the step needs a JSON-schema response format, and the Claude Code CLI has no equivalent to return one)"
}

// callerSite names the code that made a refused call, as file:line.
//
// The alternative was threading a label through a dozen call sites for the sake
// of one log field. This reads the stack instead and skips the frames that are
// transport rather than caller: this package, and the usage-recording/embedding
// wrappers that sit between it and everything else. It runs only on the refusal
// path, which is rare by construction.
func callerSite() string {
	pcs := make([]uintptr, 16)
	// 2: runtime.Callers itself and callerSite.
	n := goruntime.Callers(2, pcs)
	frames := goruntime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if frame.File != "" && !isTransportFrame(frame.File) {
			return fmt.Sprintf("%s:%d", trimModulePath(frame.File), frame.Line)
		}
		if !more {
			return "unknown"
		}
	}
}

// isTransportFrame reports whether a file belongs to the plumbing between a
// caller and this client, rather than to a caller worth naming.
//
// A _test.go file is never transport, even inside those directories: it is the
// caller, and naming it is what lets a test assert this field at all.
func isTransportFrame(file string) bool {
	if strings.HasSuffix(file, "_test.go") {
		return false
	}
	return strings.Contains(file, "/internal/adapter/llm/") ||
		strings.Contains(file, "/internal/application/usage/")
}

// trimModulePath cuts an absolute build path down to the repo-relative one, so
// the log line reads internal/application/evolution/golden.go rather than the
// build machine's directory layout.
func trimModulePath(file string) string {
	if idx := strings.LastIndex(file, "/internal/"); idx >= 0 {
		return file[idx+1:]
	}
	return file
}

func (m *MultiProviderClient) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	if err := m.guardHostExecuted(req); err != nil {
		return domain.AgentResponse{}, err
	}
	set := m.providers(ctx)
	client, key := m.resolve(set, req)
	if client == nil {
		return domain.AgentResponse{}, fmt.Errorf("no llm client available for provider %q", req.ProviderType)
	}
	var resp domain.AgentResponse
	err := m.limiterFor(set, key).guard(ctx, key, req.Model, func(ctx context.Context) error {
		var err error
		resp, err = client.Chat(ctx, req)
		return err
	})
	return resp, err
}

func (m *MultiProviderClient) ChatStream(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	if err := m.guardHostExecuted(req); err != nil {
		return domain.AgentResponse{}, err
	}
	set := m.providers(ctx)
	client, key := m.resolve(set, req)
	if client == nil {
		return domain.AgentResponse{}, fmt.Errorf("no llm client available for provider %q", req.ProviderType)
	}
	var resp domain.AgentResponse
	err := m.limiterFor(set, key).guard(ctx, key, req.Model, func(ctx context.Context) error {
		var err error
		resp, err = client.ChatStream(ctx, req, onToken)
		return err
	})
	return resp, err
}

func (m *MultiProviderClient) Models(ctx context.Context) ([]string, error) {
	client, ok := m.ClientFor(ctx, "")
	if !ok {
		return nil, fmt.Errorf("no default llm client configured")
	}
	return client.Models(ctx)
}

func (m *MultiProviderClient) ModelsFor(ctx context.Context, providerType domain.LLMProviderType) ([]string, error) {
	client, ok := m.ClientFor(ctx, providerType)
	if !ok {
		return nil, fmt.Errorf("provider %q is not configured", providerType)
	}
	return client.Models(ctx)
}

func (m *MultiProviderClient) Embed(ctx context.Context, input string, model string) ([]float32, error) {
	set := m.providers(ctx)
	return m.limiterFor(set, embeddingTarget(set)).do(ctx, func(ctx context.Context) ([]float32, error) {
		return m.embedOnce(ctx, set, input, model)
	})
}

func (m *MultiProviderClient) embedOnce(ctx context.Context, set ProviderSet, input string, model string) ([]float32, error) {
	pinned := set.EmbeddingProvider
	clients := set.Clients
	order := make([]domain.LLMProviderType, 0, len(clients)+2)
	order = append(order, set.Default)
	for pt := range clients {
		order = append(order, pt)
	}
	if set.EmbeddingModel != "" {
		model = set.EmbeddingModel
	}
	m.mu.RLock()
	fallback := m.fallback
	m.mu.RUnlock()

	// Embedding sağlayıcısı açıkça seçildiyse (ya da "auto" kullanıcının kendi
	// Mac'ine karar verdiyse) yalnızca onu kullan — sessizce claude/cursor
	// CLI'a düşüp yanıltıcı "embedding desteklemiyor" hatası verme.
	if pinned != "" {
		if !embeddingCapable(pinned) {
			return nil, fmt.Errorf("embedding provider %q cannot produce embeddings; pick a provider that supports them in LLM settings", pinned)
		}
		c, ok := clients[pinned]
		if !ok {
			return nil, fmt.Errorf("embedding provider %q is not configured; connect it and choose its model in LLM settings", pinned)
		}
		return c.Embed(ctx, input, model)
	}

	tried := make(map[domain.LLMProviderType]bool)
	var lastErr error
	for _, pt := range order {
		if pt == "" || tried[pt] || !embeddingCapable(pt) {
			continue
		}
		tried[pt] = true
		c, ok := clients[pt]
		if !ok {
			continue
		}
		vec, err := c.Embed(ctx, input, model)
		if err == nil {
			return vec, nil
		}
		lastErr = err
	}
	// Fallback yalnızca embedding-yetkin bir sağlayıcı hiç denenmediğinde (gerçek
	// hata yokken) devreye girsin; aksi halde gerçek hatayı maskelemesin.
	if lastErr == nil && fallback != nil {
		if vec, err := fallback.Embed(ctx, input, model); err == nil {
			return vec, nil
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no embedding-capable llm provider configured (claude/cursor CLI cannot embed)")
}

// ping fills entry.Status by resolving the client for key and calling Models.
// Not-configured and missing-client cases short-circuit without a network call.
func (m *MultiProviderClient) ping(ctx context.Context, entry ProviderHealth, key domain.LLMProviderType) ProviderHealth {
	if !entry.Configured {
		entry.Status = "disconnected"
		return entry
	}
	client, ok := m.ClientFor(ctx, key)
	if !ok {
		entry.Status = "error"
		entry.Message = "client not loaded"
		return entry
	}
	pCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if _, err := client.Models(pCtx); err != nil {
		entry.Status = "error"
		entry.Message = err.Error()
	} else {
		entry.Status = "ok"
	}
	return entry
}

func (m *MultiProviderClient) HealthCheck(ctx context.Context, views []domain.LLMProviderView) []ProviderHealth {
	out := make([]ProviderHealth, len(views))
	var wg sync.WaitGroup
	for i, view := range views {
		entry := ProviderHealth{
			ProviderType: view.Definition.Type,
			Label:        view.Definition.Label,
			Configured:   view.Config.Configured,
			Active:       view.Active,
		}
		wg.Add(1)
		go func(idx int, e ProviderHealth, key domain.LLMProviderType) {
			defer wg.Done()
			out[idx] = m.ping(ctx, e, key)
		}(i, entry, view.Definition.Type)
	}
	wg.Wait()
	return out
}

// HealthCheckEndpoints is HealthCheck for named OpenAI-compatible endpoints,
// which are keyed by their uuid in the client map rather than a provider type.
func (m *MultiProviderClient) HealthCheckEndpoints(ctx context.Context, eps []domain.LLMEndpoint) []ProviderHealth {
	out := make([]ProviderHealth, len(eps))
	var wg sync.WaitGroup
	for i, ep := range eps {
		entry := ProviderHealth{
			ProviderType: domain.LLMProviderType(ep.ID),
			Label:        ep.Name,
			Configured:   ep.Configured,
		}
		wg.Add(1)
		go func(idx int, e ProviderHealth, key domain.LLMProviderType) {
			defer wg.Done()
			out[idx] = m.ping(ctx, e, key)
		}(i, entry, domain.LLMProviderType(ep.ID))
	}
	wg.Wait()
	return out
}
