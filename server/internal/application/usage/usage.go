// Package usage decorates the LLM client so every chat and embedding call's
// token usage is recorded, and identical embedding queries are served from
// an in-memory cache (see cache.go's CachingEmbedder). Recording is
// best-effort and asynchronous; it never delays or fails the underlying
// call.
package usage

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const recordTimeout = 5 * time.Second

type RecordingClient struct {
	inner port.LLMClient
	store port.LLMUsageStore
}

var _ port.LLMClient = (*RecordingClient)(nil)

func NewRecordingClient(inner port.LLMClient, store port.LLMUsageStore) *RecordingClient {
	return &RecordingClient{inner: inner, store: store}
}

// Unwrap exposes the wrapped client so callers can reach capabilities the
// port.LLMClient interface doesn't declare (e.g. per-provider model listing
// and health checks on the underlying MultiProviderClient).
func (r *RecordingClient) Unwrap() port.LLMClient { return r.inner }

func (r *RecordingClient) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	resp, err := r.inner.Chat(ctx, req)
	if err == nil {
		r.record(ctx, req.Model, resp.Usage)
		TokenUsageFromContext(ctx).Add(resp.Usage)
	}
	return resp, err
}

func (r *RecordingClient) ChatStream(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	resp, err := r.inner.ChatStream(ctx, req, onToken)
	if err == nil {
		r.record(ctx, req.Model, resp.Usage)
		TokenUsageFromContext(ctx).Add(resp.Usage)
	}
	return resp, err
}

func (r *RecordingClient) Models(ctx context.Context) ([]string, error) {
	return r.inner.Models(ctx)
}

// Embed records estimated prompt-token usage for the call. Unlike Chat,
// port.LLMClient.Embed returns only the vector — no usage struct — so there
// is no provider-reported count to pass through; CountTokens' chars-per-token
// heuristic (the same one the context budget uses) stands in.
//
// TODO(usage): openai_compat.go's embedResponse (POST /embeddings) does not parse
// a "usage" object. OpenAI-compatible embeddings endpoints return
// usage.prompt_tokens/usage.total_tokens in that same response; once
// port.LLMClient.Embed can surface it (openai_compat.go is out of scope for this
// change), prefer the provider's real count over this estimate.
func (r *RecordingClient) Embed(ctx context.Context, input, model string) ([]float32, error) {
	vec, err := r.inner.Embed(ctx, input, model)
	if err == nil {
		estimated := appcontext.CountTokens([]domain.Message{{Content: input}})
		r.record(ctx, model, domain.Usage{PromptTokens: estimated})
	}
	return vec, err
}

// record writes the call's tokens to llm_usage in the background.
//
// The write must outlive the request whose tokens it is recording
// (context.WithoutCancel) — it once ran on a bare context.Background(), so
// EVERY chat and embedding call's usage was dropped with one Warn line: no
// spend, no budget gate, no billing.
func (r *RecordingClient) record(ctx context.Context, model string, u domain.Usage) {
	if u.PromptTokens == 0 && u.CompletionTokens == 0 {
		return
	}
	if model == "" {
		model = "(default)"
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
		defer cancel()
		if err := r.store.Record(ctx, domain.LLMUsageRecord{
			Model:            model,
			PromptTokens:     u.PromptTokens,
			CompletionTokens: u.CompletionTokens,
			CacheReadTokens:  u.CacheReadTokens,
			CacheWriteTokens: u.CacheWriteTokens,
		}); err != nil {
			log.Warn().Err(err).Msg("llm usage record failed")
		}
	}()
}
