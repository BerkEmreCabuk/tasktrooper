package antigravity

import (
	"context"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const ModelsTimeout = core.ModelsTimeout

func Models(ctx context.Context, binary string) ([]domain.LLMModelOption, error) {
	resolved, err := ResolveBinary(binary)
	if err != nil {
		return nil, fmt.Errorf("%s. Install the Antigravity CLI on this machine, or set ANTIGRAVITY_BIN to its path: %w",
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
	for _, line := range strings.Split(out, "\n") {
		id, label, ok := strings.Cut(strings.TrimSpace(line), "\t")
		id = strings.TrimSpace(id)
		if !ok || id == "" {
			continue
		}
		opts = append(opts, domain.LLMModelOption{ID: id, Label: strings.TrimSpace(label)})
	}
	return opts
}