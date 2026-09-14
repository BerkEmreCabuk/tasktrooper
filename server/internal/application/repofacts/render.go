package repofacts

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// DerivedSections renders the collected facts into the profile's parser-owned
// sections. These are rewritten on every refresh and are never handed to the
// model to "improve" — that is the whole point of collecting them.
func DerivedSections(f Facts) []domain.ProfileSection {
	if !f.HasAny() {
		return nil
	}
	var out []domain.ProfileSection
	add := func(section, body string, sources []string, evidence []domain.ProfileEvidence) {
		body = strings.TrimSpace(body)
		if body == "" {
			return
		}
		out = append(out, domain.ProfileSection{
			Section:      section,
			BodyMD:       body,
			Evidence:     evidence,
			SourcePaths:  dedupeSorted(sources),
			SourceCommit: f.Git.HeadSHA,
			Origin:       domain.ProfileOriginDerived,
		})
	}

	add(domain.ProfileSectionStack, renderStack(f), manifestPaths(f), evidenceFrom(manifestPaths(f)))
	add(domain.ProfileSectionLayout, renderLayout(f), nil, nil)
	add(domain.ProfileSectionCommands, renderCommands(f), commandSources(f), evidenceFrom(commandSources(f)))
	add(domain.ProfileSectionCICD, renderCI(f), workflowPaths(f), evidenceFrom(workflowPaths(f)))
	add(domain.ProfileSectionDeploy, renderDeploy(f), deploySources(f), evidenceFrom(deploySources(f)))
	add(domain.ProfileSectionIntegrations, renderIntegrations(f), integrationSources(f), evidenceFrom(integrationSources(f)))
	add(domain.ProfileSectionGitWorkflow, renderGitWorkflow(f), nil, nil)
	add(domain.ProfileSectionTestMap, renderTestMap(f), testSources(f), evidenceFrom(testSources(f)))
	add(domain.ProfileSectionHotspots, renderHotspots(f), nil, nil)
	return out
}

// PromptFacts renders the fact block injected into the profiling run. It is
// framed as given truth with an explicit prohibition, because the failure mode
// being fixed is a model that reads facts and then writes its priors anyway.
func PromptFacts(f Facts) string {
	if !f.HasAny() {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Verified repository facts (collected by a parser, not by you)\n\n")
	b.WriteString("These were read directly off the working copy. They are already stored as profile sections. ")
	b.WriteString("Do NOT restate, re-derive or contradict them, and never replace a concrete command or deploy path here with a generic one. ")
	b.WriteString("Use them as the ground you build your judgment on.\n\n")

	writeBlock := func(title, body string) {
		body = strings.TrimSpace(body)
		if body == "" {
			return
		}
		b.WriteString("## " + title + "\n" + body + "\n\n")
	}
	writeBlock("Stack", renderStack(f))
	writeBlock("Layout", renderLayout(f))
	writeBlock("Commands", renderCommands(f))
	writeBlock("CI", renderCI(f))
	writeBlock("How it ships", renderDeploy(f))
	writeBlock("Integrations", renderIntegrations(f))
	writeBlock("Git workflow", renderGitWorkflow(f))
	writeBlock("Test map", renderTestMap(f))
	writeBlock("Churn hotspots", renderHotspots(f))
	if f.Kind != "" {
		writeBlock("Inferred repository kind", "`"+f.Kind+"`"+subKindSuffix(f)+" — "+strings.Join(f.KindEvidence, "; "))
	}
	if len(f.Warnings) > 0 {
		writeBlock("Collection warnings", "- "+strings.Join(f.Warnings, "\n- "))
	}
	return strings.TrimSpace(b.String())
}

func subKindSuffix(f Facts) string {
	if len(f.SubKinds) == 0 {
		return ""
	}
	return " (" + strings.Join(f.SubKinds, ", ") + ")"
}

func renderStack(f Facts) string {
	var b strings.Builder
	if len(f.Languages) > 0 {
		parts := make([]string, 0, len(f.Languages))
		for _, l := range f.Languages {
			parts = append(parts, fmt.Sprintf("%s (%d files)", l.Language, l.Files))
		}
		b.WriteString("Languages: " + strings.Join(parts, ", ") + "\n")
	}
	for _, m := range f.Manifests {
		line := "- `" + m.Path + "` — " + m.Ecosystem
		if m.Manager != "" {
			line += " via " + m.Manager
		}
		if m.Name != "" {
			line += ", module `" + m.Name + "`"
		}
		if m.Version != "" {
			line += ", " + m.Version
		}
		if len(m.Deps) > 0 {
			line += "; " + strings.Join(m.Deps, ", ")
		}
		if len(m.Workspace) > 0 {
			line += "; workspaces: " + strings.Join(m.Workspace, ", ")
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func renderLayout(f Facts) string {
	var b strings.Builder
	for _, d := range f.Layout {
		line := "- `" + d.Path + "/`"
		if d.Role != "" {
			line += " — " + d.Role
		}
		line += fmt.Sprintf(" (%d files)", d.Files)
		b.WriteString(line + "\n")
	}
	if f.Truncated {
		b.WriteString("\n_The walk hit its file cap; deep subtrees may be under-counted._\n")
	}
	return b.String()
}

func renderCommands(f Facts) string {
	if len(f.Commands) == 0 {
		return "No build/test/run command is declared anywhere in the tree (no package.json scripts, Makefile targets or module manifest)."
	}
	var b strings.Builder
	for _, purpose := range []string{"build", "test", "run", "lint", "typecheck", "migrate"} {
		cmds := f.commandsFor(purpose)
		if len(cmds) == 0 {
			continue
		}
		b.WriteString("**" + purpose + "**\n")
		for _, c := range cmds {
			area := c.Area
			if area == "" {
				area = "repo root"
			}
			b.WriteString(fmt.Sprintf("- `%s` in `%s` (declared in `%s`)\n", c.Cmd, area, c.Source))
		}
	}
	return b.String()
}

func renderCI(f Facts) string {
	if len(f.Workflows) == 0 {
		return "No GitHub Actions workflows in `.github/workflows`."
	}
	return "- " + strings.Join(summarizeWorkflows(f), "\n- ")
}

func renderDeploy(f Facts) string {
	if len(f.Deploys) == 0 {
		if len(f.Workflows) == 0 {
			return "Nothing in the tree ships this repository automatically: no deploy workflow and no hosting-provider link."
		}
		return "No workflow or provider link in the tree deploys this repository; CI only builds and tests it."
	}
	var b strings.Builder
	for _, d := range f.Deploys {
		line := "- **" + d.Provider + "**"
		if d.Environment != "" {
			line += " → " + d.Environment
		}
		line += " on `" + d.Trigger + "` (`" + d.Evidence + "`)"
		if d.Automatic {
			line += " — **automatic: landing a commit is the deploy**"
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func renderIntegrations(f Facts) string {
	if len(f.Integrations) == 0 {
		return ""
	}
	var b strings.Builder
	for _, in := range f.Integrations {
		line := "- " + in.Name + " (" + in.Category + ")"
		if in.Detail != "" {
			line += " — " + in.Detail
		}
		line += " · `" + in.Evidence + "`"
		b.WriteString(line + "\n")
	}
	return b.String()
}

func renderGitWorkflow(f Facts) string {
	g := f.Git
	var b strings.Builder
	if g.RemoteSlug != "" {
		b.WriteString("- Remote: `" + g.RemoteSlug + "` on " + g.RemoteHost + "\n")
	}
	if g.DefaultBranch != "" {
		b.WriteString("- Default branch: `" + g.DefaultBranch + "`\n")
	}
	if g.BranchPattern != "" {
		line := "- Branch naming: " + g.BranchPattern
		if len(g.BranchSamples) > 0 {
			line += " — e.g. `" + strings.Join(g.BranchSamples, "`, `") + "`"
		}
		b.WriteString(line + "\n")
	}
	if g.MergeStyle != "" {
		b.WriteString("- Merge style: " + g.MergeStyle + "\n")
	}
	if g.CommitStyle != "" {
		b.WriteString("- Commit subjects: " + g.CommitStyle + "\n")
	}
	// The deploy consequence of landing on the default branch is repeated here
	// on purpose: whoever is reading "git workflow" is deciding how to land a
	// change, and that is the moment the automatic deploy matters.
	for _, d := range f.Deploys {
		if d.Automatic {
			what := d.Provider
			if d.Environment != "" {
				what += " " + d.Environment
			}
			b.WriteString("- ⚠ Landing on `" + strings.TrimPrefix(d.Trigger, "push:") + "` triggers a " +
				what + " deploy (`" + d.Evidence + "`)\n")
			break
		}
	}
	return b.String()
}

func renderTestMap(f Facts) string {
	if len(f.TestAreas) == 0 {
		return "No test runner config or test files were found."
	}
	var b strings.Builder
	for _, a := range f.TestAreas {
		area := a.Area
		if area == "" {
			area = "repo root"
		}
		line := "- `" + area + "` — " + a.Framework
		if a.Command != "" {
			line += ", `" + a.Command + "`"
		}
		line += " (`" + a.Evidence + "`)"
		b.WriteString(line + "\n")
	}
	return b.String()
}

func renderHotspots(f Facts) string {
	if len(f.Git.Hotspots) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Files with the most commits in the last " + itoa(churnCommits) + " commits — where change concentrates:\n")
	for _, h := range f.Git.Hotspots {
		b.WriteString(fmt.Sprintf("- `%s` (%d commits)\n", h.Path, h.Commits))
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// source path collection — what makes a section stale
// ---------------------------------------------------------------------------

func manifestPaths(f Facts) []string {
	out := make([]string, 0, len(f.Manifests))
	for _, m := range f.Manifests {
		out = append(out, m.Path)
	}
	return out
}

func commandSources(f Facts) []string {
	out := make([]string, 0, len(f.Commands))
	for _, c := range f.Commands {
		out = append(out, c.Source)
	}
	return out
}

func workflowPaths(f Facts) []string {
	out := make([]string, 0, len(f.Workflows))
	for _, w := range f.Workflows {
		out = append(out, w.File)
	}
	return out
}

func deploySources(f Facts) []string {
	out := make([]string, 0, len(f.Deploys))
	for _, d := range f.Deploys {
		out = append(out, d.Evidence)
	}
	return out
}

func integrationSources(f Facts) []string {
	out := make([]string, 0, len(f.Integrations))
	for _, in := range f.Integrations {
		out = append(out, in.Evidence)
	}
	return out
}

func testSources(f Facts) []string {
	out := make([]string, 0, len(f.TestAreas))
	for _, a := range f.TestAreas {
		out = append(out, a.Evidence)
	}
	return out
}

func evidenceFrom(paths []string) []domain.ProfileEvidence {
	paths = dedupeSorted(append([]string(nil), paths...))
	if len(paths) > 8 {
		paths = paths[:8]
	}
	out := make([]domain.ProfileEvidence, 0, len(paths))
	for _, p := range paths {
		out = append(out, domain.ProfileEvidence{Path: p})
	}
	return out
}

// ---------------------------------------------------------------------------
// settings proposals
// ---------------------------------------------------------------------------

// Proposals turns the facts into concrete settings the repository page is
// still asking a human to fill in by hand. Nothing is applied here — the
// service decides between auto-apply (field empty) and offer (field set).
func Proposals(f Facts) []domain.ProfileProposal {
	var out []domain.ProfileProposal
	add := func(field string, value any, label string, evidence []string) {
		raw, err := json.Marshal(value)
		if err != nil {
			return
		}
		out = append(out, domain.ProfileProposal{
			Field:    field,
			Value:    raw,
			Label:    label,
			Evidence: evidenceFrom(evidence),
			Status:   domain.ProposalPending,
		})
	}

	if f.Kind != "" {
		add(domain.ProposalFieldRepoKind, f.Kind, "Repository kind: "+f.Kind, kindEvidencePaths(f))
		if len(f.SubKinds) > 0 {
			add(domain.ProposalFieldSubRepoKinds, f.SubKinds, "Sub-projects: "+strings.Join(f.SubKinds, ", "), kindEvidencePaths(f))
		}
	}

	// Commands: the root-most declaration of each purpose is the one the
	// repository settings want. A monorepo whose only build command lives in
	// apps/web still gets a proposal — with the area spelled out, because the
	// command is only correct when run there.
	if cmd, src := primaryCommand(f, "build"); cmd != "" {
		add(domain.ProposalFieldBuildCommand, cmd, "Build command: "+cmd, []string{src})
	}
	if cmd, src := primaryCommand(f, "test"); cmd != "" {
		add(domain.ProposalFieldTestCommand, cmd, "Test command: "+cmd, []string{src})
	}
	if cmd, src := primaryCommand(f, "lint"); cmd != "" {
		add(domain.ProposalFieldVerifyCommand, cmd, "Verify command: "+cmd, []string{src})
	} else if cmd, src := primaryCommand(f, "typecheck"); cmd != "" {
		add(domain.ProposalFieldVerifyCommand, cmd, "Verify command: "+cmd, []string{src})
	}

	for _, p := range pipelineProposals(f) {
		add(domain.ProposalFieldPipelineJob, p,
			"Pipeline "+p.Category+labelSuffix(p.SubRepoKind)+" → "+p.TargetRef, []string{p.TargetRef})
	}
	return out
}

func labelSuffix(subKind string) string {
	if subKind == "" {
		return ""
	}
	return " (" + subKind + ")"
}

func kindEvidencePaths(f Facts) []string {
	var out []string
	for _, m := range f.Manifests {
		out = append(out, m.Path)
	}
	if len(out) > 4 {
		out = out[:4]
	}
	return out
}

// primaryCommand picks the command a repository-level setting should carry:
// the one declared closest to the root, preferring a shorter area over a
// deeper one so a monorepo's root script wins over a per-app duplicate.
func primaryCommand(f Facts, purpose string) (cmd, source string) {
	cmds := f.commandsFor(purpose)
	if len(cmds) == 0 {
		return "", ""
	}
	sort.SliceStable(cmds, func(i, j int) bool { return len(cmds[i].Area) < len(cmds[j].Area) })
	best := cmds[0]
	if best.Area == "" {
		return best.Cmd, best.Source
	}
	return "cd " + best.Area + " && " + best.Cmd, best.Source
}

// pipelineProposals maps deploy-capable workflows onto the pipeline slots the
// settings page exposes. Only unambiguous cases are proposed: a workflow that
// names its environment. Guessing between two deploy workflows is exactly the
// choice the settings page already asks a human to make.
func pipelineProposals(f Facts) []domain.PipelineJobProposal {
	var out []domain.PipelineJobProposal
	for _, w := range f.Workflows {
		if !w.Deploys || !w.Dispatchable {
			continue
		}
		category := ""
		hay := strings.ToLower(w.Name + " " + w.File)
		switch {
		case strings.Contains(hay, "prod"):
			category = domain.PipelineCategoryProdDeploy
		case strings.Contains(hay, "preprod"), strings.Contains(hay, "pre-prod"):
			category = domain.PipelineCategoryPreProdDeploy
		case strings.Contains(hay, "stag"), strings.Contains(hay, "stage"):
			category = domain.PipelineCategoryStageDeploy
		}
		if category == "" {
			continue
		}
		out = append(out, domain.PipelineJobProposal{
			Category:   category,
			TargetKind: domain.PipelineTargetWorkflow,
			TargetRef:  strings.TrimPrefix(w.File, ".github/workflows/"),
		})
	}
	return out
}
