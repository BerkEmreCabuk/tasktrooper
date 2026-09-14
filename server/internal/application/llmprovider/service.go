package llmprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/llm"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// InvalidateFunc is called after any write that changes what Resolve would
// return for the tenant on ctx.
//
// It replaced a ReloadFunc that took the tenant's entries and PUSHED them into
// a process-wide client. That push was the credential leak: the last tenant to
// save LLM settings decided which clients every other tenant used. Nothing is
// pushed anywhere now — a save only says "this tenant's answer changed", and
// the next call for that tenant resolves it again.
type InvalidateFunc func(ctx context.Context)

// Resolved is one tenant's LLM configuration, as DATA.
//
// Deliberately not a set of built clients: constructing those needs
// adapter/llm, and this package is application-layer. The composition root
// turns this into clients (platform/runtime), which is also where the cache
// that stops it happening per call lives.
type Resolved struct {
	Entries           []ProviderReloadEntry
	Default           domain.LLMProviderType
	EmbeddingProvider domain.LLMProviderType
	EmbeddingModel    string
}

type ProviderReloadEntry struct {
	ProviderType   domain.LLMProviderType
	BaseURL        string
	DefaultModel   string
	APIKey         string
	TimeoutSeconds int
}

type Service struct {
	store     port.LLMProviderStore
	endpoints port.LLMEndpointStore
	cipher    *secrets.Cipher
	timeout   time.Duration
	// invalidate tells the resolver cache that this tenant's answer changed.
	// Nil is valid and means nothing caches.
	invalidate InvalidateFunc

	// runnerBaseURL / runnerSigningKey wire domain.LLMProviderLocalRunner —
	// see SetControlPlane. Both empty (the zero value) means this deployment
	// never called it, which is the correct state for self-hosted/desktop
	// builds: there is no control plane to reach, so "auto" embeddings keep
	// resolving to the tenant's chat default exactly as they did before this
	// provider existed.
	runnerBaseURL    string
	runnerSigningKey string
	// embeddingHost asks the acting member's Mac whether it can embed right
	// now. Nil is valid and is the whole answer on a deployment with no control
	// plane; see SetEmbeddingHost.
	embeddingHost port.EmbeddingHostProbe
}

func NewService(store port.LLMProviderStore, endpoints port.LLMEndpointStore, cipher *secrets.Cipher, timeout time.Duration, invalidate InvalidateFunc) *Service {
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	return &Service{store: store, endpoints: endpoints, cipher: cipher, timeout: timeout, invalidate: invalidate}
}

// SetControlPlane wires domain.LLMProviderLocalRunner to the control plane a
// cloud deployment's Mac tunnel is reached through. It is additive and
// optional, the same shape as session.Service's SetChatExecutor/SetAttachments
// — called once at boot, after NewService, so that adding this capability
// never changes NewService's signature or breaks an existing caller.
//
// baseURL is tenant-manager's own origin (cfg.Server.PublicBaseURL's
// counterpart on the control-plane side); internalAuthKey is the SAME shared
// HMAC secret already threaded into this process as cfg.Cloud.InternalAuthKey
// — reused rather than duplicated under a second name, because it is
// literally the same signature tenant-manager's gateway verifies everywhere
// else on /internal/*.
//
// Calling it triggers a reload so the effect is immediate rather than waiting
// for the next unrelated Connect/Activate/SetEmbedding call.
func (s *Service) SetControlPlane(ctx context.Context, baseURL, internalAuthKey string) error {
	s.SetControlPlaneEndpoint(baseURL, internalAuthKey)
	return s.reloadAllConfigured(ctx)
}

// SetControlPlaneEndpoint is SetControlPlane's half that touches no database.
//
// It exists because the caller that most needs it is process boot, which has no
// tenant: SetControlPlane's reload reads llm_provider_configs, so calling it
// there raised tenant.ErrNoTenant and the control plane was never recorded at
// all — which is the failure that leaves "auto" embeddings falling through to
// the chat provider.
//
// Splitting them is honest rather than convenient. Where the control plane is
// is a fact about the DEPLOYMENT — one origin, one signing key, the same for
// every tenant — while which providers are configured is a fact about a tenant.
// The synthesised domain.LLMProviderLocalRunner entry these two fields produce
// carries no tenant either: it is one client that resolves the acting member
// from the request context on every call (adapter/llm/runner_embed.go), so
// registering it once for the process is not a shortcut, it is the correct
// scope.
func (s *Service) SetControlPlaneEndpoint(baseURL, internalAuthKey string) {
	s.runnerBaseURL = strings.TrimSpace(baseURL)
	s.runnerSigningKey = internalAuthKey
}

// ControlPlaneEmbeddingEntry is the local-runner client the process should hold
// so that "auto" embeddings resolve to the acting member's Mac, for every
// tenant, from the first request rather than from whenever some tenant next
// happens to reload its providers.
//
// ok is false when this deployment has no control plane (self-hosted, desktop),
// where the entry must be absent — its presence is exactly what
// MultiProviderClient.embedOnce reads to decide that "auto" means the Mac.
func (s *Service) ControlPlaneEmbeddingEntry() (ProviderReloadEntry, bool) {
	if s == nil || s.runnerBaseURL == "" {
		return ProviderReloadEntry{}, false
	}
	return ProviderReloadEntry{
		ProviderType:   domain.LLMProviderLocalRunner,
		BaseURL:        s.runnerBaseURL,
		DefaultModel:   domain.PinnedLocalEmbeddingModel,
		APIKey:         s.runnerSigningKey,
		TimeoutSeconds: runnerEmbeddingTimeoutSeconds,
	}, true
}

// runnerEmbeddingTimeoutSeconds is generous relative to LM Studio's own
// 2-minute budget (web/desktop/runner/embeddings.go's embeddingsTimeout): this
// call also crosses the tunnel and the control plane, and a cold model load on
// the Mac already spends part of the Mac-side budget.
const runnerEmbeddingTimeoutSeconds = 180

// resolveEmbeddingDisplay turns the raw stored embedding_provider/
// embedding_model ("" = auto, per SetEmbedding's convention) into what "auto"
// actually means right now, so a caller — the API response List() builds, or
// ResolvedEmbedding below — never has to re-derive MultiProviderClient's own
// resolution order (embeddingTarget/embedOnce in internal/adapter/llm/multi.go)
// to answer "what model is this tenant's search actually running on".
//
// Deliberately conservative: it only fills in the pin when this Service was
// wired to a control plane (SetControlPlane), matching exactly the condition
// MultiProviderClient uses to decide whether domain.LLMProviderLocalRunner is
// registered at all. Anything already explicit (a stored provider or model)
// is returned unchanged — this never overrides a tenant's deliberate choice.
func (s *Service) resolveEmbeddingDisplay(provider domain.LLMProviderType, model string) (domain.LLMProviderType, string) {
	if provider == "" && s.runnerBaseURL != "" {
		provider = domain.LLMProviderLocalRunner
	}
	if model == "" && provider == domain.LLMProviderLocalRunner {
		model = domain.PinnedLocalEmbeddingModel
	}
	return provider, model
}

// ResolvedEmbedding answers "what model, and what dimension, is this tenant's
// embedding search actually running on right now" — the same resolution
// List()'s response uses, exposed as its own call so a caller outside this
// package (the indexer, comparing against workspace_indexes.embedding_model/
// embedding_dims — see domain.EmbeddingProvenanceStale) never has to re-derive
// the "auto" convention itself. dimensions is 0 when the resolved model is not
// the one pinned model this package knows the size of; a caller with a better
// source (a vector it just received) should prefer that over guessing here.
func (s *Service) ResolvedEmbedding(ctx context.Context) (model string, dimensions int, err error) {
	provider, err := s.store.GetEmbeddingProvider(ctx)
	if err != nil {
		return "", 0, err
	}
	storedModel, err := s.store.GetEmbeddingModel(ctx)
	if err != nil {
		return "", 0, err
	}
	_, model = s.resolveEmbeddingDisplay(provider, storedModel)
	if model == domain.PinnedLocalEmbeddingModel {
		dimensions = domain.PinnedLocalEmbeddingDimensions
	}
	return model, dimensions, nil
}

// SetEmbeddingHost wires the probe that answers "can this member's Mac produce
// an embedding right now". Additive and optional, the same shape as
// SetControlPlane above and called from the same place at boot; nil or a nil
// probe leaves EmbeddingStatus reporting domain.EmbeddingHostUnknown, which is
// the correct answer for a deployment with no laptop in its embedding path.
func (s *Service) SetEmbeddingHost(probe port.EmbeddingHostProbe) {
	if probe == nil || !probe.Available() {
		return
	}
	s.embeddingHost = probe
}

// EmbeddingsOnMemberMac reports whether THIS DEPLOYMENT can produce embeddings
// on a member's own Mac at all — a fact about the process, not about a tenant,
// so it needs no context and hits nothing.
//
// It is what lets a client offer domain.LLMProviderLocalRunner in an embedding
// picker. It cannot be derived from AllLLMProviderDefinitions(), which that
// provider is deliberately absent from, and deriving it from the provider
// catalog was exactly the mistake that told a tenant embedding happily on their
// own Mac that no provider could produce embeddings.
func (s *Service) EmbeddingsOnMemberMac() bool {
	return s != nil && s.runnerBaseURL != ""
}

// EmbeddingStatus answers "what is producing this tenant's embeddings, and can
// it do it right now" — the question the settings page has to answer and the
// provider catalog cannot.
//
// The Mac is only asked when the answer depends on it. For an HTTP provider
// there is no laptop in the path, and a round trip through the tunnel to learn
// nothing would cost a settings page load several seconds.
func (s *Service) EmbeddingStatus(ctx context.Context) (domain.EmbeddingStatus, error) {
	stored, err := s.store.GetEmbeddingProvider(ctx)
	if err != nil {
		return domain.EmbeddingStatus{}, err
	}
	storedModel, err := s.store.GetEmbeddingModel(ctx)
	if err != nil {
		return domain.EmbeddingStatus{}, err
	}
	provider, model := s.resolveEmbeddingDisplay(stored, storedModel)
	out := domain.EmbeddingStatus{
		Provider:    provider,
		Model:       model,
		OnMemberMac: provider == domain.LLMProviderLocalRunner,
		Host:        domain.EmbeddingHostStatus{State: domain.EmbeddingHostUnknown},
	}
	if model == domain.PinnedLocalEmbeddingModel {
		out.Dimensions = domain.PinnedLocalEmbeddingDimensions
	}
	if !out.OnMemberMac || s.embeddingHost == nil {
		return out, nil
	}
	// A probe failure is NOT this call's failure. It means the Mac could not be
	// asked, which is a third thing next to "ready" and "broken", and reporting
	// it as either would put the wrong instruction in front of the user. The
	// caller still gets the provider and model, which are true regardless.
	//
	// The error's own text is logged, not returned: everything the probe can
	// fail with is about THIS process (no transport, no member on the request,
	// a tunnel that broke) and none of it is a sentence a person can act on.
	// Anything the Mac itself said comes back as a state with a detail instead.
	status, err := s.embeddingHost.Probe(ctx)
	if err != nil {
		log.Debug().Err(err).Msg("embedding host could not be asked whether it is ready")
		return out, nil
	}
	out.Host = status
	return out, nil
}

// EmbeddingNeedsMemberMac answers "does producing an embedding for this tenant
// have to reach one of its members' own machines".
//
// It is the same resolution ResolvedEmbedding uses, asked about the PROVIDER
// rather than the model, and it exists for the one caller that has to decide
// something before it starts work: a GitHub push carries no actor, so a
// webhook-triggered reindex has no member uid and no Mac to embed on. A
// deployment with no control plane answers false — embeddings there are
// produced by whatever HTTP provider the tenant configured, and a webhook
// reindex works exactly as it always did — and so does a tenant that has
// deliberately picked an HTTP embedding provider on a cloud deployment.
func (s *Service) EmbeddingNeedsMemberMac(ctx context.Context) (bool, error) {
	stored, err := s.store.GetEmbeddingProvider(ctx)
	if err != nil {
		return false, err
	}
	provider, _ := s.resolveEmbeddingDisplay(stored, "")
	return provider == domain.LLMProviderLocalRunner, nil
}

func (s *Service) List(ctx context.Context) (domain.LLMProvidersResponse, error) {
	stored, err := s.store.List(ctx)
	if err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	active, err := s.store.GetActiveProvider(ctx)
	if err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	byType := make(map[domain.LLMProviderType]domain.LLMProviderConfig, len(stored))
	for _, cfg := range stored {
		byType[cfg.ProviderType] = cfg
	}

	providers := make([]domain.LLMProviderView, 0, len(domain.AllLLMProviderDefinitions()))
	for _, def := range domain.AllLLMProviderDefinitions() {
		cfg, ok := byType[def.Type]
		if !ok {
			cfg = domain.LLMProviderConfig{
				ProviderType: def.Type,
				BaseURL:      def.DefaultBaseURL,
				DefaultModel: def.DefaultModel,
			}
		}
		providers = append(providers, domain.LLMProviderView{
			Definition: def,
			Config:     cfg,
			Active:     active == def.Type,
		})
	}
	embedding, err := s.store.GetEmbeddingProvider(ctx)
	if err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	embeddingModel, err := s.store.GetEmbeddingModel(ctx)
	if err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	// Resolved for display: a tenant on "auto" (both blank) is silently on the
	// Mac's pinned model whenever this deployment is wired to a control plane,
	// and an empty EmbeddingProvider/EmbeddingModel in the response would tell
	// the UI nothing is configured when something concrete already is — see
	// resolveEmbeddingDisplay. An explicit choice always passes through
	// unchanged.
	embedding, embeddingModel = s.resolveEmbeddingDisplay(embedding, embeddingModel)
	endpoints := []domain.LLMEndpoint{}
	if s.endpoints != nil {
		eps, err := s.endpoints.List(ctx)
		if err != nil {
			return domain.LLMProvidersResponse{}, err
		}
		if eps != nil {
			endpoints = eps
		}
	}
	return domain.LLMProvidersResponse{
		ActiveProvider:       active,
		EmbeddingProvider:    embedding,
		EmbeddingModel:       embeddingModel,
		EmbeddingOnMemberMac: s.EmbeddingsOnMemberMac(),
		Providers:            providers,
		Endpoints:            endpoints,
	}, nil
}

// errHostExecuted is the one answer every "reach this provider over HTTP" path
// gives for a provider that is a local process.
//
// Connect, Test, Activate and SetEmbedding all mean "dial this base URL with
// this key". A host-executed provider (claude_code) has neither: it is selected
// on an AGENT, and the board runner hands that agent's task to the CLI on
// whichever machine that agent's work belongs to. Letting these paths proceed
// would store a configured row for an endpoint that does not exist, and — for
// Activate — make it the tenant's default chat provider, which would break
// every chat turn with a missing-client error rather than one honest sentence
// here.
//
// It wraps domain.ErrHostExecutedUnservable so the transport can answer 409
// rather than 500: nothing about this becomes true on a retry, and a 500 tells
// every client and every monitor to back off and send the same refused request
// again.
func errHostExecuted(providerType domain.LLMProviderType) error {
	return fmt.Errorf("%s runs as a process on a machine rather than as an endpoint on the network — in cloud the assigned "+
		"member's own Mac, on a self-hosted install the host this server runs on — so there is nothing to connect, test or "+
		"activate. Select it as an agent's provider instead: %w", providerType, domain.ErrHostExecutedUnservable)
}

func (s *Service) Connect(ctx context.Context, providerType domain.LLMProviderType, req domain.ConnectLLMProviderRequest) (domain.LLMProvidersResponse, error) {
	def, ok := domain.LLMProviderDefinitionFor(providerType)
	if !ok {
		return domain.LLMProvidersResponse{}, fmt.Errorf("invalid provider type: %s", providerType)
	}
	// Availability is asked first, and it is a different refusal from the one
	// below: host-executed means "there is nothing to dial, select it on an
	// agent instead", which would be a lie for a provider that has no executor
	// to select either.
	if !def.Available {
		return domain.LLMProvidersResponse{}, domain.ErrUnavailableProvider(providerType)
	}
	if def.HostExecuted {
		return domain.LLMProvidersResponse{}, errHostExecuted(providerType)
	}

	apiKey := strings.TrimSpace(req.APIKey)
	existing, _ := s.store.Get(ctx, providerType)

	// Not the stored value: connect is the form's save, so an empty base_url
	// there means "reset me to the default", not "leave what you have".
	baseURL := firstNonBlank(req.BaseURL, def.DefaultBaseURL)
	if def.BaseURLRequired && baseURL == "" {
		return domain.LLMProvidersResponse{}, fmt.Errorf("base_url is required")
	}

	// Model bağlantı formunda sorulmaz. Yapılandırılmış bir sağlayıcıda kayıtlı
	// değer korunur, ilk kurulumda tanımın varsayılanı kullanılır. Model seçimi
	// ajan bazında yapılır; buradaki değer yalnızca geri düşüş.
	defaultModel := firstNonBlank(req.DefaultModel, existing.DefaultModel, def.DefaultModel)
	if def.ModelRequired && defaultModel == "" {
		return domain.LLMProvidersResponse{}, fmt.Errorf("default_model is required")
	}

	if def.RequiresAPIKey && apiKey == "" && !existing.HasAPIKey {
		return domain.LLMProvidersResponse{}, fmt.Errorf("api_key is required")
	}

	timeoutSeconds := resolveTimeoutSeconds(req.TimeoutSeconds, existing.TimeoutSeconds, providerType)
	if timeoutSeconds < 30 {
		return domain.LLMProvidersResponse{}, fmt.Errorf("timeout_seconds must be at least 30")
	}
	if timeoutSeconds > 3600 {
		return domain.LLMProvidersResponse{}, fmt.Errorf("timeout_seconds must be at most 3600")
	}

	if err := s.testClient(providerType, baseURL, defaultModel, s.resolveAPIKey(apiKey, existing.HasAPIKey, ctx, providerType), timeoutSeconds); err != nil {
		return domain.LLMProvidersResponse{}, fmt.Errorf("connection test failed: %w", err)
	}

	cfg := domain.LLMProviderConfig{
		ProviderType:   providerType,
		BaseURL:        baseURL,
		DefaultModel:   defaultModel,
		TimeoutSeconds: timeoutSeconds,
		Configured:     true,
	}
	if err := s.store.Upsert(ctx, cfg); err != nil {
		return domain.LLMProvidersResponse{}, err
	}

	if apiKey != "" {
		if s.cipher == nil {
			return domain.LLMProvidersResponse{}, fmt.Errorf("secret storage unavailable")
		}
		encrypted, err := s.cipher.Encrypt(apiKey)
		if err != nil {
			return domain.LLMProvidersResponse{}, err
		}
		if err := s.store.SetAPIKey(ctx, providerType, encrypted); err != nil {
			return domain.LLMProvidersResponse{}, err
		}
	}

	// Only change the default provider when no provider is currently configured,
	// or we're reconnecting the one already set as default.
	currentActive, _ := s.store.GetActiveProvider(ctx)
	if currentActive == "" || currentActive == providerType {
		if err := s.store.SetActiveProvider(ctx, providerType); err != nil {
			return domain.LLMProvidersResponse{}, err
		}
	} else {
		currentCfg, _ := s.store.Get(ctx, currentActive)
		if !currentCfg.Configured {
			if err := s.store.SetActiveProvider(ctx, providerType); err != nil {
				return domain.LLMProvidersResponse{}, err
			}
		}
	}
	if err := s.reloadAllConfigured(ctx); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	return s.List(ctx)
}

// BootstrapEmbeddings points the OpenAI-compatible provider slot at baseURL and
// selects it for embeddings, so RAG and code search work on a fresh install
// with nobody opening the settings page. baseURL is the embedder the desktop
// app bundles and starts.
//
// It deliberately does NOT go through Connect: Connect's reachability test is
// GET /models, and an embedder that serves only POST /v1/embeddings is a
// correct embedder. It also never touches the ACTIVE (chat) provider — this
// endpoint cannot answer a chat turn.
//
// Idempotent by intent rather than by a flag: it rewrites the base URL when it
// has changed (the desktop picks a new port per run) and it leaves an embedding
// provider a human has already chosen alone.
func (s *Service) BootstrapEmbeddings(ctx context.Context, baseURL string) error {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	const providerType = domain.LLMProviderLocal

	existing, _ := s.store.Get(ctx, providerType)
	if !existing.Configured || existing.BaseURL != baseURL || existing.DefaultModel == "" {
		if err := s.store.Upsert(ctx, domain.LLMProviderConfig{
			ProviderType:   providerType,
			BaseURL:        baseURL,
			DefaultModel:   firstNonBlank(existing.DefaultModel, domain.PinnedLocalEmbeddingModel),
			TimeoutSeconds: resolveTimeoutSeconds(existing.TimeoutSeconds, 0, providerType),
			Configured:     true,
		}); err != nil {
			return err
		}
	}

	chosen, _ := s.store.GetEmbeddingProvider(ctx)
	if chosen != "" && chosen != providerType {
		return s.reloadAllConfigured(ctx)
	}
	if err := s.store.SetEmbeddingProvider(ctx, providerType); err != nil {
		return err
	}
	if model, _ := s.store.GetEmbeddingModel(ctx); model == "" {
		if err := s.store.SetEmbeddingModel(ctx, domain.PinnedLocalEmbeddingModel); err != nil {
			return err
		}
	}
	return s.reloadAllConfigured(ctx)
}

// BootstrapFromEnv configures providers not yet set up using well-known env vars.
// Called once at startup; skips any provider that already has stored config.
func (s *Service) BootstrapFromEnv(ctx context.Context) {
	type candidate struct {
		providerType domain.LLMProviderType
		envKey       string
	}
	for _, c := range []candidate{
		{domain.LLMProviderAnthropic, "ANTHROPIC_API_KEY"},
		{domain.LLMProviderOpenAI, "OPENAI_API_KEY"},
	} {
		apiKey := os.Getenv(c.envKey)
		if apiKey == "" {
			continue
		}
		existing, _ := s.store.Get(ctx, c.providerType)
		if existing.Configured {
			continue
		}
		def, _ := domain.LLMProviderDefinitionFor(c.providerType)
		_, _ = s.Connect(ctx, c.providerType, domain.ConnectLLMProviderRequest{
			BaseURL:        def.DefaultBaseURL,
			APIKey:         apiKey,
			TimeoutSeconds: def.DefaultTimeoutSeconds,
		})
	}

	// Bootstrap Gemini (AI Studio) when GOOGLE_API_KEY is set. GOOGLE_CLOUD_PROJECT
	// is optional — only the legacy keyless Vertex/ADC path needs it, and the API
	// key alone routes to the free generativelanguage.googleapis.com backend.
	if apiKey := os.Getenv("GOOGLE_API_KEY"); apiKey != "" {
		existing, _ := s.store.Get(ctx, domain.LLMProviderGemini)
		if !existing.Configured {
			def, _ := domain.LLMProviderDefinitionFor(domain.LLMProviderGemini)
			_, _ = s.Connect(ctx, domain.LLMProviderGemini, domain.ConnectLLMProviderRequest{
				BaseURL:        def.DefaultBaseURL,
				APIKey:         apiKey,
				TimeoutSeconds: def.DefaultTimeoutSeconds,
			})
		}
	}
}

// Activate sets the default provider. The ref is EITHER a native provider type
// OR an endpoint uuid; both must already be configured.
func (s *Service) Activate(ctx context.Context, providerType domain.LLMProviderType) (domain.LLMProvidersResponse, error) {
	// Only native types are checked here: an endpoint ref is a uuid, which is
	// never a declared provider and so never unavailable.
	if domain.ValidLLMProviderType(string(providerType)) && !domain.ProviderAvailable(providerType) {
		return domain.LLMProvidersResponse{}, domain.ErrUnavailableProvider(providerType)
	}
	if domain.RequiresHostExecutor(providerType) {
		return domain.LLMProvidersResponse{}, errHostExecuted(providerType)
	}
	if domain.ValidLLMProviderType(string(providerType)) {
		cfg, err := s.store.Get(ctx, providerType)
		if err != nil {
			return domain.LLMProvidersResponse{}, err
		}
		if !cfg.Configured {
			return domain.LLMProvidersResponse{}, fmt.Errorf("provider is not configured")
		}
	} else if s.endpoints != nil {
		ep, err := s.endpoints.Get(ctx, string(providerType))
		if err != nil {
			return domain.LLMProvidersResponse{}, fmt.Errorf("invalid provider ref: %s", providerType)
		}
		if !ep.Configured {
			return domain.LLMProvidersResponse{}, fmt.Errorf("endpoint is not configured")
		}
	} else {
		return domain.LLMProvidersResponse{}, fmt.Errorf("invalid provider type: %s", providerType)
	}
	if err := s.store.SetActiveProvider(ctx, providerType); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	if err := s.reloadAllConfigured(ctx); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	return s.List(ctx)
}

func (s *Service) Disconnect(ctx context.Context, providerType domain.LLMProviderType) (domain.LLMProvidersResponse, error) {
	def, ok := domain.LLMProviderDefinitionFor(providerType)
	if !ok {
		return domain.LLMProvidersResponse{}, fmt.Errorf("invalid provider type: %s", providerType)
	}
	cfg := domain.LLMProviderConfig{
		ProviderType:   providerType,
		BaseURL:        def.DefaultBaseURL,
		DefaultModel:   def.DefaultModel,
		TimeoutSeconds: 0,
		Configured:     false,
	}
	if err := s.store.Upsert(ctx, cfg); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	if err := s.store.DeleteAPIKey(ctx, providerType); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	active, err := s.store.GetActiveProvider(ctx)
	if err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	if active == providerType {
		if err := s.store.SetActiveProvider(ctx, domain.LLMProviderLocal); err != nil {
			return domain.LLMProvidersResponse{}, err
		}
	}
	if err := s.reloadAllConfigured(ctx); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	return s.List(ctx)
}

func (s *Service) Test(ctx context.Context, providerType domain.LLMProviderType, req domain.TestLLMProviderRequest) error {
	def, ok := domain.LLMProviderDefinitionFor(providerType)
	if !ok {
		return fmt.Errorf("invalid provider type: %s", providerType)
	}
	if !def.Available {
		return domain.ErrUnavailableProvider(providerType)
	}
	if def.HostExecuted {
		return errHostExecuted(providerType)
	}
	existing, _ := s.store.Get(ctx, providerType)
	// The stored configuration is what a test with no overrides is ABOUT.
	// Falling straight through to the definition's default dialled
	// 127.0.0.1:1234 for a provider connected on 127.0.0.1:11234 and reported
	// the refusal as the user's, which is worse than not testing at all —
	// the answer was true about an address nobody configured. This is the same
	// request → stored → default order TestEndpoint already uses.
	baseURL := firstNonBlank(req.BaseURL, existing.BaseURL, def.DefaultBaseURL)
	defaultModel := firstNonBlank(req.DefaultModel, existing.DefaultModel, def.DefaultModel)
	apiKey := s.resolveAPIKey(strings.TrimSpace(req.APIKey), existing.HasAPIKey, ctx, providerType)
	timeoutSeconds := resolveTimeoutSeconds(req.TimeoutSeconds, existing.TimeoutSeconds, providerType)
	return s.testClient(providerType, baseURL, defaultModel, apiKey, timeoutSeconds)
}

// reloadAllConfigured is now an INVALIDATION, not a reload.
//
// Its callers are every write that changes what Resolve returns — Connect,
// Disconnect, the active-provider switch, the embedding pin, endpoint CRUD. It
// used to rebuild the process-wide client map from THIS tenant's rows, which is
// what handed the next caller somebody else's credentials. It now says only
// "this tenant's answer changed"; the cache drops that tenant's entry and the
// next call for that tenant resolves it again.
//
// The name is kept because every call site reads correctly with it, and because
// the diff is easier to review as a change of meaning than as a rename of ten
// lines that stayed the same.
func (s *Service) reloadAllConfigured(ctx context.Context) error {
	if s.invalidate != nil {
		s.invalidate(ctx)
	}
	return nil
}

// Resolve reads the LLM configuration of the tenant on ctx.
//
// Every read below is policy-scoped, so what comes back is that tenant's rows
// and nobody else's — including the decrypted API keys. This used to be the
// first half of reloadAllConfigured, whose second half pushed the result into a
// process-wide client; the push is gone and the result is returned instead.
func (s *Service) Resolve(ctx context.Context) (Resolved, error) {
	stored, err := s.store.List(ctx)
	if err != nil {
		return Resolved{}, err
	}
	active, err := s.store.GetActiveProvider(ctx)
	if err != nil {
		return Resolved{}, err
	}
	entries := make([]ProviderReloadEntry, 0, len(stored))
	for _, cfg := range stored {
		if !cfg.Configured {
			continue
		}
		if cfg.TimeoutSeconds == 0 {
			cfg.TimeoutSeconds = resolveTimeoutSeconds(0, 0, cfg.ProviderType)
			if err := s.store.Upsert(ctx, cfg); err != nil {
				return Resolved{}, err
			}
		}
		apiKey, err := s.decryptAPIKey(ctx, cfg.ProviderType)
		if err != nil {
			return Resolved{}, err
		}
		entries = append(entries, ProviderReloadEntry{
			ProviderType:   cfg.ProviderType,
			BaseURL:        cfg.BaseURL,
			DefaultModel:   cfg.DefaultModel,
			APIKey:         apiKey,
			TimeoutSeconds: cfg.TimeoutSeconds,
		})
	}
	// Named endpoints: one entry per configured endpoint, keyed by its uuid.
	if s.endpoints != nil {
		eps, err := s.endpoints.List(ctx)
		if err != nil {
			return Resolved{}, err
		}
		for _, ep := range eps {
			if !ep.Configured {
				continue
			}
			apiKey, err := s.decryptEndpointAPIKey(ctx, ep.ID)
			if err != nil {
				return Resolved{}, err
			}
			entries = append(entries, ProviderReloadEntry{
				ProviderType:   domain.LLMProviderType(ep.ID),
				BaseURL:        ep.BaseURL,
				DefaultModel:   ep.DefaultModel,
				APIKey:         apiKey,
				TimeoutSeconds: ep.TimeoutSeconds,
			})
		}
	}
	// domain.LLMProviderLocalRunner: synthesised, never stored. There is no
	// llm_provider_configs row for it — nothing about it is a tenant setting —
	// so it is added here, generated fresh on every reload from whatever
	// SetControlPlane last set, rather than round-tripped through the store
	// like every entry above it. Absent entirely when this deployment was
	// never wired to a control plane (self-hosted/desktop), which is what
	// keeps embedOnce's auto-resolution falling back to the old
	// default-provider behaviour there — see multi.go's embeddingTarget.
	if entry, ok := s.ControlPlaneEmbeddingEntry(); ok {
		entries = append(entries, entry)
	}
	embedding, err := s.store.GetEmbeddingProvider(ctx)
	if err != nil {
		return Resolved{}, err
	}
	embeddingModel, err := s.store.GetEmbeddingModel(ctx)
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{
		Entries:           entries,
		Default:           active,
		EmbeddingProvider: embedding,
		EmbeddingModel:    embeddingModel,
	}, nil
}

// EmbeddingProvider returns the pinned embedding provider ("" = auto).
func (s *Service) EmbeddingProvider(ctx context.Context) (domain.LLMProviderType, error) {
	return s.store.GetEmbeddingProvider(ctx)
}

// errNoEmbeddingsFromProvider is the one sentence this path gives for "the
// thing you named cannot produce embeddings", whatever the reason — a chat-only
// provider, a ref that matches no endpoint, a ref that is not even shaped like
// one. It is deliberately about EMBEDDINGS and never about storage: everything
// below it used to be able to reach a user's screen, and the failure that
// prompted this was a tenant being shown `invalid input syntax for type uuid`
// by a page whose subject is which model indexes their code.
func errNoEmbeddingsFromProvider() error {
	return fmt.Errorf("this provider cannot produce embeddings")
}

// errNoSuchEndpoint is what every "the endpoint you named is not there" path
// says, for both reasons that can be true: the ref is not shaped like a row id,
// or it is and no row has it. They are one answer to the caller because they
// are one fact — this endpoint does not exist — and because the alternative was
// the store's own sentence, which names a table, a column and a SQLSTATE.
func errNoSuchEndpoint(id string) error {
	return fmt.Errorf("no such endpoint: %s", id)
}

// endpointRef reports whether ref can be an llm_endpoints row id at all.
//
// The check is the fix for a real bug, not defensive padding: llm_endpoints.id
// is a uuid column, so a provider string that is not a uuid — "local_runner",
// a typo, anything a future non-catalog provider key introduces — reached
// Postgres as a query parameter and came back as SQLSTATE 22P02, which was
// then printed under the embedding model picker. Deciding here that a non-uuid
// ref names no endpoint keeps the database out of a question it was never
// being asked.
func endpointRef(ref domain.LLMProviderType) bool {
	_, err := uuid.Parse(string(ref))
	return err == nil
}

// ListEmbeddingModels returns embedding-capable models for a provider. For LM
// Studio (local) it uses the native /api/v0/models API which tags model type;
// other providers fall back to their plain model list.
//
// No error this returns is ever a storage error. See errNoEmbeddingsFromProvider.
func (s *Service) ListEmbeddingModels(ctx context.Context, providerType domain.LLMProviderType) ([]string, error) {
	// The Mac, first, because it is neither a catalog provider nor an endpoint
	// row and every branch below would misread it. Its catalog is one model by
	// construction: domain.PinnedLocalEmbeddingModel is pinned on BOTH sides —
	// the runner refuses a request naming anything else
	// (web/desktop/runner/embeddings.go) — so listing whatever LM Studio
	// happens to have downloaded would offer choices that cannot be chosen.
	// Whether that one model is loaded right now is a different question, asked
	// by EmbeddingStatus, and it must not empty this list: an empty picker next
	// to a warning is what the user was already looking at.
	if providerType == domain.LLMProviderLocalRunner {
		if s.runnerBaseURL == "" {
			return nil, fmt.Errorf("this deployment has no control plane, so there is no Mac to produce embeddings on")
		}
		return []string{domain.PinnedLocalEmbeddingModel}, nil
	}
	// Named endpoint (uuid): not in llm_provider_configs. Query its own catalog —
	// LM Studio's native API tags embedding models; otherwise fall back to the
	// plain /models list so the user can pick the embedding one.
	if !domain.ValidLLMProviderType(string(providerType)) {
		if s.endpoints == nil || !endpointRef(providerType) {
			return nil, errNoEmbeddingsFromProvider()
		}
		ep, err := s.endpoints.Get(ctx, string(providerType))
		if err != nil {
			// The store's own wording names a table and a column. Whatever went
			// wrong, what the person asking can act on is that this provider is
			// not one that answers.
			return nil, errNoEmbeddingsFromProvider()
		}
		if !ep.Configured || ep.BaseURL == "" {
			return nil, fmt.Errorf("provider is not connected")
		}
		if models, err := lmStudioEmbeddingModels(ctx, ep.BaseURL); err == nil && len(models) > 0 {
			return models, nil
		}
		apiKey, err := s.decryptEndpointAPIKey(ctx, ep.ID)
		if err != nil {
			return nil, fmt.Errorf("this endpoint's stored API key could not be read; reconnect it with the key again")
		}
		models, err := openAICompatibleModels(ctx, ep.BaseURL, apiKey)
		if err != nil {
			return nil, err
		}
		return preferEmbeddingModels(models), nil
	}
	cfg, err := s.store.Get(ctx, providerType)
	if err != nil {
		return nil, errNoEmbeddingsFromProvider()
	}
	if !cfg.Configured || cfg.BaseURL == "" {
		return nil, fmt.Errorf("provider is not connected")
	}
	switch providerType {
	case domain.LLMProviderLocal:
		// LM Studio tags model types; a remote OpenAI-compatible host in the
		// same slot does not, so fall back to its plain catalog.
		if models, err := lmStudioEmbeddingModels(ctx, cfg.BaseURL); err == nil && len(models) > 0 {
			return models, nil
		}
		apiKey, err := s.decryptAPIKey(ctx, providerType)
		if err != nil {
			return nil, err
		}
		models, err := openAICompatibleModels(ctx, cfg.BaseURL, apiKey)
		if err != nil {
			return nil, err
		}
		return preferEmbeddingModels(models), nil
	case domain.LLMProviderGemini:
		// Bilinen embedding modelleri; /models listesi embedding tipini
		// işaretlemediği (ve key'siz 401 döndüğü) için statik.
		// text-embedding-004 v1beta'dan kaldırıldı (404) — listede yok.
		return []string{"gemini-embedding-001"}, nil
	case domain.LLMProviderOpenAI:
		return []string{"text-embedding-3-small", "text-embedding-3-large", "text-embedding-ada-002"}, nil
	default:
		return nil, fmt.Errorf("this provider cannot produce embeddings")
	}
}

// preferEmbeddingModels narrows a plain /models catalog down to the models that
// name themselves embedding models (mistral-embed, codestral-embed,
// text-embedding-3-small, …). Providers that expose no type info would
// otherwise offer their chat models as embedding candidates, which only fails
// once indexing starts. When nothing matches the full catalog is returned:
// self-hosted embedding models are named freely (bge-m3, nomic-…), and an
// unfiltered list beats an empty dropdown.
func preferEmbeddingModels(models []string) []string {
	embedding := make([]string, 0, len(models))
	for _, m := range models {
		if strings.Contains(strings.ToLower(m), "embed") {
			embedding = append(embedding, m)
		}
	}
	if len(embedding) == 0 {
		return models
	}
	return embedding
}

// lmStudioEmbeddingModels queries LM Studio's native model catalog and returns
// only models of type "embeddings" (downloaded; loaded on demand via JIT).
func lmStudioEmbeddingModels(ctx context.Context, baseURL string) ([]string, error) {
	host := strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/v1")
	url := host + "/api/v0/models"
	reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list LM Studio models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LM Studio /api/v0/models returned status %d", resp.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := make([]string, 0)
	for _, m := range payload.Data {
		if m.Type == "embeddings" {
			out = append(out, m.ID)
		}
	}
	return out, nil
}

// SetEmbedding pins the embedding provider + model and reloads clients. Groq is
// rejected because its API has no /embeddings endpoint. Empty provider = auto.
func (s *Service) SetEmbedding(ctx context.Context, providerType domain.LLMProviderType, model string) (domain.LLMProvidersResponse, error) {
	if providerType != "" {
		if domain.ValidLLMProviderType(string(providerType)) {
			if !domain.ProviderAvailable(providerType) {
				return domain.LLMProvidersResponse{}, domain.ErrUnavailableProvider(providerType)
			}
			if providerType == domain.LLMProviderGroq {
				return domain.LLMProvidersResponse{}, fmt.Errorf("%s cannot produce embeddings; pick a different provider for embeddings", providerType)
			}
			// A CLI session answers a task, not an embedding request: there is
			// no /embeddings to call and no vector to get back.
			if domain.RequiresHostExecutor(providerType) {
				return domain.LLMProvidersResponse{}, fmt.Errorf("%s cannot produce embeddings; pick a different provider for embeddings", providerType)
			}
		} else if providerType == domain.LLMProviderLocalRunner {
			// Accepted, and it has to be: List() RESOLVES a blank stored value
			// to this provider for display, so the settings page shows
			// "local_runner" as the current selection and saving what is on
			// screen was refused with "invalid provider ref" — the one choice a
			// user could not make was the one already in force.
			//
			// It is refused on a deployment with no control plane, where there
			// is no tunnel to reach a Mac through and storing it would make
			// every later embedding call fail with no way to see why from here.
			if s.runnerBaseURL == "" {
				return domain.LLMProvidersResponse{}, fmt.Errorf(
					"this deployment has no control plane, so there is no Mac to produce embeddings on")
			}
			// The model is a pin on both sides. Storing a different one would
			// be accepted here and then refused by the Mac on every single
			// embedding call, so the whole tenant's indexing would stop with
			// the settings page still showing the choice as saved.
			if model != "" && model != domain.PinnedLocalEmbeddingModel {
				return domain.LLMProvidersResponse{}, fmt.Errorf(
					"embeddings on a Mac are pinned to %s and cannot be substituted — vectors from another model are not "+
						"comparable to the ones already indexed", domain.PinnedLocalEmbeddingModel)
			}
			model = domain.PinnedLocalEmbeddingModel
		} else if s.endpoints != nil && endpointRef(providerType) {
			ep, err := s.endpoints.Get(ctx, string(providerType))
			if err != nil {
				return domain.LLMProvidersResponse{}, fmt.Errorf("invalid provider ref: %s", providerType)
			}
			if !ep.Configured {
				return domain.LLMProvidersResponse{}, fmt.Errorf("endpoint is not configured")
			}
		} else {
			return domain.LLMProvidersResponse{}, fmt.Errorf("invalid provider type: %s", providerType)
		}
	}
	if err := s.store.SetEmbeddingProvider(ctx, providerType); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	if err := s.store.SetEmbeddingModel(ctx, model); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	if err := s.reloadAllConfigured(ctx); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	return s.List(ctx)
}

func (s *Service) BootstrapFromYAML(ctx context.Context, yamlCfg domain.LLMConfig) error {
	active, err := s.store.GetActiveProvider(ctx)
	if err != nil {
		return err
	}
	cfg, err := s.store.Get(ctx, active)
	if err != nil {
		return err
	}
	if cfg.Configured {
		return s.reloadAllConfigured(ctx)
	}
	if yamlCfg.BaseURL == "" {
		return nil
	}
	timeoutSeconds := int(yamlCfg.Timeout.Seconds())
	if timeoutSeconds <= 0 {
		timeoutSeconds = resolveTimeoutSeconds(0, 0, domain.LLMProviderLocal)
	}
	localCfg := domain.LLMProviderConfig{
		ProviderType:   domain.LLMProviderLocal,
		BaseURL:        yamlCfg.BaseURL,
		DefaultModel:   yamlCfg.Model,
		TimeoutSeconds: timeoutSeconds,
		Configured:     true,
	}
	if err := s.store.Upsert(ctx, localCfg); err != nil {
		return err
	}
	if yamlCfg.APIKey != "" && s.cipher != nil {
		encrypted, err := s.cipher.Encrypt(yamlCfg.APIKey)
		if err != nil {
			return err
		}
		if err := s.store.SetAPIKey(ctx, domain.LLMProviderLocal, encrypted); err != nil {
			return err
		}
	}
	if err := s.store.SetActiveProvider(ctx, domain.LLMProviderLocal); err != nil {
		return err
	}
	return s.reloadAllConfigured(ctx)
}

func (s *Service) resolveAPIKey(incoming string, hasStored bool, ctx context.Context, providerType domain.LLMProviderType) string {
	if incoming != "" {
		return incoming
	}
	if !hasStored {
		return ""
	}
	key, err := s.decryptAPIKey(ctx, providerType)
	if err != nil {
		return ""
	}
	return key
}

func (s *Service) decryptAPIKey(ctx context.Context, providerType domain.LLMProviderType) (string, error) {
	encrypted, err := s.store.GetAPIKeyEncrypted(ctx, providerType)
	if err != nil {
		return "", err
	}
	if len(encrypted) == 0 {
		return "", nil
	}
	if s.cipher == nil {
		return "", fmt.Errorf("secret storage unavailable")
	}
	return s.cipher.Decrypt(encrypted)
}

func (s *Service) testClient(providerType domain.LLMProviderType, baseURL, defaultModel, apiKey string, timeoutSeconds int) error {
	timeout := time.Duration(timeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = s.timeout
	}
	client := llm.NewProviderClient(providerType, baseURL, defaultModel, apiKey, timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err := client.Models(ctx)
	return err
}

// endpointProviderRef is the client-map key used when testing an endpoint before
// it has an id. Any non-native ref routes through the factory's OpenAI-compatible
// default case.
const endpointProviderRef = domain.LLMProviderType("endpoint")

func (s *Service) decryptEndpointAPIKey(ctx context.Context, id string) (string, error) {
	if s.endpoints == nil {
		return "", nil
	}
	encrypted, err := s.endpoints.GetAPIKeyEncrypted(ctx, id)
	if err != nil {
		return "", err
	}
	if len(encrypted) == 0 {
		return "", nil
	}
	if s.cipher == nil {
		return "", fmt.Errorf("secret storage unavailable")
	}
	return s.cipher.Decrypt(encrypted)
}

func normalizeEndpointTimeout(seconds int) (int, error) {
	if seconds <= 0 {
		return 120, nil
	}
	if seconds < 30 {
		return 0, fmt.Errorf("timeout_seconds must be at least 30")
	}
	if seconds > 3600 {
		return 0, fmt.Errorf("timeout_seconds must be at most 3600")
	}
	return seconds, nil
}

// CreateEndpoint validates + connection-tests a new named OpenAI-compatible
// endpoint, persists it (encrypting the key), and reloads clients.
func (s *Service) CreateEndpoint(ctx context.Context, req domain.SaveLLMEndpointRequest) (domain.LLMProvidersResponse, error) {
	if s.endpoints == nil {
		return domain.LLMProvidersResponse{}, fmt.Errorf("endpoint storage unavailable")
	}
	name := strings.TrimSpace(req.Name)
	baseURL := strings.TrimSpace(req.BaseURL)
	if name == "" {
		return domain.LLMProvidersResponse{}, fmt.Errorf("name is required")
	}
	if baseURL == "" {
		return domain.LLMProvidersResponse{}, fmt.Errorf("base_url is required")
	}
	timeoutSeconds, err := normalizeEndpointTimeout(req.TimeoutSeconds)
	if err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	apiKey := strings.TrimSpace(req.APIKey)
	defaultModel := strings.TrimSpace(req.DefaultModel)

	if err := s.testClient(endpointProviderRef, baseURL, defaultModel, apiKey, timeoutSeconds); err != nil {
		return domain.LLMProvidersResponse{}, fmt.Errorf("connection test failed: %w", err)
	}

	created, err := s.endpoints.Create(ctx, domain.LLMEndpoint{
		Name:           name,
		BaseURL:        baseURL,
		DefaultModel:   defaultModel,
		TimeoutSeconds: timeoutSeconds,
		Configured:     true,
	})
	if err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	if apiKey != "" {
		if s.cipher == nil {
			return domain.LLMProvidersResponse{}, fmt.Errorf("secret storage unavailable")
		}
		encrypted, err := s.cipher.Encrypt(apiKey)
		if err != nil {
			return domain.LLMProvidersResponse{}, err
		}
		if err := s.endpoints.SetAPIKey(ctx, created.ID, encrypted); err != nil {
			return domain.LLMProvidersResponse{}, err
		}
	}
	if err := s.reloadAllConfigured(ctx); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	return s.List(ctx)
}

// UpdateEndpoint merges the request onto the stored endpoint (empty fields keep
// their stored value, empty api_key keeps the stored key), re-tests, and reloads.
func (s *Service) UpdateEndpoint(ctx context.Context, id string, req domain.SaveLLMEndpointRequest) (domain.LLMProvidersResponse, error) {
	if s.endpoints == nil {
		return domain.LLMProvidersResponse{}, fmt.Errorf("endpoint storage unavailable")
	}
	if !endpointRef(domain.LLMProviderType(id)) {
		return domain.LLMProvidersResponse{}, errNoSuchEndpoint(id)
	}
	existing, err := s.endpoints.Get(ctx, id)
	if err != nil {
		return domain.LLMProvidersResponse{}, errNoSuchEndpoint(id)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = existing.Name
	}
	baseURL := strings.TrimSpace(req.BaseURL)
	if baseURL == "" {
		baseURL = existing.BaseURL
	}
	defaultModel := strings.TrimSpace(req.DefaultModel)
	if defaultModel == "" {
		defaultModel = existing.DefaultModel
	}
	timeoutSeconds := req.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = existing.TimeoutSeconds
	}
	timeoutSeconds, err = normalizeEndpointTimeout(timeoutSeconds)
	if err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	apiKey := strings.TrimSpace(req.APIKey)
	testKey := apiKey
	if testKey == "" && existing.HasAPIKey {
		testKey, _ = s.decryptEndpointAPIKey(ctx, id)
	}

	if err := s.testClient(domain.LLMProviderType(id), baseURL, defaultModel, testKey, timeoutSeconds); err != nil {
		return domain.LLMProvidersResponse{}, fmt.Errorf("connection test failed: %w", err)
	}

	if err := s.endpoints.Update(ctx, domain.LLMEndpoint{
		ID:             id,
		Name:           name,
		BaseURL:        baseURL,
		DefaultModel:   defaultModel,
		TimeoutSeconds: timeoutSeconds,
		Configured:     true,
	}); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	if apiKey != "" {
		if s.cipher == nil {
			return domain.LLMProvidersResponse{}, fmt.Errorf("secret storage unavailable")
		}
		encrypted, err := s.cipher.Encrypt(apiKey)
		if err != nil {
			return domain.LLMProvidersResponse{}, err
		}
		if err := s.endpoints.SetAPIKey(ctx, id, encrypted); err != nil {
			return domain.LLMProvidersResponse{}, err
		}
	}
	if err := s.reloadAllConfigured(ctx); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	return s.List(ctx)
}

// DeleteEndpoint removes an endpoint (its secret cascades) and resets the
// active/embedding refs if they pointed at it.
func (s *Service) DeleteEndpoint(ctx context.Context, id string) (domain.LLMProvidersResponse, error) {
	if s.endpoints == nil {
		return domain.LLMProvidersResponse{}, fmt.Errorf("endpoint storage unavailable")
	}
	if !endpointRef(domain.LLMProviderType(id)) {
		return domain.LLMProvidersResponse{}, errNoSuchEndpoint(id)
	}
	if err := s.endpoints.Delete(ctx, id); err != nil {
		return domain.LLMProvidersResponse{}, errNoSuchEndpoint(id)
	}
	if active, err := s.store.GetActiveProvider(ctx); err == nil && string(active) == id {
		if err := s.store.SetActiveProvider(ctx, domain.LLMProviderLocal); err != nil {
			return domain.LLMProvidersResponse{}, err
		}
	}
	if embedding, err := s.store.GetEmbeddingProvider(ctx); err == nil && string(embedding) == id {
		if err := s.store.SetEmbeddingProvider(ctx, ""); err != nil {
			return domain.LLMProvidersResponse{}, err
		}
	}
	if err := s.reloadAllConfigured(ctx); err != nil {
		return domain.LLMProvidersResponse{}, err
	}
	return s.List(ctx)
}

// TestEndpoint connection-tests an endpoint. id may be empty (test before create);
// when set, stored values fill any omitted field and the stored key is reused.
func (s *Service) TestEndpoint(ctx context.Context, id string, req domain.SaveLLMEndpointRequest) error {
	baseURL := strings.TrimSpace(req.BaseURL)
	defaultModel := strings.TrimSpace(req.DefaultModel)
	apiKey := strings.TrimSpace(req.APIKey)
	timeoutSeconds := req.TimeoutSeconds
	ref := endpointProviderRef
	if id != "" && s.endpoints != nil {
		if existing, err := s.endpoints.Get(ctx, id); err == nil {
			ref = domain.LLMProviderType(id)
			if baseURL == "" {
				baseURL = existing.BaseURL
			}
			if defaultModel == "" {
				defaultModel = existing.DefaultModel
			}
			if timeoutSeconds <= 0 {
				timeoutSeconds = existing.TimeoutSeconds
			}
			if apiKey == "" && existing.HasAPIKey {
				apiKey, _ = s.decryptEndpointAPIKey(ctx, id)
			}
		}
	}
	if baseURL == "" {
		return fmt.Errorf("base_url is required")
	}
	timeoutSeconds, err := normalizeEndpointTimeout(timeoutSeconds)
	if err != nil {
		return err
	}
	return s.testClient(ref, baseURL, defaultModel, apiKey, timeoutSeconds)
}

// openAICompatibleModels lists an endpoint's models via GET {base}/models. Used
// as a fallback when the LM Studio native catalog is unavailable.
func openAICompatibleModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	url := strings.TrimRight(baseURL, "/") + "/models"
	reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, providerErrorBodyLimit))
		return nil, newProviderError("/models", resp.StatusCode, providerErrorMessage(body))
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		out = append(out, m.ID)
	}
	return out, nil
}
