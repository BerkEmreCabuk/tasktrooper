package opencode

import (
	"context"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const ModelsTimeout = core.ModelsTimeout

func Models(ctx context.Context, binary string) ([]domain.LLMModelOption, error) {
	resolved, err := ResolveBinary(binary)
	if err != nil {
		return nil, fmt.Errorf("%s. Install the OpenCode CLI on this machine, or set OPENCODE_BIN to its path: %w",
			err.Error(), domain.ErrAgentCLIBinaryMissing)
	}

	out, errOut, err := core.RunModels(ctx, resolved)
	if err != nil {
		return nil, fmt.Errorf("%s models did not run (%v)%s", resolved, err, core.Tail(errOut))
	}
	return ParseModels(out), nil
}

func ParseModels(out string) []domain.LLMModelOption {
	var opts []domain.LLMModelOption
	for _, line := range core.ModelsLines(out) {
		opts = append(opts, domain.LLMModelOption{ID: line})
	}
	return opts
}