package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestValidateSubProjectsKeepsTheMobilePlatform(t *testing.T) {
	out, err := domain.ValidateSubProjects([]domain.RepoSubProject{
		{Path: " apps/mobile ", Kind: domain.RepoKindMobile, MobilePlatform: " ios "},
		{Path: "apps/api", Kind: domain.RepoKindBackend},
	})
	require.NoError(t, err)
	require.Equal(t, domain.MobilePlatformIOS, out[0].MobilePlatform)
	require.Empty(t, out[1].MobilePlatform)
}

// Detection results are echoed back by the client on every settings save, so
// they are carried through rather than validated — and dropped together when
// the row stops being mobile, since nobody stated them for a backend.
func TestValidateSubProjectsCarriesTheDetectedBuildTargets(t *testing.T) {
	out, err := domain.ValidateSubProjects([]domain.RepoSubProject{
		{
			Path: "apps/mobile", Kind: domain.RepoKindMobile,
			DetectedBuildTargets: domain.BuildTargets{XcodeScheme: "Runner", GradleModule: "app"},
		},
		{
			Path: "apps/api", Kind: domain.RepoKindBackend,
			DetectedBuildTargets: domain.BuildTargets{XcodeScheme: "Runner"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, domain.BuildTargets{XcodeScheme: "Runner", GradleModule: "app"}, out[0].DetectedBuildTargets)
	require.True(t, out[1].DetectedBuildTargets.IsZero())
}

func TestValidateSubProjectsRejectsABadMobilePlatform(t *testing.T) {
	_, err := domain.ValidateSubProjects([]domain.RepoSubProject{
		{Path: "apps/mobile", Kind: domain.RepoKindMobile, MobilePlatform: "windows_phone"},
	})
	require.ErrorContains(t, err, "invalid mobile platform")
}

// A platform on something that is not mobile is a typo, not a preference: the
// field means nothing there and would quietly mislead whatever reads it.
func TestValidateSubProjectsRejectsAPlatformOnANonMobileSubProject(t *testing.T) {
	_, err := domain.ValidateSubProjects([]domain.RepoSubProject{
		{Path: "apps/api", Kind: domain.RepoKindBackend, MobilePlatform: domain.MobilePlatformIOS},
	})
	require.ErrorContains(t, err, "only meaningful on a mobile project")
}

func TestValidateSubProjectsKeepsTheQualityGateOverrides(t *testing.T) {
	on := true
	threshold := 42.5
	out, err := domain.ValidateSubProjects([]domain.RepoSubProject{
		{
			Path: "apps/api", Kind: domain.RepoKindBackend,
			CoverageEnabled: &on, CoverageThreshold: &threshold,
			MutationEnabled: &on, MutationThreshold: &threshold,
		},
	})
	require.NoError(t, err)
	require.Equal(t, &on, out[0].CoverageEnabled)
	require.Equal(t, &threshold, out[0].CoverageThreshold)
	require.Equal(t, &on, out[0].MutationEnabled)
	require.Equal(t, &threshold, out[0].MutationThreshold)
}

func TestValidateSubProjectsRejectsAThresholdOutside0To100(t *testing.T) {
	for _, bad := range []float64{-1, 100.5} {
		value := bad
		_, err := domain.ValidateSubProjects([]domain.RepoSubProject{
			{Path: "apps/api", Kind: domain.RepoKindBackend, CoverageThreshold: &value},
		})
		require.ErrorContains(t, err, "coverage_threshold must be between 0 and 100")

		_, err = domain.ValidateSubProjects([]domain.RepoSubProject{
			{Path: "apps/api", Kind: domain.RepoKindBackend, MutationThreshold: &value},
		})
		require.ErrorContains(t, err, "mutation_threshold must be between 0 and 100")
	}
}

func TestValidateMobilePlatformAllowsTheUnsetValue(t *testing.T) {
	require.NoError(t, domain.ValidateMobilePlatform("", domain.RepoKindBackend))
	require.NoError(t, domain.ValidateMobilePlatform("  ", domain.RepoKindMonorepo))
	require.NoError(t, domain.ValidateMobilePlatform(domain.MobilePlatformAndroid, domain.RepoKindMobile))
	require.Error(t, domain.ValidateMobilePlatform(domain.MobilePlatformAndroid, domain.RepoKindMonorepo))
}

func TestValidMobilePlatform(t *testing.T) {
	for _, ok := range []string{domain.MobilePlatformIOS, domain.MobilePlatformAndroid, domain.MobilePlatformCross} {
		require.True(t, domain.ValidMobilePlatform(ok), ok)
	}
	require.False(t, domain.ValidMobilePlatform(""), "the unset value is not a platform")
	require.False(t, domain.ValidMobilePlatform("IOS"))
}

func TestEffectiveCoverageGateFallsBackToTheRepository(t *testing.T) {
	off := false
	sub := 30.0
	repo := domain.Repository{
		RequireOverallCoverage: true,
		CoverageThreshold:      85,
		SubProjects: []domain.RepoSubProject{
			{Path: "apps/api", Kind: domain.RepoKindBackend},
			{Path: "apps/web", Kind: domain.RepoKindFrontend, CoverageEnabled: &off, CoverageThreshold: &sub},
		},
	}
	require.Equal(t, domain.QualityGate{Enabled: true, Threshold: 85}, repo.EffectiveCoverageGate(""))
	require.Equal(t, domain.QualityGate{Enabled: true, Threshold: 85}, repo.EffectiveCoverageGate("apps/api"))
	require.Equal(t, domain.QualityGate{Enabled: false, Threshold: 30}, repo.EffectiveCoverageGate("apps/web"))
	require.Equal(t, domain.QualityGate{Enabled: true, Threshold: 85}, repo.EffectiveCoverageGate("apps/unknown"))
}

// A zero override is a statement ("no bar"), not an absence — which is why the
// sub-project fields are pointers.
func TestEffectiveGateHonoursAZeroOverride(t *testing.T) {
	zero := 0.0
	repo := domain.Repository{
		MutationEnabled:   true,
		MutationThreshold: 70,
		SubProjects: []domain.RepoSubProject{
			{Path: "apps/api", Kind: domain.RepoKindBackend, MutationThreshold: &zero},
		},
	}
	require.Equal(t, domain.QualityGate{Enabled: true, Threshold: 0}, repo.EffectiveMutationGate("apps/api"))
}

func TestValidateReleaseEngineAllowsTheUnsetValueAsAuto(t *testing.T) {
	require.NoError(t, domain.ValidateReleaseEngine("", domain.RepoKindBackend))
	require.NoError(t, domain.ValidateReleaseEngine("  ", domain.RepoKindMobile))
	require.NoError(t, domain.ValidateReleaseEngine(domain.ReleaseEngineAuto, domain.RepoKindBackend))
	require.NoError(t, domain.ValidateReleaseEngine(domain.ReleaseEngineActions, domain.RepoKindMobile))
	require.NoError(t, domain.ValidateReleaseEngine(domain.ReleaseEngineLocal, domain.RepoKindMobile))
}

func TestValidateReleaseEngineRejectsAnUnknownValue(t *testing.T) {
	require.ErrorContains(t, domain.ValidateReleaseEngine("jenkins", domain.RepoKindMobile), "invalid release engine")
}

// An engine other than auto is a statement about which machine builds mobile
// releases, and means nothing on a repository that has none.
func TestValidateReleaseEngineRejectsANonAutoEngineOnANonMobileRepo(t *testing.T) {
	require.ErrorContains(t, domain.ValidateReleaseEngine(domain.ReleaseEngineActions, domain.RepoKindBackend), "only meaningful on a mobile project")
	require.ErrorContains(t, domain.ValidateReleaseEngine(domain.ReleaseEngineLocal, domain.RepoKindMonorepo), "only meaningful on a mobile project")
}

func TestDefaultRepoDocPathForLocalRunIsAScript(t *testing.T) {
	require.Equal(t, "scripts/dev.sh", domain.DefaultRepoDocPath(domain.RepoDocLocalRun))
	require.Equal(t, ".ai/architecture.md", domain.DefaultRepoDocPath(domain.RepoDocArchitecture))
	require.Empty(t, domain.DefaultRepoDocPath("nonsense"))
}
