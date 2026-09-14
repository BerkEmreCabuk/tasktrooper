package repoprofile

import (
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repofacts"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// profileReminderMessage is appended for the single retry when a run ends
// without having stored a section.
const profileReminderMessage = "You have not stored a single section. The run only counts once update_project_profile accepts one — " +
	"call it now with the sections you can back with real file paths. If a section rejected your evidence, fix the path and resend it."

// profileSystemPrompt is the fixed refresh prompt for the judgment half.
//
// It is written against the specific failure it replaces: the previous prompt
// asked for "purpose; tech stack; layout; commands; conventions; deploy shape;
// gotchas" as free markdown, and a model with a Next.js prior answered with
// "Use TypeScript for type safety", "Organize components in src/components"
// and "Deploy using Vercel" for a repository that is none of those things.
// Every clause below exists to make that answer impossible: the facts are
// given, the derived sections are off-limits, each section names what only
// THIS repository could put in it, and nothing lands without a path that
// resolves in the working copy.
const profileSystemPrompt = `You are a system architect writing the judgment half of a repository's project profile.
Every agent that later works on this repository inherits what you write, so a plausible-sounding invention costs every future run.

The deterministic half is already collected and stored: stack, layout, build/test/run commands, CI, deploy path, integrations, git workflow, test map and churn hotspots are in the "Verified repository facts" message. They are facts read off the tree by a parser. Do not restate them, do not re-derive them, do not contradict them, and never call update_project_profile with those sections — it will refuse them.

Your job is the part a parser cannot produce. Explore the code with the read-only tools, then store these sections:

- purpose: what this repository is for and who consumes it, in 1-3 sentences. Name the actual product surface, not the framework.
- entrypoints: where execution starts — main functions, route tables, job registrations, screen roots. Give the file for each.
- conventions: the rules THIS codebase follows that a new contributor would otherwise break. Each one needs the file that demonstrates it.
- invariants: things that break if violated — an ordering, a required field, a check every handler must pass through. Name where it is enforced.
- change_recipes: for the 2-4 changes this repo actually receives, the files to touch and in what order. Derive them from the code and from what changes together in the churn hotspots.
- danger_zones: the code that is expensive to get wrong — high fan-in, order-sensitive migrations, anything with a comment explaining why it is the way it is.
- gotchas: surprises that cost someone an hour. Only ones you can point at.

Hard rules:
1. Every section must carry evidence: real repo-relative paths (with a line when it sharpens the point). A path that does not exist in the working copy is rejected and the section is dropped.
2. If a claim would be equally true of any other repository with the same stack, it is not a fact about this one — delete it. "Use TypeScript for type safety" and "follow the framework's best practices" are the exact shape being banned.
3. Prefer 3 sharp claims with file paths over 10 general ones.
4. Say nothing about how to run, build or deploy the project — that half is already stored.
5. Record only stable facts, never a change log.

Repository kind: %s — weight your exploration accordingly (frontend → routes/components/state; backend → services/DB/migrations/APIs; mobile → targets/screens/signing; monorepo → each app, and what crossing between them requires).

When done, call update_project_profile with your sections (it replaces each section you send and leaves the others alone). Also call save_memory for at most 3 durable non-obvious facts — facts about the CODEBASE that a future run would otherwise have to rediscover, never a note about a task, a PR or a commit.`

// buildRefreshMessages assembles the refresh conversation: the fixed prompt,
// the verified facts, whatever judgment sections already exist, and the ask.
func buildRefreshMessages(repo domain.Repository, reason string, facts repofacts.Facts, existing []domain.ProfileSection, only []string) []domain.Message {
	kind := repo.Kind
	if kind == "" {
		kind = "unspecified"
	}
	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: fmt.Sprintf(profileSystemPrompt, kind)},
	}

	if block := repofacts.PromptFacts(facts); block != "" {
		messages = append(messages, domain.Message{Role: domain.RoleSystem, Content: block})
	}

	if prev := renderExistingAgentSections(existing); prev != "" {
		messages = append(messages, domain.Message{
			Role:    domain.RoleSystem,
			Content: "Sections you wrote previously — keep what is still true, correct what is not, and resend any section you want to change:\n\n" + prev,
		})
	}

	ask := fmt.Sprintf("Analyze this repository and update its project profile. Reason: %s.", reason)
	if scoped := scopeInstruction(only); scoped != "" {
		ask += " " + scoped
	}
	messages = append(messages, domain.Message{Role: domain.RoleUser, Content: ask})
	return messages
}

// scopeInstruction narrows a push-triggered refresh to the sections whose
// source files actually moved. Rewriting the whole profile because one file
// changed is how a profile drifts: each full rewrite is another chance for the
// model to replace a specific claim with a general one.
func scopeInstruction(only []string) string {
	agentScoped := make([]string, 0, len(only))
	for _, s := range only {
		if isAgentWritable(s) {
			agentScoped = append(agentScoped, s)
		}
	}
	if len(agentScoped) == 0 {
		if len(only) > 0 {
			// Only derived sections went stale; those were already rebuilt
			// without the model. Give it the smallest honest job.
			return "The derived sections were rebuilt from the tree. Only re-check your own sections against those changes and resend the ones that are now wrong."
		}
		return ""
	}
	return "A push changed files these sections depend on: " + strings.Join(agentScoped, ", ") +
		". Verify those sections against the current code and resend the ones that changed; leave the rest alone."
}

func renderExistingAgentSections(sections []domain.ProfileSection) string {
	var b strings.Builder
	for _, s := range sections {
		if s.Origin != domain.ProfileOriginAgent || strings.TrimSpace(s.BodyMD) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("### " + s.Section)
		if s.Stale {
			b.WriteString(" (STALE — its sources changed)")
		}
		b.WriteString("\n" + s.BodyMD)
	}
	return b.String()
}
