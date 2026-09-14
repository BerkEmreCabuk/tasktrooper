package deploy_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deploy"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type TemplateSuite struct{ suite.Suite }

func TestTemplateSuite(t *testing.T) { suite.Run(t, new(TemplateSuite)) }

func (s *TemplateSuite) TestCatalogLoadsAndIsWellFormed() {
	templates := deploy.Templates()

	s.Require().NotEmpty(templates)
	for _, tpl := range templates {
		s.NotEmpty(tpl.ID, "template id")
		s.True(domain.ValidDeployProvider(tpl.Provider), "provider %q", tpl.Provider)
		s.NotEmpty(tpl.WorkflowFile, "%s: workflow file", tpl.ID)
		s.NotEmpty(tpl.Body, "%s: body", tpl.ID)
		s.NotEmpty(tpl.RequiredVars, "%s: required vars", tpl.ID)
		// Every recipe must ship a verification and a way back; a deploy that
		// cannot prove itself or be undone is the thing this catalog replaces.
		s.Contains(tpl.Body, "Smoke check", "%s: no smoke check", tpl.ID)
		s.NotEmpty(tpl.RollbackHint, "%s: no rollback hint", tpl.ID)
		s.Contains(strings.ToLower(tpl.Body), "workflow_dispatch", "%s: not dispatchable", tpl.ID)
	}
}

func (s *TemplateSuite) TestLookupByIDAndProvider() {
	byID, ok := deploy.Template("gcp-cloud-run")
	s.Require().True(ok)
	s.Equal(domain.DeployProviderGCPCloudRun, byID.Provider)

	byProvider, ok := deploy.Template(domain.DeployProviderGCPCloudRun)
	s.Require().True(ok)
	s.Equal(byID.ID, byProvider.ID)

	_, ok = deploy.Template("does-not-exist")
	s.False(ok)
}

func (s *TemplateSuite) TestKindFilterKeepsMobileAwayFromServerDeploys() {
	frontend := deploy.TemplatesForKind(domain.RepoKindFrontend)
	s.Require().NotEmpty(frontend)
	for _, tpl := range frontend {
		s.True(tpl.SupportsKind(domain.RepoKindFrontend))
		s.NotEqual(domain.DeployProviderGCPCloudRun, tpl.Provider)
	}
}

func (s *TemplateSuite) TestMissingVarsListsOnlyUnfilledRequiredKeys() {
	tpl, ok := deploy.Template("gcp-cloud-run")
	s.Require().True(ok)

	missing := tpl.MissingVars(map[string]string{
		"gcp_project_id": "tt-prod",
		"gcp_region":     "   ",
	})

	s.Contains(missing, "gcp_region", "whitespace is not a value")
	s.NotContains(missing, "gcp_project_id")
	s.Equal(missing, sortedCopy(missing), "the list is sorted so the message is stable")
}

func (s *TemplateSuite) TestRenderFillsVarsAndFlagsTheMissingOnes() {
	tpl, ok := deploy.Template("gcp-cloud-run")
	s.Require().True(ok)

	out := deploy.Render(tpl, domain.DeployTarget{
		Env:       domain.DeployEnvProd,
		HealthURL: "https://api.example.com/health",
		Vars: map[string]string{
			"gcp_project_id": "tt-prod",
			"gcp_region":     "europe-west1",
			"service_name":   "api",
		},
	})

	s.Contains(out, "tt-prod")
	s.Contains(out, "europe-west1")
	s.Contains(out, "https://api.example.com/health")
	s.Contains(out, "deploy-cloud-run-prod")
	s.Contains(out, "SET THIS", "artifact_repo was not provided and must stay visible")
	s.NotContains(out, "{{gcp_region}}")
}

// GitHub Actions expressions use the same braces; rendering must not eat them.
func (s *TemplateSuite) TestRenderPreservesActionsExpressions() {
	tpl, ok := deploy.Template("gcp-cloud-run")
	s.Require().True(ok)

	out := deploy.Render(tpl, domain.DeployTarget{Env: domain.DeployEnvProd})

	s.Contains(out, "${{ secrets.GCP_WIF_PROVIDER }}")
	s.Contains(out, "${{ github.sha }}")
}

// Mobile deploy no longer goes through the template catalog — it is a store
// connection (App Store Connect / Play Console) picking an app, not a
// rendered workflow recipe — so the catalog carries no kind:[mobile] entries
// and this filter is expected to come back empty rather than erroring.
func (s *TemplateSuite) TestTemplatesForKindMobileIsIntentionallyEmpty() {
	s.Empty(deploy.TemplatesForKind(domain.RepoKindMobile))
}

func sortedCopy(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
