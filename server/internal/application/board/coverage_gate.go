package board

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/toolchain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/childenv"
)

const DefaultCoverageThreshold = 90.0

const coverageWarningMarker = "[coverage warning]"

const mutationWarningMarker = "[mutation warning]"

const coverageTimeout = 20 * time.Minute

const mutationTimeout = 25 * time.Minute

type CoverageResult struct {
	Percent  float64
	Measured bool
	Detail   string
	Output   string
	Lines    lineHits
}

type coverageStage struct {
	name      string
	command   []string
	parse     func(dir, out string) (float64, bool)
	lines     func(dir string) (lineHits, bool)
	artifacts []string
}

func detectCoverage(dir string) *coverageStage {
	switch {
	case markerExists(dir, "go.mod"):
		return &coverageStage{
			name:      "coverage",
			command:   []string{"go", "test", "-coverprofile=coverage.out", "-covermode=atomic", "./..."},
			parse:     parseGoCoverage,
			lines:     goProfileLines,
			artifacts: []string{"coverage.out"},
		}
	case markerExists(dir, "pubspec.yaml"):
		return &coverageStage{
			name:    "coverage",
			command: []string{"flutter", "test", "--coverage"},
			parse:   parseLcovCoverage,
			lines:   lcovLines,
		}
	case hasVitest(dir):
		return &coverageStage{
			name: "coverage",
			command: []string{
				"npx", "vitest", "run", "--coverage",
				"--coverage.reporter=text-summary", "--coverage.reporter=lcov",
			},
			parse: parseReportedCoverage,
			lines: lcovLines,
		}
	}
	return nil
}

func detectMutation(dir string) *coverageStage {
	switch {
	case markerExists(dir, "go.mod"):
		return &coverageStage{
			name:    "mutation",
			command: []string{"gremlins", "unleash", "--dry-run=false", "--output=json"},
			parse:   parseGremlinsScore,
		}
	case hasStryker(dir):
		return &coverageStage{
			name:    "mutation",
			command: []string{"npx", "stryker", "run"},
			parse:   parseStrykerScore,
		}
	}
	return nil
}

func hasVitest(dir string) bool {
	return markerExists(dir,
		"vitest.config.ts", "vitest.config.js", "vitest.config.mts",
		"vite.config.ts", "vite.config.js") && markerExists(dir, "package.json")
}

func hasStryker(dir string) bool {
	return markerExists(dir, "stryker.config.json", "stryker.conf.json", "stryker.config.mjs", ".stryker.conf.json")
}

func runCoverage(ctx context.Context, dir string, stage *coverageStage, timeout time.Duration) CoverageResult {
	if stage == nil {
		return CoverageResult{Detail: "no coverage recipe for this project type"}
	}
	overlay := toolchain.Default.Overlay(dir)
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, stage.command[0], stage.command[1:]...)
	cmd.Dir = dir
	cmd.Env = childenv.For(os.Environ(), overlay.Env)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := buf.String()

	defer func() {
		for _, artifact := range stage.artifacts {
			_ = os.Remove(filepath.Join(dir, artifact))
		}
	}()
	var lines lineHits
	if stage.lines != nil {
		if read, ok := stage.lines(dir); ok {
			lines = read
		}
	}

	if err != nil && isToolMissing(err) {
		return CoverageResult{Detail: stage.command[0] + " is not installed in this environment"}
	}
	pct, ok := stage.parse(dir, out)
	if !ok {
		detail := "could not read a coverage number from the run"
		if err != nil {
			detail = "the test suite failed"
		}
		return CoverageResult{Detail: detail, Output: truncateTail(out, 6000), Lines: lines}
	}
	if err != nil {

		log.Debug().Err(err).Str("stage", stage.name).Msg("coverage command exited non-zero but reported a number")
	}
	return CoverageResult{Percent: pct, Measured: true, Output: truncateTail(out, 4000), Lines: lines}
}

func isToolMissing(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "executable file not found") ||
		strings.Contains(err.Error(), "no such file or directory"))
}

func parseGoCoverage(dir, out string) (float64, bool) {
	profile := filepath.Join(dir, "coverage.out")
	if _, err := os.Stat(profile); err != nil {
		return 0, false
	}

	cmd := exec.Command("go", "tool", "cover", "-func="+profile)
	cmd.Dir = dir
	funcOut, err := cmd.CombinedOutput()
	if err != nil {
		return 0, false
	}

	return parseReportedCoverage("", string(funcOut))
}

func parseLcovCoverage(dir, _ string) (float64, bool) {
	f, err := os.Open(filepath.Join(dir, "coverage", "lcov.info"))
	if err != nil {
		return 0, false
	}
	defer f.Close()
	var total, covered int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "DA:") {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(line, "DA:"), ",")
		if len(parts) != 2 {
			continue
		}
		total++
		if hits, err := strconv.Atoi(parts[1]); err == nil && hits > 0 {
			covered++
		}
	}
	if total == 0 {
		return 0, false
	}
	return float64(covered) * 100 / float64(total), true
}

func parseReportedCoverage(_, out string) (float64, bool) {
	if pct := ParseCoverage(out); pct != nil {
		return *pct, true
	}
	return 0, false
}

var gremlinsScore = regexp.MustCompile(`(?i)"mutation\s*score"?\s*:?\s*([0-9.]+)`)

func parseGremlinsScore(_, out string) (float64, bool) {
	m := gremlinsScore.FindStringSubmatch(out)
	if len(m) != 2 {
		return 0, false
	}
	pct, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	if pct <= 1 {
		pct *= 100
	}
	return pct, true
}

var strykerScore = regexp.MustCompile(`(?i)mutation score[^0-9]*([0-9.]+)`)

func parseStrykerScore(_, out string) (float64, bool) {
	m := strykerScore.FindStringSubmatch(out)
	if len(m) != 2 {
		return 0, false
	}
	pct, err := strconv.ParseFloat(m[1], 64)
	return pct, err == nil
}

func coverageThreshold(repo domain.Repository, subProjectPath string) float64 {
	if gate := repo.EffectiveCoverageGate(subProjectPath); gate.Threshold > 0 {
		return gate.Threshold
	}
	return DefaultCoverageThreshold
}

func coverageReport(ctx context.Context, dir string, repo domain.Repository, subProjectPath string) string {
	stage := detectCoverage(dir)
	if stage == nil {
		return ""
	}
	res := runCoverage(ctx, dir, stage, coverageTimeout)
	if !res.Measured {
		note := "[unverified] coverage: " + res.Detail
		if res.Output != "" {
			note += "\n" + res.Output
		}
		return note
	}

	notes := []string{overallCoverageNote(repo, subProjectPath, res.Percent)}
	if newCodeNote := newCodeCoverageReport(ctx, dir, res.Lines); newCodeNote != "" {
		notes = append(notes, newCodeNote)
	}

	threshold := coverageThreshold(repo, subProjectPath)
	if repo.EffectiveCoverageGate(subProjectPath).Enabled && res.Percent+0.005 < threshold {
		notes = append(notes, fmt.Sprintf(
			coverageWarningMarker+" overall %.1f%% is below the %.0f%% this repository asks for.\n"+
				"What is missing is coverage of code this task did not touch, so treat it as a note for whoever "+
				"reads this run: if untested paths sit next to your change — the branches that handle errors and "+
				"edge cases — covering them is worth a few minutes. "+
				"It does not hold the task: the hand-off proceeds either way.",
			res.Percent, threshold))
	}
	return strings.Join(notes, "\n")
}

func overallCoverageNote(repo domain.Repository, subProjectPath string, percent float64) string {
	threshold := coverageThreshold(repo, subProjectPath)
	if repo.EffectiveCoverageGate(subProjectPath).Enabled {
		return fmt.Sprintf("[coverage] overall %.1f%% (threshold %.0f%%, reported — not enforced)", percent, threshold)
	}
	return fmt.Sprintf("[coverage] overall %.1f%% (reported only — this repository sets no overall bar)", percent)
}

func runMutation(ctx context.Context, dir string, repo domain.Repository, subProjectPath string) string {
	stage := detectMutation(dir)
	if stage == nil {
		return ""
	}
	res := runCoverage(ctx, dir, stage, mutationTimeout)
	if !res.Measured {
		log.Debug().Str("dir", dir).Str("detail", res.Detail).Msg("mutation testing not run")
		return ""
	}
	note := fmt.Sprintf(
		"[mutation] %.1f%% of mutants killed. Coverage says which lines ran; this says whether a test would have "+
			"noticed them behaving differently. A low score with high coverage means assertions are missing, not lines.",
		res.Percent)
	gate := repo.EffectiveMutationGate(subProjectPath)
	if !gate.Enabled || gate.Threshold <= 0 {
		return note
	}
	if res.Percent+0.005 < gate.Threshold {
		return note + fmt.Sprintf(
			"\n"+mutationWarningMarker+" %.1f%% is below the %.0f%% this repository asks for. "+
				"Say so in your hand-off; it does not hold the task.",
			res.Percent, gate.Threshold)
	}
	return note + fmt.Sprintf(" (threshold %.0f%%, met)", gate.Threshold)
}
