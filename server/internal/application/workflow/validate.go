package workflow

import (
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// StageProblem is one thing wrong with a stage (or a task-type-level
// behaviour, where ColumnSlug is "") sent to PUT
// /v1/task-types/:key/workflow. The handler renders a slice of these as the
// 422 body's `problems` array.
type StageProblem struct {
	ColumnSlug string
	Field      string
	Message    string
}

func (p StageProblem) Error() string {
	if p.ColumnSlug == "" {
		return fmt.Sprintf("%s: %s", p.Field, p.Message)
	}
	return fmt.Sprintf("%s.%s: %s", p.ColumnSlug, p.Field, p.Message)
}

// ValidateStages checks a type's whole proposed stage set: unknown behaviour
// keys, behaviours attached at the wrong scope, missing/invalid params, a
// column param naming a column the board does not have, and more than one
// worker or approver participant per stage. validColumns is nil-safe: when
// nil, column-slug/param checks against the board are skipped (used by
// workflowtest and any caller with no board-columns source at hand).
func ValidateStages(stages []domain.WorkflowStage, validColumns map[string]bool) []StageProblem {
	var problems []StageProblem
	for _, st := range stages {
		col := string(st.Column)
		if !domain.ValidStageKind(st.Kind) {
			problems = append(problems, StageProblem{col, "kind", fmt.Sprintf("unknown stage kind %q", st.Kind)})
		}
		if validColumns != nil && !validColumns[col] {
			problems = append(problems, StageProblem{col, "column_slug", "not a board column"})
		}
		workers, approvers := 0, 0
		for _, p := range st.Participants {
			switch p.Mode {
			case domain.ParticipantModeWorker:
				workers++
			case domain.ParticipantModeApprover:
				approvers++
			default:
				problems = append(problems, StageProblem{col, "participants", fmt.Sprintf("unknown participant mode %q", p.Mode)})
			}
		}
		if workers > 1 {
			problems = append(problems, StageProblem{col, "participants", "at most one worker role per stage"})
		}
		if approvers > 1 {
			problems = append(problems, StageProblem{col, "participants", "at most one approver role per stage"})
		}
		for _, b := range st.Behaviours {
			problems = append(problems, validateBehaviourRef(col, b, domain.BehaviourScopeStage, validColumns)...)
			// A behaviour attached to a kind it can never fire on is not a
			// harmless extra tick: it reads as configured and does nothing.
			if spec, ok := domain.BehaviourRegistry[b.Key]; ok && !spec.AppliesToKind(st.Kind) {
				problems = append(problems, StageProblem{col, "behaviours",
					fmt.Sprintf("behaviour %q does not apply to a %q stage", b.Key, st.Kind)})
			}
		}
	}
	return problems
}

// ValidateTypeBehaviours checks a task type's own behaviours (the three
// (type)-scoped keys): same unknown-key/scope/param rules as ValidateStages,
// at ColumnSlug "".
func ValidateTypeBehaviours(behaviours []domain.BehaviourRef, validColumns map[string]bool) []StageProblem {
	var problems []StageProblem
	for _, b := range behaviours {
		problems = append(problems, validateBehaviourRef("", b, domain.BehaviourScopeType, validColumns)...)
	}
	return problems
}

func validateBehaviourRef(columnSlug string, ref domain.BehaviourRef, wantScope domain.BehaviourScope, validColumns map[string]bool) []StageProblem {
	spec, ok := domain.BehaviourRegistry[ref.Key]
	if !ok {
		return []StageProblem{{columnSlug, "behaviours", fmt.Sprintf("unknown behaviour %q", ref.Key)}}
	}
	var problems []StageProblem
	if spec.Scope != wantScope {
		problems = append(problems, StageProblem{columnSlug, "behaviours",
			fmt.Sprintf("%q is a %s-scoped behaviour and may not be attached here", ref.Key, spec.Scope)})
	}
	for _, p := range spec.Params {
		v, has := ref.Params[p.Name]
		if p.Required && (!has || v == "") {
			problems = append(problems, StageProblem{columnSlug, string(ref.Key), fmt.Sprintf("missing required param %q", p.Name)})
			continue
		}
		if !has || v == "" {
			continue
		}
		switch p.Type {
		case domain.ParamTypeColumn:
			if validColumns != nil && !validColumns[v] {
				problems = append(problems, StageProblem{columnSlug, string(ref.Key), fmt.Sprintf("param %q names unknown column %q", p.Name, v)})
			}
		case domain.ParamTypeEnum:
			if !containsStr(p.Options, v) {
				problems = append(problems, StageProblem{columnSlug, string(ref.Key), fmt.Sprintf("param %q must be one of %v", p.Name, p.Options)})
			}
		case domain.ParamTypeBool:
			if v != "true" && v != "false" {
				problems = append(problems, StageProblem{columnSlug, string(ref.Key), fmt.Sprintf("param %q must be \"true\" or \"false\"", p.Name)})
			}
		}
	}
	for name := range ref.Params {
		if !hasParamSpec(spec.Params, name) {
			problems = append(problems, StageProblem{columnSlug, string(ref.Key), fmt.Sprintf("unknown param %q", name)})
		}
	}
	return problems
}

func hasParamSpec(specs []domain.ParamSpec, name string) bool {
	for _, s := range specs {
		if s.Name == name {
			return true
		}
	}
	return false
}

func containsStr(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
