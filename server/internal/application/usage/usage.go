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

func (r *RecordingClient) Embed(ctx context.Context, input, model string) ([]float32, error) {
	vec, err := r.inner.Embed(ctx, input, model)
	if err == nil {
		estimated := appcontext.CountTokens([]domain.Message{{Content: input}})
		r.record(ctx, model, domain.Usage{PromptTokens: estimated})
	}
	return vec, err
}

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
