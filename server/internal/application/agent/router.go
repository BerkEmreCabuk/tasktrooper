package agent

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type Runner interface {
	Run(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, opts ...RunOption) (domain.AgentResponse, error)
	RunTask(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, opts ...RunOption) (domain.AgentResponse, error)
	RunStream(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, onToken func(string), opts ...RunOption) (domain.AgentResponse, error)
}

var (
	_ Runner = (*Loop)(nil)
	_ Runner = (*Router)(nil)
)

type Router struct {
	loop     Runner
	mu       sync.RWMutex
	executor port.TaskExecutor
}

func NewRouter(loop Runner) *Router {
	return &Router{loop: loop}
}

func (r *Router) SetTaskExecutor(ex port.TaskExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executor = ex
}

func (r *Router) taskExecutor() port.TaskExecutor {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.executor
}

func (r *Router) SupportsHostExecution(provider domain.LLMProviderType) bool {
	ex := r.taskExecutor()
	return ex != nil && ex.Supports(provider)
}

func (r *Router) Run(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, opts ...RunOption) (domain.AgentResponse, error) {
	if !domain.RequiresHostExecutor(provider) {
		return r.loop.Run(ctx, messages, model, provider, policy, opts...)
	}
	return r.execute(ctx, messages, model, provider, policy, opts)
}

func (r *Router) RunTask(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, opts ...RunOption) (domain.AgentResponse, error) {
	if !domain.RequiresHostExecutor(provider) {
		return r.loop.RunTask(ctx, messages, model, provider, policy, opts...)
	}

	return r.execute(ctx, messages, model, provider, policy, opts)
}

func (r *Router) RunStream(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, onToken func(string), opts ...RunOption) (domain.AgentResponse, error) {
	return r.loop.RunStream(ctx, messages, model, provider, policy, onToken, opts...)
}

// execute hands the run to the CLI executor.
func (r *Router) execute(
	ctx context.Context,
	messages []domain.Message,
	model string,
	provider domain.LLMProviderType,
	policy domain.ToolPolicy,
	opts []RunOption,
) (domain.AgentResponse, error) {
	ex := r.taskExecutor()
	if ex == nil || !ex.Supports(provider) {
		return domain.AgentResponse{}, domain.ErrHostExecutedProvider(provider)
	}

	cfg := applyRunOptions(opts)

	workDir := registry.EffectiveWorkspaceDir(ctx)
	if workDir == "" {
		if !cfg.scratchWorkspace {
			return domain.AgentResponse{}, fmt.Errorf(
				"this agent runs on %s, which is a process and needs a working directory, "+
					"but this run was started without one; scope the work to a repository and try again", provider)
		}
		dir, err := os.MkdirTemp("", "tt-cli-run-")
		if err != nil {
			return domain.AgentResponse{}, fmt.Errorf("create a scratch workspace for the %s session: %w", provider, err)
		}
		defer func() {
			if rmErr := os.RemoveAll(dir); rmErr != nil {
				log.Warn().Err(rmErr).Str("dir", dir).Msg("could not remove the cli session's scratch workspace")
			}
		}()
		workDir = dir
	}

	log.Debug().
		Str("provider", string(provider)).
		Str("model", model).
		Str("label", cfg.cliLabel).
		Str("work_dir", workDir).
		Msg("routing an agentic run to the host executor")

	return ex.Execute(ctx, domain.TaskExecution{
		History:   messages,
		Model:     model,
		Provider:  provider,
		MaxTurns:  cfg.maxTurns,
		Effort:    cfg.effort,
		Policy:    policy,
		WorkDir:   workDir,
		TaskKey:   cfg.cliLabel,
		TaskTitle: cfg.cliTitle,
	})
}
