package cursor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ModelsTimeout bounds `cursor-agent models`, a catalog read the CLI answers
// without starting a session.
const ModelsTimeout = 30 * time.Second

// Models asks the CLI for this account's model catalog.
//
// Unlike claude_code (see domain.ClaudeCodeModels, where no such subcommand
// exists), cursor-agent documents and answers this directly: `cursor-agent
// models` prints one "id - Label" line per model under an "Available models"
// header, so the picker can be a live query instead of a curated guess.
func Models(ctx context.Context, binary string) ([]domain.LLMModelOption, error) {
	resolved, err := ResolveBinary(binary)
	if err != nil {
		return nil, fmt.Errorf("%s. Install the Cursor CLI on this machine, or set CURSOR_AGENT_BIN to its path: %w",
			err.Error(), domain.ErrAgentCLIBinaryMissing)
	}

	ctx, cancel := context.WithTimeout(ctx, ModelsTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, resolved, "models")
	cmd.Env = childEnv(ctx)
	cmd.Dir = os.TempDir()
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s models did not run (%v)%s", resolved, err, tail(errOut.String()))
	}
	return ParseModels(out.String()), nil
}

// ParseModels reads cursor-agent's own listing format. Every line that is not
// "id - Label" (the "Available models" header, the blank line under it) has
// no " - " separator and is skipped rather than misread.
//
// Exported so the remote path (agent-server/internal/adapter/agentcli/
// claudecode.Preflight.ModelsFor) can parse the raw stdout a Mac's runner
// returns over the tunnel without a second implementation of this format in
// another program.
func ParseModels(out string) []domain.LLMModelOption {
	var opts []domain.LLMModelOption
	for _, line := range strings.Split(out, "\n") {
		id, label, ok := strings.Cut(strings.TrimSpace(line), " - ")
		id = strings.TrimSpace(id)
		if !ok || id == "" {
			continue
		}
		opts = append(opts, domain.LLMModelOption{ID: id, Label: strings.TrimSpace(label)})
	}
	return opts
}
