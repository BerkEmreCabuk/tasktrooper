package orchestrator

import (
	"context"

	"github.com/google/uuid"
	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/application/indexer"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ContextBuilder struct {
	injector   *indexer.Injector
	budget     appcontext.Budget
	indexerCfg domain.IndexerConfig
	mappingCfg domain.MappingConfig
}

func NewContextBuilder(injector *indexer.Injector, budget appcontext.Budget, indexerCfg domain.IndexerConfig, mappingCfg domain.MappingConfig) *ContextBuilder {
	return &ContextBuilder{
		injector:   injector,
		budget:     budget,
		indexerCfg: indexerCfg,
		mappingCfg: mappingCfg,
	}
}

func (b *ContextBuilder) BuildPlannerContext(ctx context.Context, sessionID uuid.UUID, userMessage string) ([]domain.Message, error) {
	if b.injector == nil || sessionID == uuid.Nil {
		return nil, nil
	}
	msgs := []domain.Message{{Role: domain.RoleUser, Content: userMessage}}
	opts := domain.InjectOptions{
		TopK:            3,
		IncludeTree:     true,
		IncludeSkeleton: b.mappingCfg.Enabled,
		MaxChunkTokens:  3000,
	}
	injected, err := b.injector.InjectContext(ctx, sessionID, msgs, opts)
	if err != nil {
		return nil, err
	}
	return b.budget.Apply(injected), nil
}

func (b *ContextBuilder) BuildExplorerContext(ctx context.Context, sessionID uuid.UUID, taskDescription string) ([]domain.Message, error) {
	if b.injector == nil || sessionID == uuid.Nil {
		return nil, nil
	}
	msgs := []domain.Message{{Role: domain.RoleUser, Content: taskDescription}}
	topK := b.indexerCfg.TopK
	if topK <= 0 {
		topK = 5
	}
	opts := domain.InjectOptions{
		TopK:            topK,
		IncludeTree:     true,
		IncludeSkeleton: true,
		MaxChunkTokens:  6000,
	}
	injected, err := b.injector.InjectContext(ctx, sessionID, msgs, opts)
	if err != nil {
		return nil, err
	}
	return b.budget.Apply(injected), nil
}

func (b *ContextBuilder) FormatExplorerFindings(taskKey, title, result string) string {
	return "## Explorer findings: " + taskKey + " — " + title + "\n\n" + result
}
