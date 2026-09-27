package prompt

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const workspaceRepoDescriptionMax = 120

type workspaceFactsInput struct {
	ProjectsHeader string
	HasRepos       bool
	RepoHeader     string
	RepoLines      []string
	NoReposLine    string
}

var workspaceFactsKey = Define("agent.workspace_facts", workspaceFactsInput{
	ProjectsHeader: "Projects (0): none registered",
	NoReposLine:    "Repositories (0): none registered — the team registers one as part of delivery.",
})

// Empty input still renders: "none registered" is a fact worth stating and keeps the never-ask rule attached.
// projectTypes and componentsByRepo are optional (nil when the project model
// has not scanned yet, or is not wired) — the snapshot still renders without them.
func WorkspaceFactsBlock(projects []domain.InitiativeProject, repos []domain.Repository, projectTypes map[uuid.UUID]domain.ProjectType, componentsByRepo map[uuid.UUID][]domain.ComponentSummary) string {
	projectNames := make(map[uuid.UUID]string, len(projects))
	labels := make([]string, 0, len(projects))
	for _, p := range projects {
		projectNames[p.ID] = p.Name
		labels = append(labels, projectLabel(p, projectTypes))
	}

	in := workspaceFactsInput{ProjectsHeader: "Projects (0): none registered"}
	if len(labels) > 0 {
		in.ProjectsHeader = fmt.Sprintf("Projects (%d): %s", len(labels), strings.Join(labels, ", "))
	}

	if len(repos) == 0 {
		in.NoReposLine = "Repositories (0): none registered — the team registers one as part of delivery."
	} else {
		in.HasRepos = true
		in.RepoHeader = fmt.Sprintf("Repositories (%d) — all checked out and fully accessible to the agent team:", len(repos))
		in.RepoLines = make([]string, 0, len(repos))
		for _, r := range repos {
			in.RepoLines = append(in.RepoLines, workspaceRepoLine(r, projectNames, componentsByRepo[r.ID]))
		}
	}
	return strings.TrimRight(workspaceFactsKey.Render(in), "\n")
}

func workspaceRepoLine(r domain.Repository, projectNames map[uuid.UUID]string, components []domain.ComponentSummary) string {
	var line strings.Builder
	line.WriteString("- ")
	line.WriteString(r.Name)
	if r.Kind != "" {
		line.WriteString(" (kind=")
		line.WriteString(r.Kind)
		line.WriteString(")")
	}
	if linked := linkedProjectNames(projectNames, r.ProjectIDs); len(linked) > 0 {
		line.WriteString(" — projects: ")
		line.WriteString(strings.Join(linked, ", "))
	}
	if label := componentsLabel(components); label != "" {
		line.WriteString(" — ")
		line.WriteString(label)
	}
	if desc := truncateRunes(r.Description, workspaceRepoDescriptionMax); desc != "" {
		line.WriteString(" — ")
		line.WriteString(desc)
	}
	return line.String()
}

func projectLabel(p domain.InitiativeProject, projectTypes map[uuid.UUID]domain.ProjectType) string {
	t, ok := projectTypes[p.ID]
	if !ok || t == "" {
		return p.Name
	}
	return fmt.Sprintf("%s (%s)", p.Name, t)
}

// componentsLabel renders a repository's components: a single-component repo
// (or one not yet scanned) just names the role, a monorepo lists every
// component as "path (role)".
func componentsLabel(components []domain.ComponentSummary) string {
	if len(components) == 0 {
		return ""
	}
	if len(components) == 1 {
		return "role: " + string(components[0].Role)
	}
	parts := make([]string, 0, len(components))
	for _, c := range components {
		parts = append(parts, fmt.Sprintf("%s (%s)", c.Path, c.Role))
	}
	return "components: " + strings.Join(parts, ", ")
}

func linkedProjectNames(names map[uuid.UUID]string, ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if name, ok := names[id]; ok && name != "" {
			out = append(out, name)
		}
	}
	return out
}

func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
