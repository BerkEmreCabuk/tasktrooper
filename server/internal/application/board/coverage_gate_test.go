package board

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestCoverageThresholdDefaultsTo90(t *testing.T) {
	assert.Equal(t, 90.0, coverageThreshold(domain.Repository{}, ""))
}

func TestCoverageThresholdHonoursTheRepositoryOverride(t *testing.T) {
	assert.Equal(t, 55.0, coverageThreshold(domain.Repository{CoverageThreshold: 55}, ""))
}

func TestCoverageThresholdHonoursTheSubProjectOverride(t *testing.T) {
	sub := 40.0
	repo := domain.Repository{
		CoverageThreshold: 55,
		SubProjects: []domain.RepoSubProject{
			{Path: "apps/web", Kind: domain.RepoKindFrontend, CoverageThreshold: &sub},
			{Path: "apps/api", Kind: domain.RepoKindBackend},
		},
	}
	assert.Equal(t, 40.0, coverageThreshold(repo, "apps/web"))
	assert.Equal(t, 55.0, coverageThreshold(repo, "apps/api"))
	assert.Equal(t, 55.0, coverageThreshold(repo, "apps/nope"))
}

func TestCoverageIsSilentWhenThereIsNoRecipe(t *testing.T) {
	dir := t.TempDir()
	assert.Empty(t, coverageReport(t.Context(), dir, domain.Repository{}, ""))
}

func TestOverallCoverageNoteSaysItIsNotEnforced(t *testing.T) {
	armed := overallCoverageNote(domain.Repository{RequireOverallCoverage: true, CoverageThreshold: 80}, "", 48.5)
	assert.Contains(t, armed, "48.5%")
	assert.Contains(t, armed, "threshold 80%")
	assert.Contains(t, armed, "not enforced")

	assert.Contains(t, overallCoverageNote(domain.Repository{}, "", 48.5), "reported only")
}

func TestOverallCoverageNoteFollowsASubProjectThatDisarmsTheGate(t *testing.T) {
	off := false
	repo := domain.Repository{
		RequireOverallCoverage: true,
		CoverageThreshold:      80,
		SubProjects: []domain.RepoSubProject{
			{Path: "apps/worker", Kind: domain.RepoKindWorker, CoverageEnabled: &off},
		},
	}
	assert.Contains(t, overallCoverageNote(repo, "apps/worker", 48.5), "reported only")
	assert.Contains(t, overallCoverageNote(repo, "", 48.5), "not enforced")
}

func TestDetectCoveragePicksTheEcosystem(t *testing.T) {
	goDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(goDir, "go.mod"), []byte("module x\n"), 0o644))
	stage := detectCoverage(goDir)
	require.NotNil(t, stage)
	assert.Equal(t, []string{"go", "test", "-coverprofile=coverage.out", "-covermode=atomic", "./..."}, stage.command)

	flutterDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(flutterDir, "pubspec.yaml"), []byte("name: x\n"), 0o644))
	stage = detectCoverage(flutterDir)
	require.NotNil(t, stage)
	assert.Equal(t, []string{"flutter", "test", "--coverage"}, stage.command)
}

func TestParseLcovCoverageCountsExecutedLines(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "coverage"), 0o755))
	lcov := "SF:lib/a.dart\nDA:1,1\nDA:2,0\nDA:3,4\nend_of_record\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "coverage", "lcov.info"), []byte(lcov), 0o644))

	pct, ok := parseLcovCoverage(dir, "")
	require.True(t, ok)
	assert.InDelta(t, 66.7, pct, 0.1)
}

func TestParseLcovCoverageReadsACRLFReport(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "coverage"), 0o755))
	lcov := "SF:lib/a.dart\r\nDA:1,1\r\nDA:2,0\r\nDA:3,4\r\nend_of_record\r\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "coverage", "lcov.info"), []byte(lcov), 0o644))

	pct, ok := parseLcovCoverage(dir, "")
	require.True(t, ok)
	assert.InDelta(t, 66.7, pct, 0.1)
}

type exitCodeError int

func (e exitCodeError) Error() string { return "exit status" }
func (e exitCodeError) ExitCode() int { return int(e) }

func TestMissingToolExitIsTheWindowsCommandNotFoundCode(t *testing.T) {
	tests := []struct {
		name string
		goos string
		err  error
		want bool
	}{
		{name: "store alias stub on windows", goos: "windows", err: exitCodeError(9009), want: true},
		{name: "wrapped store alias stub on windows", goos: "windows", err: fmt.Errorf("stage: %w", exitCodeError(9009)), want: true},
		{name: "ordinary failure on windows", goos: "windows", err: exitCodeError(1), want: false},
		{name: "same code elsewhere is a real exit", goos: "linux", err: exitCodeError(9009), want: false},
		{name: "not an exit", goos: "windows", err: errors.New("boom"), want: false},
		{name: "no error", goos: "windows", err: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, missingToolExit(tt.goos, tt.err))
		})
	}
}

func TestParseLcovCoverageReportsUnmeasuredWithoutAReport(t *testing.T) {
	_, ok := parseLcovCoverage(t.TempDir(), "")
	assert.False(t, ok)
}

func TestDetectCoverageForDotnetSolution(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Shop.sln"), nil, 0o644))
	stage := detectCoverage(dir)
	require.NotNil(t, stage)
	assert.Equal(t, []string{"dotnet", "test", "Shop.sln"}, stage.command[:3])
	assert.Contains(t, stage.command, "--collect:XPlat Code Coverage")
	assert.Equal(t, []string{dotnetCoverageDir}, stage.artifacts)
}

func writeCoberturaReport(t *testing.T, dir, run, body string) {
	t.Helper()
	p := filepath.Join(dir, dotnetCoverageDir, run)
	require.NoError(t, os.MkdirAll(p, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(p, "coverage.cobertura.xml"), []byte(body), 0o644))
}

func coberturaClass(filename, lines string) string {
	return `<class name="C" filename="` + filename + `"><lines>` + lines + `</lines></class>`
}

func coberturaDoc(classes ...string) string {
	return `<?xml version="1.0"?><coverage><packages><package name="Api"><classes>` +
		strings.Join(classes, "") + `</classes></package></packages></coverage>`
}

// Generated code (source generators under obj/, *.g.cs, EF *.Designer.cs) is
// instrumented like hand-written code; left in, a template web API reports
// hundreds of generated lines against a handful of real ones.
func TestParseDotnetCoverageIgnoresGeneratedSource(t *testing.T) {
	dir := t.TempDir()
	writeCoberturaReport(t, dir, "run", coberturaDoc(
		coberturaClass("src/Api/Pricing.cs", `<line number="1" hits="1"/><line number="2" hits="0"/>`),
		coberturaClass("src/Api/obj/Debug/net10.0/Gen/OpenApi.generated.cs", `<line number="1" hits="0"/><line number="2" hits="0"/><line number="3" hits="0"/>`),
		coberturaClass("src/Api/Migrations/20260101_Init.Designer.cs", `<line number="1" hits="0"/>`),
	))

	pct, ok := parseDotnetCoverage(dir, "")
	require.True(t, ok)
	assert.InDelta(t, 50.0, pct, 0.1)

	hits, ok := dotnetCoverageLines(dir)
	require.True(t, ok)
	assert.Equal(t, lineHits{"src/Api/Pricing.cs": {1: 1, 2: 0}}, hits)
}

func TestParseDotnetCoverageWithoutReportsIsUnmeasured(t *testing.T) {
	_, ok := parseDotnetCoverage(t.TempDir(), "")
	assert.False(t, ok)
}

func TestParseReportedCoverageDefersToTheSharedReader(t *testing.T) {
	pct, ok := parseReportedCoverage("", "Lines        : 93.5%")
	require.True(t, ok)
	assert.InDelta(t, 93.5, pct, 0.01)

	_, ok = parseReportedCoverage("", "no numbers here")
	assert.False(t, ok)
}

func TestParseGremlinsScoreNormalisesFractions(t *testing.T) {
	pct, ok := parseGremlinsScore("", `{"mutation score": 0.82}`)
	require.True(t, ok)
	assert.InDelta(t, 82.0, pct, 0.01)

	pct, ok = parseGremlinsScore("", `{"mutation score": 82.0}`)
	require.True(t, ok)
	assert.InDelta(t, 82.0, pct, 0.01)
}

func TestMutationIsSilentWithoutARecipe(t *testing.T) {
	assert.Empty(t, runMutation(t.Context(), t.TempDir(), domain.Repository{}, ""))
}

func TestEffectiveMutationGateResolvesRepoThenSubProject(t *testing.T) {
	on, off := true, false
	sub := 70.0
	repo := domain.Repository{
		MutationEnabled:   true,
		MutationThreshold: 60,
		SubProjects: []domain.RepoSubProject{
			{Path: "apps/api", Kind: domain.RepoKindBackend, MutationThreshold: &sub},
			{Path: "apps/web", Kind: domain.RepoKindFrontend, MutationEnabled: &off},
			{Path: "apps/cli", Kind: domain.RepoKindBackend, MutationEnabled: &on},
		},
	}
	assert.Equal(t, domain.QualityGate{Enabled: true, Threshold: 60}, repo.EffectiveMutationGate(""))
	assert.Equal(t, domain.QualityGate{Enabled: true, Threshold: 70}, repo.EffectiveMutationGate("apps/api"))
	assert.Equal(t, domain.QualityGate{Enabled: false, Threshold: 60}, repo.EffectiveMutationGate("apps/web"))
	assert.Equal(t, domain.QualityGate{Enabled: true, Threshold: 60}, repo.EffectiveMutationGate("apps/cli"))
}
