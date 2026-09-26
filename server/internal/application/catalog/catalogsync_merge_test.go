package catalog

import "testing"

// Golden fixtures pin mergeSkill's LLM-facing prose exactly as it read before
// the move to catalog/system/prompts/catalog/*.md (see the prompt library
// program). The move must keep these byte-identical.

func TestMergeSkillSystemPromptGolden(t *testing.T) {
	const want = "You merge two versions of the same agent skill into one SKILL.md document. " +
		"Reply with ONLY the merged document, frontmatter first (name:, description:, category:), " +
		"then ---, then the merged body. Keep every distinct instruction from both sides; " +
		"drop only what the two versions contradict themselves about."
	if got := mergeSkillSystemPrompt(); got != want {
		t.Fatalf("mergeSkillSystemPrompt() =\n%q\nwant\n%q", got, want)
	}
}

func TestMergeSkillUserMessageGolden(t *testing.T) {
	cases := []struct {
		name     string
		skill    string
		local    string
		upstream string
		want     string
	}{
		{
			name:     "simple bodies",
			skill:    "code-review-rubric",
			local:    "# Local\nReview for correctness.",
			upstream: "# Upstream\nReview for correctness and security.",
			want: "Skill: code-review-rubric\n" +
				"This machine's current copy (LOCAL):\n---\n# Local\nReview for correctness.\n---\n" +
				"New catalog revision (UPSTREAM):\n---\n# Upstream\nReview for correctness and security.\n---\n",
		},
		{
			name:     "empty upstream",
			skill:    "spec-authoring",
			local:    "Write specs with acceptance criteria.",
			upstream: "",
			want: "Skill: spec-authoring\n" +
				"This machine's current copy (LOCAL):\n---\nWrite specs with acceptance criteria.\n---\n" +
				"New catalog revision (UPSTREAM):\n---\n\n---\n",
		},
		{
			name:     "multiline content with embedded dashes",
			skill:    "deploy-templates",
			local:    "Step 1\n---\nStep 2",
			upstream: "Step A\nStep B\nStep C",
			want: "Skill: deploy-templates\n" +
				"This machine's current copy (LOCAL):\n---\nStep 1\n---\nStep 2\n---\n" +
				"New catalog revision (UPSTREAM):\n---\nStep A\nStep B\nStep C\n---\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeSkillUserMessage(tc.skill, tc.local, tc.upstream)
			if got != tc.want {
				t.Fatalf("mergeSkillUserMessage(%q, ...) =\n%q\nwant\n%q", tc.skill, got, tc.want)
			}
		})
	}
}
