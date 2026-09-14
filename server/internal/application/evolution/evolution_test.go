package evolution

import (
	"strings"
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestParseReflectionOutput(t *testing.T) {
	clean := `{"self_assessment":"ok","skills":[],"rules":[],"memories":[],"reverts":[]}`
	out, err := parseReflectionOutput(clean)
	if err != nil || out.SelfAssessment != "ok" {
		t.Fatalf("clean parse failed: %v", err)
	}

	fenced := "Here is my analysis:\n```json\n{\"self_assessment\":\"fenced\",\"skills\":[{\"action\":\"create\",\"name\":\"x\",\"description\":\"d\",\"category\":\"c\",\"content\":\"body\"}]}\n```"
	out, err = parseReflectionOutput(fenced)
	if err != nil {
		t.Fatalf("fenced parse failed: %v", err)
	}
	if out.SelfAssessment != "fenced" || len(out.Skills) != 1 || out.Skills[0].Action != "create" {
		t.Fatalf("fenced content wrong: %+v", out)
	}

	prose := "Sure! {\"self_assessment\":\"embedded\"} hope that helps"
	out, err = parseReflectionOutput(prose)
	if err != nil || out.SelfAssessment != "embedded" {
		t.Fatalf("embedded parse failed: %v", err)
	}

	if _, err = parseReflectionOutput("no json here at all"); err == nil {
		t.Fatal("expected error for garbage output")
	}
	if _, err = parseReflectionOutput("{broken json"); err == nil {
		t.Fatal("expected error for broken json")
	}
}

func evts(deltas ...float64) []domain.AgentScoreEvent {
	out := make([]domain.AgentScoreEvent, len(deltas))
	for i, d := range deltas {
		out[i] = domain.AgentScoreEvent{Delta: d, CreatedAt: time.Now()}
	}
	return out
}

func TestClassifyImpact(t *testing.T) {
	cases := []struct {
		name   string
		before []domain.AgentScoreEvent
		after  []domain.AgentScoreEvent
		want   string
	}{
		{"insufficient after data", evts(5, 5, 5), evts(5), domain.EvolutionImpactInsufficientData},
		{"effective: revisions dropped", evts(-10, -10, 5), evts(5, 5, 5), domain.EvolutionImpactEffective},
		{"regressed: revisions rose", evts(5, 5, 5), evts(-10, -10, 5), domain.EvolutionImpactRegressed},
		{"neutral: same shape", evts(5, -10, 5), evts(5, -10, 5), domain.EvolutionImpactNeutral},
		{"no baseline, positive net", evts(), evts(5, 5, 5), domain.EvolutionImpactEffective},
		{"no baseline, negative net", evts(), evts(-10, -5, 5), domain.EvolutionImpactRegressed},
		{"no baseline, flat", evts(), evts(5, -5, -1), domain.EvolutionImpactNeutral},
	}
	for _, c := range cases {
		if got := ClassifyImpact(c.before, c.after, 3); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestReflectionSystemPromptGating(t *testing.T) {
	cfg := domain.EvolutionConfig{MaxSkillChanges: 3, MaxRuleChanges: 3, MaxMemoryChanges: 5, AllowWebResearch: true}
	enabled := reflectionSystemPrompt(domain.Agent{Name: "dev", SelfEvolutionEnabled: true}, cfg)
	if !contains(enabled, "You MAY change") || !contains(enabled, "web_search") {
		t.Error("enabled prompt missing skill-change or web-research permission")
	}
	disabled := reflectionSystemPrompt(domain.Agent{Name: "dev", SelfEvolutionEnabled: false}, cfg)
	if !contains(disabled, "Self-evolution is DISABLED") {
		t.Error("disabled prompt missing gating clause")
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
