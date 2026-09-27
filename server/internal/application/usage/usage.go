package usage

import (
	"context"
	"time"

	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const recordTimeout = 5 * time.Second

type RecordingClient struct {
	inner port.LLMClient
	meter *Meter
}

var _ port.LLMClient = (*RecordingClient)(nil)

func NewRecordingClient(inner port.LLMClient, meter *Meter) *RecordingClient {
	return &RecordingClient{inner: inner, meter: meter}
}

func (r *RecordingClient) Unwrap() port.LLMClient { return r.inner }

func (r *RecordingClient) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	resp, err := r.inner.Chat(ctx, req)
	if err == nil {
		r.record(ctx, domain.LLMUsageKindAPI, req.Model, r.chatProvider(ctx, req), resp.Usage)
	}
	return resp, err
}

func (r *RecordingClient) ChatStream(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	resp, err := r.inner.ChatStream(ctx, req, onToken)
	if err == nil {
		r.record(ctx, domain.LLMUsageKindAPI, req.Model, r.chatProvider(ctx, req), resp.Usage)
	}
	return resp, err
}

func (r *RecordingClient) Models(ctx context.Context) ([]string, error) {
	return r.inner.Models(ctx)
}

// chatProvider prefers the request's own provider (set when a task/chat pins
// one) and otherwise resolves whichever provider the MultiProviderClient
// would actually route to, so a request left at its zero value still gets a
// real provider name in the usage table instead of "".
func (r *RecordingClient) chatProvider(ctx context.Context, req domain.AgentRequest) string {
	if req.ProviderType != "" {
		return string(req.ProviderType)
	}
	if d, ok := findInUnwrapChain(r.inner, func(cur port.LLMClient) (domain.LLMProviderType, bool) {
		if p, ok := cur.(interface {
			DefaultProvider(context.Context) domain.LLMProviderType
		}); ok {
			return p.DefaultProvider(ctx), true
		}
		return "", false
	}); ok {
		return string(d)
	}
	return ""
}

func (r *RecordingClient) Embed(ctx context.Context, input, model string) ([]float32, error) {
	vec, err := r.inner.Embed(ctx, input, model)
	if err == nil {
		estimated := appcontext.CountTokens([]domain.Message{{Content: input}})
		embedModel := model
		if embedModel == "" {
			embedModel = r.embeddingModel(ctx)
		}
		r.record(ctx, domain.LLMUsageKindEmbedding, embedModel, r.embeddingProvider(ctx), domain.Usage{PromptTokens: estimated})
	}
	return vec, err
}

func (r *RecordingClient) embeddingProvider(ctx context.Context) string {
	return embeddingProviderOf(ctx, r.inner)
}

func (r *RecordingClient) embeddingModel(ctx context.Context) string {
	return embeddingModelOf(ctx, r.inner)
}

func (r *RecordingClient) record(ctx context.Context, kind domain.LLMUsageKind, model, provider string, u domain.Usage) {
	r.meter.Record(ctx, domain.LLMUsageRecord{
		Kind:             kind,
		Provider:         provider,
		Model:            model,
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
	})
}
