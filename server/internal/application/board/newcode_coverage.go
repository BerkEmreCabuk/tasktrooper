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

// NewCodeCoverageThreshold is the bar the report is written against: the share
// of the lines THIS change wrote that a test executes.
//
// It is the number worth showing, because it is about the change under review
// rather than the codebase around it — a repo measuring 44% overall says
// nothing about whether the diff in front of you is tested. It no longer blocks
// the hand-off: a run that is otherwise finished is not worth holding over a
// percentage, and what the figure is for is telling the reader which lines to
// look at.
//
// Ninety rather than a hundred because a diff always carries lines no test can
// reasonably reach — a wiring line in main, an error branch behind a syscall
// that cannot be provoked — and demanding all of them teaches the agent to
// delete the branch instead of testing it.
const NewCodeCoverageThreshold = 90.0

// newCodeMinLines is the smallest diff whose percentage means anything.
//
// Under it the number is noise that reads as signal: a change touching two
// coverable lines is either 100%, 50% or 0%, so one unreachable wiring line
// reads as half the diff untested. Such a diff gets its counts stated plainly
// instead — the reviewer reading it can see two lines.
const newCodeMinLines = 5

// newCodeSampleLimit bounds the uncovered lines named in the report. The point
// is to show the agent where to start, not to reproduce the diff.
const newCodeSampleLimit = 25

// gitTimeout bounds the diff calls. They are local and read-only; a git that
// hangs this long is broken, and the gate must not inherit the hang.
const gitTimeout = 60 * time.Second

// NewCodeCoverage is the verdict on the lines a change added or modified.
type NewCodeCoverage struct {
	// Percent is Covered/Total as a percentage. Meaningless unless Measured.
	Percent float64
	// Measured separates "the diff's new lines were checked" from "they could
	// not be", which the gate must react to in opposite ways: the first may
	// block, the second may only report. Everything that can go wrong here —
	// no git, no profile, a language with no per-line recipe — lands as false.
	Measured bool
	// Covered/Total count only COVERABLE changed lines: a line the coverage
	// tool has an opinion about. Comments, blanks, imports and type
	// declarations are not instrumented by any of these tools, and counting
	// them as uncovered would fail a documentation commit.
	Covered int
	Total   int
	// Detail carries the reason when Measured is false.
	Detail string
	// Uncovered names up to newCodeSampleLimit changed-but-unexecuted lines as
	// "path:line", so the failure report points at work rather than a number.
	Uncovered []string
}

// changedLines maps repo-relative path → the set of line numbers this change
// added or modified, read from the diff against the branch point.
//
// -U0 is what makes the answer exact: with context lines the hunk header spans
// unchanged code, and every neighbour of an edit would be billed to this task.
// The base is the merge base rather than the branch tip for the same reason
// TaskDiff picks it — a task branch that has fallen behind must not be asked to
// cover everything the default branch gained in the meantime.
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

// parseDiffLines reads a -U0 unified diff and returns the NEW-side line numbers
// each file gained. A hunk with a zero new-side count is a pure deletion and
// contributes nothing: removing code is not a thing tests can cover.
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
			// /dev/null is a deleted file; b/ is git's new-side prefix.
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

// lineHits maps repo-relative path → line number → execution count, for every
// line the coverage tool instrumented. A line absent from the map is a line the
// tool has no opinion about, which is what keeps comments and declarations out
// of the denominator.
type lineHits map[string]map[int]int

// record keeps the highest count seen for a line. Coverage formats emit
// overlapping records — nested Go blocks, a template inlined twice — and the
// line did execute the larger number of times.
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

// goProfileLines reads a Go coverage profile. Its lines are
// "<import-path>/<file>:<startLine>.<col>,<endLine>.<col> <numStmt> <count>",
// so the file has to be re-rooted from the module path onto the repo before it
// can be matched against a diff path.
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

// parseBlockRange reads "12.34,15.2" into its first and last line.
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

// goModulePath reads the module line out of go.mod so profile paths can be made
// repo-relative. An unreadable go.mod is not fatal: paths then fail to match and
// the result is reported unmeasured rather than wrong.
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

// lcovLines reads coverage/lcov.info — what `flutter test --coverage` writes and
// what vitest's lcov reporter writes — into per-line hits. SF: is the file,
// DA:<line>,<hits> the record.
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
			// lcov writers disagree about absolute vs relative; the diff only
			// speaks repo-relative, so everything is normalised to that.
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

// measureNewCode intersects the diff with the coverage profile.
//
// Only lines present in BOTH count. A changed line the tool never instrumented
// is not evidence of anything — it is a comment, an import or a declaration —
// and a changed file the profile does not mention at all is usually a config or
// a fixture, which no suite covers and which must not fail a task.
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
	// Sorted so the sample of uncovered lines is stable between runs: an agent
	// re-reading the report after a fix must not see a different set of lines
	// merely because a map iterated differently.
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

// newCodeCoverageReport states what the diff's own lines measured. It is a
// report, not a verdict: a shortfall is named — with the lines to look at, which
// is the part worth having — and the run hands off anyway.
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
