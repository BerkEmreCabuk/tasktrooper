package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/resources"
)

// Load reads path, falling back to the copy compiled into the binary when there
// is no such file.
//
// That fallback is what lets the packaged desktop app run: it ships one
// executable, with no resources directory beside it and no say in the working
// directory it is launched from. A file on disk still wins, so a checkout and a
// CONFIG_PATH override behave exactly as before.
//
// A file that exists but cannot be read is still an error — that is a broken
// configuration somebody wrote, and quietly serving the built-in one instead
// would hide it.
func Load(path string) (*domain.Config, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Parse(resources.ConfigYAML)
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(raw)
}

func Parse(raw []byte) (*domain.Config, error) {
	expanded := expandEnv(string(raw))

	k := koanf.New(".")
	if err := k.Load(rawbytes.Provider([]byte(expanded)), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	var cfg domain.Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	cfg.ExpandEnv()
	applyDefaults(&cfg)
	// Browser tools default to enabled — on images without chromium every
	// browser_* call returns an explicit "chromium not found" error, so being
	// on costs nothing. Checked against the raw keys because a bool zero-value
	// cannot distinguish an absent tools.browser block from an explicit false.
	if !k.Exists("tools.browser.enabled") {
		cfg.Tools.Browser.Enabled = true
	}
	return &cfg, nil
}

func applyDefaults(cfg *domain.Config) {
	if cfg.LLM.MaxIterations <= 0 {
		cfg.LLM.MaxIterations = 30
	}
	if cfg.LLM.TaskMaxIterations <= 0 {
		cfg.LLM.TaskMaxIterations = 80
	}
	// llm.run_max_total_tokens has no clamp here: 0 is a meaningful value on
	// its own ("disabled"), not "unset" — same convention as
	// embedding.requests_per_minute below (0 = unthrottled), which also has no
	// applyDefaults entry. The loop's own SetRunTokenCap treats <=0 as off.
	if cfg.Tools.Web.MaxResponseBytes <= 0 {
		cfg.Tools.Web.MaxResponseBytes = 1048576
	}
	if cfg.Storage.Postgres.MaxConns <= 0 {
		cfg.Storage.Postgres.MaxConns = 10
	}
	if cfg.Storage.Sessions.TTL <= 0 {
		cfg.Storage.Sessions.TTL = 24 * time.Hour
	}
	if cfg.Jobs.MaxConcurrent <= 0 {
		cfg.Jobs.MaxConcurrent = 3
	}
	if cfg.Jobs.Timeout <= 0 {
		cfg.Jobs.Timeout = 10 * time.Minute
	}
	if cfg.Board.VerifyMaxFixAttempts <= 0 {
		cfg.Board.VerifyMaxFixAttempts = 2
	}
	if cfg.Board.ReconcileStaleAfter <= 0 {
		cfg.Board.ReconcileStaleAfter = 30 * time.Minute
	}
	// One minute, not five: the sweep is now the recovery path for an orphaned
	// PENDING run (board.pendingStaleAfter), and the interval is what a user
	// actually waits with a spinning card. The work is two queries against one
	// board, so the extra ticks cost nothing worth counting.
	if cfg.Board.ReconcileInterval <= 0 {
		cfg.Board.ReconcileInterval = time.Minute
	}
	// 45m / 2m. The window has to outlast the pipeline runner's own 30-minute
	// poll so a live pipeline always gets to produce a real verdict first; see
	// board.PipelineGateWindow for the full derivation.
	if cfg.Board.PipelineGateTimeout <= 0 {
		cfg.Board.PipelineGateTimeout = 45 * time.Minute
	}
	if cfg.Board.PipelineGateInterval <= 0 {
		cfg.Board.PipelineGateInterval = 2 * time.Minute
	}
	if cfg.RAG.ChunkSize <= 0 {
		cfg.RAG.ChunkSize = 1000
	}
	if cfg.RAG.ChunkOverlap <= 0 {
		cfg.RAG.ChunkOverlap = 200
	}
	if cfg.RAG.TopK <= 0 {
		cfg.RAG.TopK = 5
	}
	if cfg.RAG.StorageDir == "" {
		cfg.RAG.StorageDir = "./data/files"
	}
	// Retrying is on by default; throttling is not — most providers answer a
	// serial embedding stream fine, and requests_per_minute is what an operator
	// sets once they know the quota of theirs.
	// A negative max_retries is how retrying is turned off; 0 is "unset".
	if cfg.Embedding.MaxRetries == 0 {
		cfg.Embedding.MaxRetries = 5
	}
	if cfg.Embedding.RetryBackoff <= 0 {
		cfg.Embedding.RetryBackoff = 2 * time.Second
	}
	if cfg.Embedding.MaxRetryWait <= 0 {
		cfg.Embedding.MaxRetryWait = 60 * time.Second
	}
	if cfg.Embedding.RequestTimeout <= 0 {
		cfg.Embedding.RequestTimeout = 90 * time.Second
	}
	// 0 = unset (this default applies); a negative value disables the query
	// embedding cache, same "0 unset / negative off" convention as MaxRetries
	// above.
	if cfg.Embedding.QueryCacheEntries == 0 {
		cfg.Embedding.QueryCacheEntries = 2048
	}
	if cfg.Evolution.TickInterval <= 0 {
		cfg.Evolution.TickInterval = 10 * time.Minute
	}
	if cfg.Evolution.ReflectInterval <= 0 {
		cfg.Evolution.ReflectInterval = 24 * time.Hour
	}
	if cfg.Evolution.RevisionDebounce <= 0 {
		cfg.Evolution.RevisionDebounce = 30 * time.Minute
	}
	if cfg.Evolution.ImpactWindow <= 0 {
		cfg.Evolution.ImpactWindow = 7 * 24 * time.Hour
	}
	if cfg.Evolution.MinEventsForImpact <= 0 {
		cfg.Evolution.MinEventsForImpact = 3
	}
	if cfg.Evolution.MaxSkillChanges <= 0 {
		cfg.Evolution.MaxSkillChanges = 3
	}
	if cfg.Evolution.MaxRuleChanges <= 0 {
		cfg.Evolution.MaxRuleChanges = 3
	}
	if cfg.Evolution.MaxSkillsPerAgent <= 0 {
		cfg.Evolution.MaxSkillsPerAgent = 25
	}
	if cfg.Evolution.MaxRulesPerAgent <= 0 {
		cfg.Evolution.MaxRulesPerAgent = 15
	}
	if cfg.Evolution.MaxMemoryChanges <= 0 {
		cfg.Evolution.MaxMemoryChanges = 5
	}
	if cfg.Evolution.MemoryMaxCount <= 0 {
		cfg.Evolution.MemoryMaxCount = 200
	}
	if cfg.Evolution.EvidenceMaxChars <= 0 {
		cfg.Evolution.EvidenceMaxChars = 24000
	}
	if cfg.Storeops.PollInterval <= 0 {
		cfg.Storeops.PollInterval = 5 * time.Minute
	}
	if cfg.DeployOps.PollInterval <= 0 {
		cfg.DeployOps.PollInterval = 2 * time.Minute
	}
	// DeployOps.HealthWindow is deliberately NOT defaulted here. It has exactly
	// one consumer (deploywatch.New), that consumer already defaults a
	// non-positive value to deploywatch.DefaultHealthWindow, and copying the
	// number into this file would be a second place for it to be wrong.
	// The binary name only: where the CLI lives differs per install (Homebrew,
	// npm global, a version manager shim) and PATH is the one thing every one
	// of them agrees on. An unset CLAUDE_CODE_BIN expands to "" here, which is
	// why this is a default rather than a value in config.yml.
	if strings.TrimSpace(cfg.ClaudeCode.Binary) == "" {
		cfg.ClaudeCode.Binary = "claude"
	}
	// MaxTurns is left at 0 on purpose: the executor owns those
	// defaults (claudecode.DefaultMaxTurns) and a second
	// copy here is a second number to keep in step.
}
