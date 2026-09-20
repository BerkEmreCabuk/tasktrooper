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

type ProviderSet struct {
	Clients map[domain.LLMProviderType]port.LLMClient
	Default domain.LLMProviderType
	EmbeddingProvider domain.LLMProviderType
	EmbeddingModel string
}

type ProviderResolver interface {
	ResolveProviders(ctx context.Context) (ProviderSet, error)
}

type ResolverFunc func(context.Context) (ProviderSet, error)

func (f ResolverFunc) ResolveProviders(ctx context.Context) (ProviderSet, error) { return f(ctx) }

func StaticResolver(set ProviderSet) ProviderResolver {
	return ResolverFunc(func(context.Context) (ProviderSet, error) { return set, nil })
}

type MultiProviderClient struct {
	mu sync.RWMutex
	resolver ProviderResolver
	fallback port.LLMClient
	limits   map[domain.LLMProviderType]*accountLimiter
	embedCfg domain.EmbeddingConfig
}

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

func (m *MultiProviderClient) SetResolver(r ProviderResolver) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resolver = r
}

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

func (m *MultiProviderClient) EmbeddingProvider(ctx context.Context) domain.LLMProviderType {
	return m.providers(ctx).EmbeddingProvider
}

func (m *MultiProviderClient) SetEmbeddingLimits(cfg domain.EmbeddingConfig) {
	m.mu.Lock()
	m.embedCfg = cfg
	m.mu.Unlock()
}

func embeddingTarget(set ProviderSet) domain.LLMProviderType {
	if set.EmbeddingProvider != "" {
		return set.EmbeddingProvider
	}
	return set.Default
}

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

func (m *MultiProviderClient) DefaultProvider(ctx context.Context) domain.LLMProviderType {
	return m.providers(ctx).Default
}

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

func errHostExecutedUtility(req domain.AgentRequest) error {
	label := domain.LLMProviderLabel(req.ProviderType)
	return fmt.Errorf("%s could not run: this agent runs on %s, which cannot serve it%s. "+
		"Configure an API-backed HTTP provider for this agent in LLM settings, or turn this step off: %w",
		utilityCallName(req), label, schemaClause(req), domain.ErrHostExecutedUnservable)
}

func utilityCallName(req domain.AgentRequest) string {
	if req.ResponseFormat != nil && strings.TrimSpace(req.ResponseFormat.Name) != "" {
		return "the `" + strings.TrimSpace(req.ResponseFormat.Name) + "` step"
	}
	return "the model call at " + callerSite()
}

func schemaClause(req domain.AgentRequest) string {
	if req.ResponseFormat == nil || req.ResponseFormat.Type != domain.ResponseFormatJSONSchema {
		return ""
	}
	return " (the step needs a JSON-schema response format, and the agent CLI has no equivalent to return one)"
}

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

func isTransportFrame(file string) bool {
	if strings.HasSuffix(file, "_test.go") {
		return false
	}
	return strings.Contains(file, "/internal/adapter/llm/") ||
		strings.Contains(file, "/internal/application/usage/")
}

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
	return nil, fmt.Errorf("no embedding-capable llm provider configured (host-executed agent CLIs cannot embed)")
}

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
