package domain

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// The project profile is stored as sections rather than one markdown blob:
// half of it is derivable and must not be rewritten by a model that "remembers"
// npm and Vercel; freshness and injection are per-fact, so one run spends its
// token budget on the sections that matter instead of breadth across all of
// them.
const (
	ProfileSectionPurpose       = "purpose"
	ProfileSectionStack         = "stack"
	ProfileSectionLayout        = "layout"
	ProfileSectionCommands      = "commands"
	ProfileSectionCICD          = "ci_cd"
	ProfileSectionDeploy        = "deploy"
	ProfileSectionIntegrations  = "integrations"
	ProfileSectionGitWorkflow   = "git_workflow"
	ProfileSectionTestMap       = "test_map"
	ProfileSectionHotspots      = "hotspots"
	ProfileSectionEntrypoints   = "entrypoints"
	ProfileSectionConventions   = "conventions"
	ProfileSectionInvariants    = "invariants"
	ProfileSectionChangeRecipes = "change_recipes"
	ProfileSectionDangerZones   = "danger_zones"
	ProfileSectionGotchas       = "gotchas"
	// ProfileSectionNotes is where a legacy free-markdown write lands, so an
	// agent calling the old single-string tool shape still records something.
	ProfileSectionNotes = "notes"
)

// ProfileOrigin separates what a parser established from what a model judged:
// a derived section is regenerated from the tree on every refresh; an agent
// section survives until its evidence goes stale.
type ProfileOrigin string

const (
	ProfileOriginDerived ProfileOrigin = "derived"
	ProfileOriginAgent   ProfileOrigin = "agent"
)

// profileSectionMeta fixes display order, the human title, and which sections
// an agent is allowed to write. Derived-only sections are refused from the
// tool: a model overwriting facts with priors is exactly the failure the
// section split exists to end.
type profileSectionMeta struct {
	Title       string
	Order       int
	AgentWrites bool
	// AlwaysInject marks the sections every repo-scoped run needs regardless
	// of what the task touches.
	AlwaysInject bool
}

var profileSections = map[string]profileSectionMeta{
	ProfileSectionPurpose:       {Title: "Purpose", Order: 10, AgentWrites: true, AlwaysInject: true},
	ProfileSectionStack:         {Title: "Stack", Order: 20, AlwaysInject: true},
	ProfileSectionLayout:        {Title: "Layout", Order: 30},
	ProfileSectionCommands:      {Title: "Build / test / run", Order: 40, AlwaysInject: true},
	ProfileSectionCICD:          {Title: "CI", Order: 50},
	ProfileSectionDeploy:        {Title: "How it ships", Order: 60, AlwaysInject: true},
	ProfileSectionIntegrations:  {Title: "Integrations", Order: 70},
	ProfileSectionGitWorkflow:   {Title: "Git workflow", Order: 80, AlwaysInject: true},
	ProfileSectionTestMap:       {Title: "Test map", Order: 90},
	ProfileSectionHotspots:      {Title: "Churn hotspots", Order: 100},
	ProfileSectionEntrypoints:   {Title: "Entrypoints", Order: 110, AgentWrites: true},
	ProfileSectionConventions:   {Title: "Conventions", Order: 120, AgentWrites: true, AlwaysInject: true},
	ProfileSectionInvariants:    {Title: "Invariants", Order: 130, AgentWrites: true, AlwaysInject: true},
	ProfileSectionChangeRecipes: {Title: "Change recipes", Order: 140, AgentWrites: true},
	ProfileSectionDangerZones:   {Title: "Danger zones", Order: 150, AgentWrites: true, AlwaysInject: true},
	ProfileSectionGotchas:       {Title: "Gotchas", Order: 160, AgentWrites: true},
	ProfileSectionNotes:         {Title: "Notes", Order: 170, AgentWrites: true},
}

// ValidProfileSection reports whether id names a known section.
func ValidProfileSection(id string) bool {
	_, ok := profileSections[id]
	return ok
}

// AgentWritableProfileSections lists the sections the update tool accepts, in
// display order — the tool's schema enumerates them so a model cannot invent a
// section name that would never be rendered.
func AgentWritableProfileSections() []string {
	out := make([]string, 0, len(profileSections))
	for id, meta := range profileSections {
		if meta.AgentWrites {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return profileSections[out[i]].Order < profileSections[out[j]].Order })
	return out
}

// ProfileSectionTitle is the heading a section renders under.
func ProfileSectionTitle(id string) string {
	if meta, ok := profileSections[id]; ok {
		return meta.Title
	}
	return strings.ToUpper(id[:1]) + strings.ReplaceAll(id[1:], "_", " ")
}

func profileSectionOrder(id string) int {
	if meta, ok := profileSections[id]; ok {
		return meta.Order
	}
	return 999
}

// ProfileEvidence is the path (and optionally the line) a claim was read from.
// Every agent-written claim needs at least one, and the write is rejected when
// the path is not in the working copy: an unverifiable claim in a profile that
// every future run inherits is worse than a missing section.
type ProfileEvidence struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
	Note string `json:"note,omitempty"`
}

func (e ProfileEvidence) String() string {
	if e.Line > 0 {
		return e.Path + ":" + itoa(e.Line)
	}
	return e.Path
}

// ProfileSection is one stored piece of the profile.
type ProfileSection struct {
	ID           uuid.UUID         `json:"id"`
	RepositoryID uuid.UUID         `json:"repository_id"`
	Section      string            `json:"section"`
	BodyMD       string            `json:"body_md"`
	Evidence     []ProfileEvidence `json:"evidence,omitempty"`
	// SourcePaths are the files this section's truth depends on: a push that
	// touches one marks the section stale; a push that touches none leaves it
	// alone. Derived sections get theirs from the collector, agent sections
	// from their own evidence.
	SourcePaths  []string      `json:"source_paths,omitempty"`
	SourceCommit string        `json:"source_commit,omitempty"`
	Origin       ProfileOrigin `json:"origin"`
	Stale        bool          `json:"stale"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// Title is the section's rendered heading.
func (s ProfileSection) Title() string { return ProfileSectionTitle(s.Section) }

// SortProfileSections orders sections for rendering and for the UI.
func SortProfileSections(sections []ProfileSection) {
	sort.SliceStable(sections, func(i, j int) bool {
		return profileSectionOrder(sections[i].Section) < profileSectionOrder(sections[j].Section)
	})
}

// RenderProfileMarkdown flattens sections into the single markdown document
// that the repositories.profile_md cache holds — what existing injection sites
// and the settings UI read. Evidence rides along as a trailing source line so a
// human (and the next refresh) can check any claim.
func RenderProfileMarkdown(sections []ProfileSection) string {
	SortProfileSections(sections)
	var b strings.Builder
	for _, s := range sections {
		body := strings.TrimSpace(s.BodyMD)
		if body == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("## ")
		b.WriteString(s.Title())
		if s.Stale {
			b.WriteString(" _(stale — sources changed since this was written)_")
		}
		b.WriteString("\n\n")
		b.WriteString(body)
		if len(s.Evidence) > 0 {
			b.WriteString("\n\nSources: ")
			refs := make([]string, 0, len(s.Evidence))
			for _, e := range s.Evidence {
				refs = append(refs, "`"+e.String()+"`")
			}
			b.WriteString(strings.Join(refs, ", "))
		}
	}
	return b.String()
}

// SelectProfileSections picks what one run needs: the always-inject set plus
// anything whose section is relevant to the repo kind the task touches. An
// empty kind returns everything, the safe default for a chat.
func SelectProfileSections(sections []ProfileSection, kind string) []ProfileSection {
	if kind == "" {
		SortProfileSections(sections)
		return sections
	}
	out := make([]ProfileSection, 0, len(sections))
	for _, s := range sections {
		meta, known := profileSections[s.Section]
		if !known || meta.AlwaysInject || sectionRelevantToKind(s, kind) {
			out = append(out, s)
		}
	}
	SortProfileSections(out)
	return out
}

// sectionRelevantToKind keeps a per-area section only when its body actually
// concerns the kind at hand — a layout section that only names apps/mobile is
// noise in a backend run.
func sectionRelevantToKind(s ProfileSection, kind string) bool {
	switch s.Section {
	case ProfileSectionLayout, ProfileSectionTestMap, ProfileSectionCICD,
		ProfileSectionHotspots, ProfileSectionEntrypoints, ProfileSectionChangeRecipes:
		hay := strings.ToLower(s.BodyMD)
		for _, hint := range kindHints(kind) {
			if strings.Contains(hay, hint) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

func kindHints(kind string) []string {
	switch kind {
	case RepoKindFrontend:
		return []string{"web", "frontend", "react", "vue", "vite", "next", "ui"}
	case RepoKindBackend:
		return []string{"backend", "api", "server", "go", "python", "service", "migration"}
	case RepoKindMobile:
		return []string{"mobile", "ios", "android", "swift", "flutter", "xcode"}
	case RepoKindWorker:
		return []string{"worker", "job", "queue", "consumer", "cron"}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Proposals
// ---------------------------------------------------------------------------

// A profiling pass reads the same tree the pipeline settings ask a human to
// describe by hand — repo kind, sub-projects, build/test commands, the deploy
// workflow. Rather than leaving those fields blank next to a profile that
// already knows the answers, the pass writes proposals: applied automatically
// when the field is still empty, offered for one click when it would overwrite
// something a human set.
const (
	ProposalFieldRepoKind      = "repo_kind"
	ProposalFieldSubRepoKinds  = "sub_repo_kinds"
	ProposalFieldBuildCommand  = "build_command"
	ProposalFieldTestCommand   = "test_command"
	ProposalFieldVerifyCommand = "verify_command"
	ProposalFieldPipelineJob   = "pipeline_job"
)

type ProfileProposalStatus string

const (
	ProposalPending   ProfileProposalStatus = "pending"
	ProposalApplied   ProfileProposalStatus = "applied"
	ProposalDismissed ProfileProposalStatus = "dismissed"
)

// ProfileProposal is one settings change the profiling pass believes in, with
// the evidence behind it and what it would replace.
type ProfileProposal struct {
	ID           uuid.UUID `json:"id"`
	RepositoryID uuid.UUID `json:"repository_id"`
	Field        string    `json:"field"`
	// Slot distinguishes the several proposals one field can carry at once —
	// a pipeline slot is (category, sub-repo kind), everything else is "".
	Slot      string                `json:"slot,omitempty"`
	Value     json.RawMessage       `json:"value"`
	Current   string                `json:"current,omitempty"`
	Label     string                `json:"label"`
	Evidence  []ProfileEvidence     `json:"evidence,omitempty"`
	Status    ProfileProposalStatus `json:"status"`
	CreatedAt time.Time             `json:"created_at"`
	AppliedAt *time.Time            `json:"applied_at,omitempty"`
}

// ValidProposalField reports whether f is a field the applier knows how to
// write; anything else is rejected at the tool boundary.
func ValidProposalField(f string) bool {
	switch f {
	case ProposalFieldRepoKind, ProposalFieldSubRepoKinds, ProposalFieldBuildCommand,
		ProposalFieldTestCommand, ProposalFieldVerifyCommand, ProposalFieldPipelineJob:
		return true
	}
	return false
}

// PipelineJobProposal is the Value shape carried by a ProposalFieldPipelineJob
// proposal: which workflow/job fills one (sub-repo, category) slot.
type PipelineJobProposal struct {
	SubRepoKind string `json:"sub_repo_kind"`
	Category    string `json:"category"`
	TargetKind  string `json:"target_kind"`
	TargetRef   string `json:"target_ref"`
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
