package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/application/llmretry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

const maxRetries = 2

const minShrinkTokens = 2000

const toolOutputTruncateSuffix = "\n\n[…middle of the output omitted; the end, where the error is, follows…]"

type Loop struct {
	llm                port.LLMClient
	registry           port.ToolRegistry
	maxIterations      int
	taskMaxIterations  int
	maxToolOutputChars int
	historyBudget      appcontext.Budget
	summarizer         appcontext.Summarizer
	runTokenCap        int
	screenshots        ScreenshotArchiver
}

type runHead struct {
	len        int
	model      string
	provider   domain.LLMProviderType
	lightModel string
	effort     string
}

func (h runHead) anchor(n int) int {
	if h.len < 0 {
		return 0
	}
	if h.len > n {
		return n
	}
	return h.len
}

type RunOption func(*runOptions)

type runOptions struct {
	lightModel       string
	maxTurns         int
	effort           string
	cliLabel         string
	cliTitle         string
	scratchWorkspace bool
}

func applyRunOptions(opts []RunOption) runOptions {
	var cfg runOptions
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

func WithCLILabel(label, title string) RunOption {
	return func(o *runOptions) { o.cliLabel, o.cliTitle = label, title }
}

func WithScratchWorkspace() RunOption {
	return func(o *runOptions) { o.scratchWorkspace = true }
}

func WithLightModel(model string) RunOption {
	return func(o *runOptions) { o.lightModel = model }
}

func WithSessionLimits(maxTurns int, effort string) RunOption {
	return func(o *runOptions) { o.maxTurns, o.effort = maxTurns, effort }
}

func newRunHead(historyLen int, model string, provider domain.LLMProviderType, opts ...RunOption) runHead {
	cfg := applyRunOptions(opts)
	lightModel := cfg.lightModel
	if lightModel == "" {
		lightModel = model
	}
	return runHead{len: historyLen, model: model, provider: provider, lightModel: lightModel, effort: cfg.effort}
}

func NewLoop(llm port.LLMClient, registry port.ToolRegistry, maxIterations, taskMaxIterations, maxToolOutputChars int) *Loop {
	if maxToolOutputChars <= 0 {
		maxToolOutputChars = 16000
	}
	if maxIterations <= 0 {
		maxIterations = 30
	}
	if taskMaxIterations <= 0 {
		taskMaxIterations = maxIterations
	}
	return &Loop{
		llm:                llm,
		registry:           registry,
		maxIterations:      maxIterations,
		taskMaxIterations:  taskMaxIterations,
		maxToolOutputChars: maxToolOutputChars,
	}
}

func (l *Loop) SetHistoryBudget(budget appcontext.Budget) {
	l.historyBudget = budget
}

func (l *Loop) SetSummarizer(s appcontext.Summarizer) {
	l.summarizer = s
}

func (l *Loop) SetRunTokenCap(tokens int) {
	l.runTokenCap = tokens
}

func iterationBudget(configured int, opts ...RunOption) int {
	if cfg := applyRunOptions(opts); cfg.maxTurns > 0 {
		return cfg.maxTurns
	}
	return configured
}

func (l *Loop) Run(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, opts ...RunOption) (domain.AgentResponse, error) {
	return l.run(ctx, messages, model, provider, policy, iterationBudget(l.maxIterations, opts...), opts...)
}

func (l *Loop) RunTask(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, opts ...RunOption) (domain.AgentResponse, error) {
	return l.run(ctx, messages, model, provider, policy, iterationBudget(l.taskMaxIterations, opts...), opts...)
}

func (l *Loop) run(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, budget int, opts ...RunOption) (domain.AgentResponse, error) {
	if err := guardHostExecuted(provider); err != nil {
		return domain.AgentResponse{}, err
	}
	if budget <= 0 {
		budget = 1
	}
	tools := l.registry.DefinitionsForPolicy(policy)

	toolTokens := appcontext.CountToolTokens(tools)

	history := make([]domain.Message, len(messages))
	copy(history, messages)

	head := newRunHead(len(history), model, provider, opts...)

	tracker := newCallTracker()
	gate := newClarificationGate(tools)
	warned := false
	tokenWarned := false
	emptyTurns := 0
	totalTokens := 0
	calib := newTokenCalibration()

	for i := range budget {
		if err := ctx.Err(); err != nil {
			return domain.AgentResponse{}, err
		}

		if remaining := budget - i; remaining <= budgetWarningTurns && !warned {
			warned = true
			history = append(history, domain.Message{Role: domain.RoleSystem, Content: budgetWarningMessage(remaining)})
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("budget_warning", map[string]int{"turns_left": remaining})
			}
		}

		if l.runTokenCap > 0 && !tokenWarned && totalTokens >= runTokenWarnThreshold(l.runTokenCap) {
			tokenWarned = true
			history = append(history, domain.Message{Role: domain.RoleSystem, Content: tokenBudgetWarningMessage(totalTokens, l.runTokenCap)})
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("token_budget_warning", map[string]int{"total_tokens": totalTokens, "cap": l.runTokenCap})
			}
		}

		history = l.fitHistory(ctx, history, head, effectiveBudget(l.historyBudget, toolTokens, calib.current()))

		log.Debug().Int("iteration", i+1).Int("messages", len(history)).Msg("agent loop iteration")

		if rec := activity.FromContext(ctx); rec != nil {
			rec.Step("iteration_start", map[string]int{"iteration": i + 1, "message_count": len(history)})
		}

		req := domain.AgentRequest{
			Messages:         history,
			Tools:            tools,
			ProviderType:     provider,
			Model:            model,
			ToolPolicy:       policy,
			CacheAnchorIndex: head.anchor(len(history)),
			Effort:           head.effort,
			MaxTokens:        l.historyBudget.ReserveOutput,
		}

		if rec := activity.FromContext(ctx); rec != nil {
			rec.Step("llm_request", buildLLMRequestPayload(model, history, len(tools)))
		}

		resp, sent, err := l.chatWithRetry(ctx, req)
		history = sent
		if err != nil {
			return domain.AgentResponse{}, &ChatFailedError{Stats: tracker.snapshot(i + 1), Err: err}
		}

		totalTokens += resp.Usage.PromptTokens + resp.Usage.CompletionTokens

		calib.observe(resp.Usage.PromptTokens, appcontext.CountTokens(history)+toolTokens)

		if len(resp.Message.ToolCalls) == 0 {
			if strings.TrimSpace(resp.Message.Content) == "" {
				emptyTurns++
				if emptyTurns <= 1 {
					log.Warn().Int("iteration", i+1).Msg("model returned an empty turn; asking for the final answer")
					if rec := activity.FromContext(ctx); rec != nil {
						rec.Step("empty_turn_retry", map[string]int{"iteration": i + 1})
					}
					history = append(history, domain.Message{Role: domain.RoleSystem, Content: emptyTurnPrompt})
					continue
				}
				resp.Message.Content = emptyTurnFallback
			}
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("assistant_message", map[string]string{"content": resp.Message.Content})
			}
			log.Debug().Int("iterations", i+1).Msg("agent loop complete")
			return resp, nil
		}
		emptyTurns = 0

		if rec := activity.FromContext(ctx); rec != nil {
			rec.Step("tool_calls_planned", map[string]any{
				"content":    resp.Message.Content,
				"tool_calls": toolCallPayloads(resp.Message.ToolCalls),
			})
		}

		history = append(history, resp.Message)

		updated, clarification, resourceBlock, stuck, deadEnd := l.runToolCalls(ctx, history, resp.Message.ToolCalls, policy, tracker, gate)
		history = updated

		if err := ctx.Err(); err != nil {
			return domain.AgentResponse{}, err
		}
		if clarification != nil {
			return domain.AgentResponse{Clarification: clarification}, nil
		}
		if resourceBlock != nil {
			return domain.AgentResponse{ResourceBlock: resourceBlock}, nil
		}
		if stuck || deadEnd {
			return l.giveUp(ctx, history, head, tracker.snapshot(i+1), budget, giveUpCause{Stuck: stuck, DeadEnd: deadEnd})
		}

		if l.runTokenCap > 0 && totalTokens >= l.runTokenCap {
			log.Warn().
				Int("total_tokens", totalTokens).
				Int("cap", l.runTokenCap).
				Int("iteration", i+1).
				Msg("agent loop stopped: run token budget exhausted")
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("token_budget_exhausted", map[string]int{"total_tokens": totalTokens, "cap": l.runTokenCap, "iteration": i + 1})
			}
			return l.giveUp(ctx, history, head, tracker.snapshot(i+1), budget, giveUpCause{TokenExhausted: true, TokensUsed: totalTokens})
		}
	}

	return l.giveUp(ctx, history, head, tracker.snapshot(budget), budget, giveUpCause{})
}

func guardHostExecuted(provider domain.LLMProviderType) error {
	if domain.RequiresHostExecutor(provider) {
		return domain.ErrHostExecutedProvider(provider)
	}
	return nil
}

func (l *Loop) RunStream(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, onToken func(string), opts ...RunOption) (domain.AgentResponse, error) {
	if err := guardHostExecuted(provider); err != nil {
		return domain.AgentResponse{}, err
	}
	tools := l.registry.DefinitionsForPolicy(policy)
	toolTokens := appcontext.CountToolTokens(tools)
	history := make([]domain.Message, len(messages))
	copy(history, messages)
	head := newRunHead(len(history), model, provider, opts...)

	tracker := newCallTracker()
	gate := newClarificationGate(tools)
	warned := false
	tokenWarned := false
	emptyTurns := 0
	budget := l.maxIterations
	totalTokens := 0

	calib := newTokenCalibration()

	for i := range budget {
		if err := ctx.Err(); err != nil {
			return domain.AgentResponse{}, err
		}

		if remaining := budget - i; remaining <= budgetWarningTurns && !warned {
			warned = true
			history = append(history, domain.Message{Role: domain.RoleSystem, Content: budgetWarningMessage(remaining)})
		}

		if l.runTokenCap > 0 && !tokenWarned && totalTokens >= runTokenWarnThreshold(l.runTokenCap) {
			tokenWarned = true
			history = append(history, domain.Message{Role: domain.RoleSystem, Content: tokenBudgetWarningMessage(totalTokens, l.runTokenCap)})
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("token_budget_warning", map[string]int{"total_tokens": totalTokens, "cap": l.runTokenCap})
			}
		}

		history = l.fitHistory(ctx, history, head, effectiveBudget(l.historyBudget, toolTokens, calib.current()))

		log.Debug().Int("iteration", i+1).Msg("stream agent loop iteration")

		if rec := activity.FromContext(ctx); rec != nil {
			rec.Step("iteration_start", map[string]int{"iteration": i + 1, "message_count": len(history)})
		}

		req := domain.AgentRequest{
			Messages:         history,
			Tools:            tools,
			ProviderType:     provider,
			Model:            model,
			ToolPolicy:       policy,
			CacheAnchorIndex: head.anchor(len(history)),
			Effort:           head.effort,
			MaxTokens:        l.historyBudget.ReserveOutput,
		}

		if rec := activity.FromContext(ctx); rec != nil {
			rec.Step("llm_request", buildLLMRequestPayload(model, history, len(tools)))
		}

		resp, sent, err := l.chatStreamWithRetry(ctx, req, onToken)
		history = sent
		if err != nil {
			return domain.AgentResponse{}, &ChatFailedError{Stats: tracker.snapshot(i + 1), Err: err}
		}
		totalTokens += resp.Usage.PromptTokens + resp.Usage.CompletionTokens

		calib.observe(resp.Usage.PromptTokens, appcontext.CountTokens(history)+toolTokens)

		if len(resp.Message.ToolCalls) == 0 {
			if strings.TrimSpace(resp.Message.Content) == "" {
				emptyTurns++
				if emptyTurns <= 1 {
					log.Warn().Int("iteration", i+1).Msg("model returned an empty turn; asking for the final answer")
					history = append(history, domain.Message{Role: domain.RoleSystem, Content: emptyTurnPrompt})
					continue
				}
			}
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("assistant_message", map[string]string{"content": resp.Message.Content})
			}
			return resp, nil
		}
		emptyTurns = 0

		segmentBreak(ctx)

		if rec := activity.FromContext(ctx); rec != nil {

			rec.Step("tool_calls_planned", map[string]any{
				"content":    resp.Message.Content,
				"tool_calls": toolCallPayloads(resp.Message.ToolCalls),
			})
		}

		history = append(history, resp.Message)

		updated, clarification, resourceBlock, stuck, deadEnd := l.runToolCalls(ctx, history, resp.Message.ToolCalls, policy, tracker, gate)
		history = updated
		if err := ctx.Err(); err != nil {
			return domain.AgentResponse{}, err
		}
		if clarification != nil {
			return domain.AgentResponse{Clarification: clarification}, nil
		}
		if resourceBlock != nil {
			return domain.AgentResponse{ResourceBlock: resourceBlock}, nil
		}
		if stuck || deadEnd {
			return l.giveUp(ctx, history, head, tracker.snapshot(i+1), budget, giveUpCause{Stuck: stuck, DeadEnd: deadEnd})
		}
		if l.runTokenCap > 0 && totalTokens >= l.runTokenCap {
			log.Warn().
				Int("total_tokens", totalTokens).
				Int("cap", l.runTokenCap).
				Int("iteration", i+1).
				Msg("agent loop stopped: run token budget exhausted")
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("token_budget_exhausted", map[string]int{"total_tokens": totalTokens, "cap": l.runTokenCap, "iteration": i + 1})
			}
			return l.giveUp(ctx, history, head, tracker.snapshot(i+1), budget, giveUpCause{TokenExhausted: true, TokensUsed: totalTokens})
		}
	}

	return l.giveUp(ctx, history, head, tracker.snapshot(budget), budget, giveUpCause{})
}

func (l *Loop) runToolCalls(
	ctx context.Context,
	history []domain.Message,
	calls []domain.ToolCall,
	policy domain.ToolPolicy,
	tracker *callTracker,
	gate *clarificationGate,
) ([]domain.Message, *domain.ClarificationRequest, *domain.ResourceBlock, bool, bool) {
	stuck := false
	deadEnd := false

	for _, tc := range calls {
		if ctx.Err() != nil {
			return history, nil, nil, stuck, deadEnd
		}

		if tracker.canSkip(tc.Function.Name, tc.Function.Arguments) {
			out := tracker.observeSkipped(tc.Function.Name, tc.Function.Arguments)
			log.Warn().
				Str("tool", tc.Function.Name).
				Int("repeats", out.Repeats+1).
				Msg("loop guard: identical back-to-back call answered without executing")
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("tool_call_skipped", map[string]any{
					"tool": tc.Function.Name, "call_id": tc.ID,
					"arguments": tc.Function.Arguments, "repeats": out.Repeats + 1,
				})
			}
			history = append(history, domain.Message{
				Role:       domain.RoleTool,
				Content:    repeatNudgeMessage(tc.Function.Name, out.Repeats),
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
			})
			if out.Repeats >= repeatAbortThreshold {
				stuck = true
			}
			continue
		}

		log.Debug().
			Str("tool", tc.Function.Name).
			Str("call_id", tc.ID).
			Msg("executing tool call")

		if rec := activity.FromContext(ctx); rec != nil {
			rec.Step("tool_call_start", map[string]string{
				"tool": tc.Function.Name, "call_id": tc.ID, "arguments": tc.Function.Arguments,
			})
		}

		result := l.registry.ExecuteWithPolicy(ctx, tc, policy)

		if result.Clarification != nil {

			if gate.refuse(ctx) {
				log.Warn().Str("tool", tc.Function.Name).Msg("clarification refused: run has not read the repository yet")
				if rec := activity.FromContext(ctx); rec != nil {
					rec.Step("clarification_refused", map[string]string{
						"reason": "no_code_exploration", "context": result.Clarification.Context,
					})
				}
				history = append(history, domain.Message{
					Role:       domain.RoleTool,
					Content:    groundClarificationNote,
					ToolCallID: tc.ID,
					Name:       tc.Function.Name,
				})
				continue
			}
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("clarification_requested", domain.ClarificationStepPayload(*result.Clarification, "ask_user"))
			}
			reqCopy := *result.Clarification
			return history, &reqCopy, nil, false, false
		}

		if result.ResourceBlock != nil {
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("resource_blocked", map[string]string{
					"tool": tc.Function.Name, "resource": result.ResourceBlock.Resource,
					"detail": result.ResourceBlock.Detail,
				})
			}
			blockCopy := *result.ResourceBlock
			return history, nil, &blockCopy, false, false
		}

		out := tracker.observe(tc.Function.Name, tc.Function.Arguments, result.Content, result.IsError)

		if rec := activity.FromContext(ctx); rec != nil {
			preview := result.Content
			if len(preview) > 500 {
				preview = domain.TruncateHead(preview, 500)
			}

			imageIDs := l.archiveImages(ctx, result.Name, result.Images)
			rec.Step("tool_call_result", map[string]any{
				"tool": result.Name, "call_id": result.ToolCallID,
				"image_attachment_ids": imageIDs,
				"content":              preview, "is_error": result.IsError, "repeats": out.Repeats,
				"error_streak": out.ErrStreak, "tool_errors": out.ToolErrors,
				"executions": out.Execs,
			})
		}

		content := truncateToolOutput(result.Content, l.maxToolOutputChars)
		images := result.Images

		if strings.TrimSpace(content) == "" && len(images) == 0 {
			content = emptyResultNote(tc.Function.Name)
		}
		switch {
		case out.Repeats >= repeatNoteThreshold:
			content = repeatNudgeMessage(tc.Function.Name, out.Repeats)

			images = nil
			log.Warn().
				Str("tool", tc.Function.Name).
				Int("repeats", out.Repeats+1).
				Msg("loop guard: identical tool call and result repeated")
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("loop_guard_repeat", map[string]any{
					"tool": tc.Function.Name, "repeats": out.Repeats + 1, "arguments": tc.Function.Arguments,
				})
			}
		case out.ErrStreak >= errStreakNoteThreshold:

			content = errorStreakMessage(out.ErrStreak) + content
			log.Warn().
				Str("tool", tc.Function.Name).
				Int("error_streak", out.ErrStreak).
				Msg("loop guard: consecutive tool failures")
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("loop_guard_error_streak", map[string]any{
					"tool": tc.Function.Name, "error_streak": out.ErrStreak,
				})
			}
		}

		if out.Repeats == 0 && out.Execs >= sameCallNoteThreshold {
			content += sameCallMessage(tc.Function.Name, out.Execs)
			log.Warn().
				Str("tool", tc.Function.Name).
				Int("executions", out.Execs).
				Msg("loop guard: identical call executed repeatedly with a changing result")
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("loop_guard_same_call", map[string]any{
					"tool": tc.Function.Name, "executions": out.Execs, "arguments": tc.Function.Arguments,
				})
			}
		}

		if result.IsError && out.ToolErrors >= toolErrorNoteThreshold {
			content += toolErrorMessage(tc.Function.Name, out.ToolErrors)
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("loop_guard_tool_errors", map[string]any{
					"tool": tc.Function.Name, "tool_errors": out.ToolErrors,
				})
			}
		}

		if out.Repeats >= repeatAbortThreshold {
			stuck = true
		}

		if out.Execs >= sameCallAbortThreshold {
			log.Warn().
				Str("tool", tc.Function.Name).
				Int("executions", out.Execs).
				Msg("loop guard: identical call executed past the abort threshold")
			stuck = true
		}
		if out.ErrStreak >= errStreakAbortThreshold {
			deadEnd = true
		}

		history = append(history, domain.Message{
			Role:       domain.RoleTool,
			Content:    content,
			ToolCallID: result.ToolCallID,
			Name:       result.Name,
			Images:     images,
		})
	}

	return history, nil, nil, stuck, deadEnd
}

func (l *Loop) giveUp(
	ctx context.Context,
	history []domain.Message,
	head runHead,
	stats RunStats,
	budget int,
	cause giveUpCause,
) (domain.AgentResponse, error) {
	partial := l.wrapUp(ctx, history, head)

	err := &BudgetExhaustedError{
		Budget: budget, Stats: stats, Partial: partial,
		Stuck: cause.Stuck, DeadEnd: cause.DeadEnd,
		TokenExhausted: cause.TokenExhausted, TokensUsed: cause.TokensUsed,
	}

	log.Warn().
		Int("budget", budget).
		Int("iterations", stats.Iterations).
		Int("tool_calls", stats.ToolCalls).
		Int("repeated_no_progress", stats.RepeatedNoProgress).
		Int("tool_errors", stats.ToolErrors).
		Int("max_error_streak", stats.MaxErrorStreak).
		Bool("stuck", cause.Stuck).
		Bool("dead_end", cause.DeadEnd).
		Bool("token_exhausted", cause.TokenExhausted).
		Int("tokens_used", cause.TokensUsed).
		Str("stats", stats.Summary()).
		Msg("agent loop out of budget")

	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("budget_exhausted", map[string]any{
			"budget": budget, "stuck": cause.Stuck, "dead_end": cause.DeadEnd,
			"token_exhausted": cause.TokenExhausted, "tokens_used": cause.TokensUsed,
			"stats": stats.Summary(), "summary": partial,
		})
	}

	return domain.AgentResponse{}, err
}

func effectiveBudget(base appcontext.Budget, toolTokens int, ratio float64) appcontext.Budget {
	if base.MaxTokens <= 0 {
		return base
	}
	if ratio <= 0 {
		ratio = 1
	}
	shrunk := max(int(float64(base.MaxTokens-toolTokens)/ratio), minShrinkTokens)
	adjusted := base
	adjusted.MaxTokens = min(base.MaxTokens, shrunk)
	return adjusted
}

func (l *Loop) fitHistory(ctx context.Context, history []domain.Message, head runHead, budget appcontext.Budget) []domain.Message {
	if budget.MaxTokens <= 0 {
		return history
	}
	if appcontext.CountTokens(history) <= budget.TokenLimit() {
		return history
	}
	if summarized, ok := l.summarizeHistory(ctx, history, head, budget); ok {
		history = summarized
		if appcontext.CountTokens(history) <= budget.TokenLimit() {
			return history
		}
	}
	trimmed := budget.Apply(history)
	dropped := len(history) - len(trimmed)
	if dropped <= 0 {
		return history
	}
	log.Info().
		Int("dropped_messages", dropped).
		Int("kept_messages", len(trimmed)).
		Int("tokens", appcontext.CountTokens(trimmed)).
		Msg("agent loop trimmed history to the context budget")
	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("history_trimmed", map[string]int{
			"dropped_messages": dropped,
			"kept_messages":    len(trimmed),
			"kept_tokens":      appcontext.CountTokens(trimmed),
		})
	}
	return trimmed
}

func (l *Loop) summarizeHistory(ctx context.Context, history []domain.Message, head runHead, budget appcontext.Budget) ([]domain.Message, bool) {
	if l.summarizer == nil {
		return nil, false
	}
	summarized, ok, err := appcontext.StableTrim(ctx, budget, l.summarizer, history, appcontext.StableTrimOptions{
		HeadLen:  head.len,
		Model:    head.lightModel,
		Provider: head.provider,
	})
	if err != nil {
		log.Warn().Err(err).
			Str("provider", string(head.provider)).
			Bool("permanent", errors.Is(err, domain.ErrHostExecutedUnservable)).
			Msg("agent loop history summary skipped; falling back to dropping the oldest messages")
		return nil, false
	}
	if !ok {
		return nil, false
	}
	dropped := len(history) - len(summarized)
	log.Info().
		Int("dropped_messages", dropped).
		Int("kept_messages", len(summarized)).
		Int("tokens", appcontext.CountTokens(summarized)).
		Msg("agent loop summarised the middle of its history")
	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("history_summarized", map[string]int{
			"dropped_messages": dropped,
			"kept_messages":    len(summarized),
			"kept_tokens":      appcontext.CountTokens(summarized),
		})
	}
	return summarized, true
}

func (l *Loop) shrinkHistory(messages []domain.Message) ([]domain.Message, bool) {
	budget := l.historyBudget
	budget.ReserveOutput = 0
	budget.MaxTokens = max(appcontext.CountTokens(messages)/2, minShrinkTokens)
	if budget.KeepRecentMessages <= 0 {
		budget.KeepRecentMessages = 6
	}
	trimmed := budget.Apply(messages)
	return trimmed, len(trimmed) < len(messages)
}

func (l *Loop) chatWithRetry(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, []domain.Message, error) {
	return l.sendWithRetry(ctx, req, l.llm.Chat)
}

func (l *Loop) chatStreamWithRetry(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, []domain.Message, error) {
	return l.sendWithRetry(ctx, req, func(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
		return l.llm.ChatStream(ctx, req, onToken)
	})
}

func (l *Loop) sendWithRetry(
	ctx context.Context,
	req domain.AgentRequest,
	send func(context.Context, domain.AgentRequest) (domain.AgentResponse, error),
) (domain.AgentResponse, []domain.Message, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {

		if err := ctx.Err(); err != nil {
			return domain.AgentResponse{}, req.Messages, err
		}

		resp, err := send(ctx, req)
		if err == nil {
			return resp, req.Messages, nil
		}
		lastErr = err

		action := llmretry.Classify(ctx, err)
		log.Warn().Err(err).
			Int("attempt", attempt+1).
			Str("action", action.String()).
			Msg("llm chat attempt failed")

		if attempt == maxRetries {
			break
		}

		switch action {
		case llmretry.Stop:
			return domain.AgentResponse{}, req.Messages, retryExhausted(attempt+1, lastErr)
		case llmretry.Shrink:
			shrunk, ok := l.shrinkHistory(req.Messages)
			if !ok {
				return domain.AgentResponse{}, req.Messages, retryExhausted(attempt+1, lastErr)
			}
			log.Warn().
				Int("dropped_messages", len(req.Messages)-len(shrunk)).
				Msg("provider refused the request as too long; retrying it smaller")
			req.Messages = shrunk
		case llmretry.Backoff:
			if sleepErr := llmretry.Wait(ctx, err, attempt); sleepErr != nil {
				return domain.AgentResponse{}, req.Messages, retryExhausted(attempt+1, lastErr)
			}
		}
	}

	return domain.AgentResponse{}, req.Messages, retryExhausted(maxRetries+1, lastErr)
}

func retryExhausted(attempts int, err error) error {
	if attempts == 1 {
		return fmt.Errorf("llm failed after 1 attempt: %w", err)
	}
	return fmt.Errorf("llm failed after %d attempts: %w", attempts, err)
}

func (l *Loop) wrapUp(ctx context.Context, history []domain.Message, head runHead) string {
	messages := make([]domain.Message, len(history), len(history)+1)
	copy(messages, history)
	messages = append(messages, domain.Message{Role: domain.RoleSystem, Content: wrapUpPrompt})

	messages = l.fitHistory(ctx, messages, head, l.historyBudget)

	resp, _, err := l.chatWithRetry(ctx, domain.AgentRequest{
		Messages:     messages,
		ProviderType: head.provider,

		Model:            head.lightModel,
		CacheAnchorIndex: head.anchor(len(messages)),
		MaxTokens:        l.historyBudget.ReserveOutput,
	})
	if err != nil {
		log.Warn().Err(err).
			Str("provider", string(head.provider)).
			Bool("permanent", errors.Is(err, domain.ErrHostExecutedUnservable)).
			Msg("agent loop wrap-up summary skipped; returning the run's own last message")
		return ""
	}
	return strings.TrimSpace(resp.Message.Content)
}

func truncateToolOutput(content string, maxChars int) string {
	if maxChars <= 0 || len(content) <= maxChars {
		return content
	}
	head := domain.TruncateHead(content, maxChars/4)
	tail := domain.TruncateTail(content, maxChars-len(head))
	return head + toolOutputTruncateSuffix + "\n" + tail
}

func toolCallPayloads(calls []domain.ToolCall) []map[string]string {
	payloads := make([]map[string]string, 0, len(calls))
	for _, tc := range calls {
		payloads = append(payloads, map[string]string{
			"id": tc.ID, "name": tc.Function.Name, "arguments": tc.Function.Arguments,
		})
	}
	return payloads
}

const previewLimit = 300

func buildLLMRequestPayload(model string, history []domain.Message, toolCount int) map[string]any {
	type callPreview struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	type msgPreview struct {
		Role      string        `json:"role"`
		Content   string        `json:"content"`
		ToolCalls []callPreview `json:"tool_calls,omitempty"`
	}
	previews := make([]msgPreview, 0, len(history))
	for _, m := range history {
		content := domain.TruncateHead(m.Content, previewLimit)
		if len(content) < len(m.Content) {
			content += "…"
		}
		if m.Role == domain.RoleSystem && content == "" {
			continue
		}
		role := string(m.Role)
		if m.Role == domain.RoleTool {
			role = "tool:" + m.Name
		}
		calls := make([]callPreview, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			args := domain.TruncateHead(tc.Function.Arguments, previewLimit)
			if len(args) < len(tc.Function.Arguments) {
				args += "…"
			}
			calls = append(calls, callPreview{Name: tc.Function.Name, Arguments: args})
		}
		if len(calls) == 0 {
			calls = nil
		}
		previews = append(previews, msgPreview{Role: role, Content: content, ToolCalls: calls})
	}
	return map[string]any{
		"model":         model,
		"message_count": len(history),
		"tool_count":    toolCount,
		"messages":      previews,
	}
}
