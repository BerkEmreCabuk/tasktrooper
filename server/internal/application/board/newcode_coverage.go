package board

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// 90 not 100: every diff carries lines no test can reach, and demanding them teaches the agent to delete the branch.
const NewCodeCoverageThreshold = 90.0

const newCodeMinLines = 5

const newCodeSampleLimit = 25

const gitTimeout = 60 * time.Second

type NewCodeCoverage struct {
	Percent float64
	// Measured is what lets the gate react in opposite ways; Percent is meaningless unless Measured.
	Measured  bool
	Covered   int
	Total     int
	Detail    string
	Uncovered []string
}

func changedLines(ctx context.Context, dir string) (map[string]map[int]bool, error) {
	base := mergeBase(ctx, dir)
	if base == "" {
		return nil, fmt.Errorf("no merge base against origin — the workspace has no upstream to diff against")
	}
	out, err := runGit(ctx, dir, "diff", "-U0", base)
	if err != nil {
		return nil, fmt.Errorf("git diff -U0: %w", err)
	}
	return parseDiffLines(out), nil
}

func mergeBase(ctx context.Context, dir string) string {
	for _, ref := range []string{"origin/HEAD", "origin/main", "origin/master"} {
		if out, err := runGit(ctx, dir, "merge-base", "HEAD", ref); err == nil {
			if base := strings.TrimSpace(out); base != "" {
				return base
			}
		}
	}
	return ""
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

func parseDiffLines(diff string) map[string]map[int]bool {
	files := make(map[string]map[int]bool)
	var current string
	scanner := bufio.NewScanner(strings.NewReader(diff))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "+++ "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "+++ "))
			if path == "/dev/null" {
				current = ""
				continue
			}
			current = strings.TrimPrefix(path, "b/")
		case strings.HasPrefix(line, "@@"):
			if current == "" {
				continue
			}
			m := hunkHeader.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			start, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			count := 1
			if m[2] != "" {
				if count, err = strconv.Atoi(m[2]); err != nil {
					continue
				}
			}
			if count == 0 {
				continue
			}
			set := files[current]
			if set == nil {
				set = make(map[int]bool)
				files[current] = set
			}
			for i := 0; i < count; i++ {
				set[start+i] = true
			}
		}
	}
	return files
}

type lineHits map[string]map[int]int

func (h lineHits) record(file string, line, count int) {
	set := h[file]
	if set == nil {
		set = make(map[int]int)
		h[file] = set
	}
	if prev, seen := set[line]; seen && prev >= count {
		return
	}
	set[line] = count
}

func goProfileLines(dir string) (lineHits, bool) {
	f, err := os.Open(filepath.Join(dir, "coverage.out"))
	if err != nil {
		return nil, false
	}
	defer f.Close()

	module := goModulePath(dir)
	hits := make(lineHits)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		colon := strings.LastIndex(line, ":")
		if colon < 0 {
			continue
		}
		file := line[:colon]
		if module != "" {
			file = strings.TrimPrefix(file, module+"/")
		}
		fields := strings.Fields(line[colon+1:])
		if len(fields) != 3 {
			continue
		}
		startLine, endLine, ok := parseBlockRange(fields[0])
		if !ok {
			continue
		}
		count, err := strconv.Atoi(fields[2])
		if err != nil {
			continue
		}
		for ln := startLine; ln <= endLine; ln++ {
			hits.record(file, ln, count)
		}
	}
	if len(hits) == 0 {
		return nil, false
	}
	return hits, true
}

func parseBlockRange(span string) (int, int, bool) {
	comma := strings.Index(span, ",")
	if comma < 0 {
		return 0, 0, false
	}
	start, ok := parsePos(span[:comma])
	if !ok {
		return 0, 0, false
	}
	end, ok := parsePos(span[comma+1:])
	if !ok || end < start {
		return 0, 0, false
	}
	return start, end, true
}

func parsePos(pos string) (int, bool) {
	if dot := strings.Index(pos, "."); dot >= 0 {
		pos = pos[:dot]
	}
	n, err := strconv.Atoi(pos)
	return n, err == nil
}

func goModulePath(dir string) string {
	f, err := os.Open(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

func lcovLines(dir string) (lineHits, bool) {
	f, err := os.Open(filepath.Join(dir, "coverage", "lcov.info"))
	if err != nil {
		return nil, false
	}
	defer f.Close()

	hits := make(lineHits)
	current := ""
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "SF:"):
			path := strings.TrimPrefix(line, "SF:")
			if filepath.IsAbs(path) {
				if rel, err := filepath.Rel(dir, path); err == nil && !strings.HasPrefix(rel, "..") {
					path = rel
				}
			}
			current = filepath.ToSlash(strings.TrimPrefix(path, "./"))
		case strings.HasPrefix(line, "DA:"):
			if current == "" {
				continue
			}
			parts := strings.SplitN(strings.TrimPrefix(line, "DA:"), ",", 2)
			if len(parts) != 2 {
				continue
			}
			ln, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil {
				continue
			}
			count, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				continue
			}
			hits.record(current, ln, count)
		case line == "end_of_record":
			current = ""
		}
	}
	if len(hits) == 0 {
		return nil, false
	}
	return hits, true
}

func measureNewCode(ctx context.Context, dir string, hits lineHits) NewCodeCoverage {
	if len(hits) == 0 {
		return NewCodeCoverage{Detail: "the run left no per-line coverage profile to read"}
	}
	changed, err := changedLines(ctx, dir)
	if err != nil {
		return NewCodeCoverage{Detail: err.Error()}
	}
	if len(changed) == 0 {
		return NewCodeCoverage{Detail: "the diff against the branch point is empty"}
	}

	var covered, total int
	var uncovered []string
	paths := make([]string, 0, len(changed))
	for path := range changed {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		fileHits, ok := hits[filepath.ToSlash(path)]
		if !ok {
			continue
		}
		nums := make([]int, 0, len(changed[path]))
		for ln := range changed[path] {
			nums = append(nums, ln)
		}
		sort.Ints(nums)
		for _, ln := range nums {
			count, instrumented := fileHits[ln]
			if !instrumented {
				continue
			}
			total++
			if count > 0 {
				covered++
				continue
			}
			if len(uncovered) < newCodeSampleLimit {
				uncovered = append(uncovered, fmt.Sprintf("%s:%d", path, ln))
			}
		}
	}
	if total == 0 {
		return NewCodeCoverage{Detail: "this change touched no lines the coverage tool instruments"}
	}
	return NewCodeCoverage{
		Percent:   float64(covered) * 100 / float64(total),
		Measured:  true,
		Covered:   covered,
		Total:     total,
		Uncovered: uncovered,
	}
}

func newCodeCoverageReport(ctx context.Context, dir string, hits lineHits) string {
	res := measureNewCode(ctx, dir, hits)
	if !res.Measured {
		log.Debug().Str("dir", dir).Str("detail", res.Detail).Msg("new-code coverage not measured")
		return "[unverified] new-code coverage: " + res.Detail
	}
	if res.Total < newCodeMinLines {
		return fmt.Sprintf("[new-code coverage] %d of %d changed lines covered — too few to read anything into.",
			res.Covered, res.Total)
	}
	if res.Percent+0.005 >= NewCodeCoverageThreshold {
		return fmt.Sprintf("[new-code coverage] %.1f%% (%d/%d changed lines, threshold %.0f%%)",
			res.Percent, res.Covered, res.Total, NewCodeCoverageThreshold)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, coverageWarningMarker+" new-code coverage %.1f%% (%d/%d changed lines) is below the %.0f%% this change should leave behind.\n",
		res.Percent, res.Covered, res.Total, NewCodeCoverageThreshold)
	sb.WriteString("This is the coverage of the lines YOUR diff added or changed, not the repository's overall figure — " +
		"it is about your change alone, and no amount of pre-existing untested code affects it.\n")
	if len(res.Uncovered) > 0 {
		sb.WriteString("Uncovered lines you wrote:\n")
		for _, u := range res.Uncovered {
			sb.WriteString("  " + u + "\n")
		}
		if missing := res.Total - res.Covered - len(res.Uncovered); missing > 0 {
			fmt.Fprintf(&sb, "  …and %d more.\n", missing)
		}
	}
	sb.WriteString("Tests that execute them — the error and edge-case branches, not more assertions on the happy path — " +
		"are worth adding while the code is still fresh. This is a warning only: the task moves on either way.")
	return strings.TrimSpace(sb.String())
}
