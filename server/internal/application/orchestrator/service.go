package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	goccyjson "github.com/goccy/go-json"
	"github.com/rs/zerolog/log"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type storedPlan struct {
	domain.PlannerOutput
	Verification *domain.VerificationResult `json:"verification,omitempty"`
}

type pipelineOpts struct {
	constrainedAgentID *uuid.UUID
}

type Service struct {
	intake         *IntakeExtractor
	planner        *Planner
	replanner      *Replanner
	verifier       *Verifier
	executor       *Executor
	catalog        port.CatalogStore
	llm            port.LLMClient
	agentLoop      agent.Runner
	cfg            domain.OrchestrationConfig
	contextBuilder *ContextBuilder
	workspace      WorkspaceLister
}

func NewService(
	llm port.LLMClient,
	catalog port.CatalogStore,
	skillRetriever port.SkillRetriever,
	agentLoop agent.Runner,
	cfg domain.OrchestrationConfig,
	contextBuilder *ContextBuilder,
) *Service {
	if cfg.SubtaskHistoryMode == "" {
		cfg.SubtaskHistoryMode = domain.SubtaskHistoryModeIsolated
	}
	if cfg.DependencyOutputMaxChars <= 0 {
		cfg.DependencyOutputMaxChars = 4000
	}
	if cfg.MaxReplanIterations <= 0 {
		cfg.MaxReplanIterations = 2
	}
	exec := NewExecutor(agentLoop, catalog, cfg)
	exec.contextBuilder = contextBuilder
	return &Service{
		intake:         NewIntakeExtractor(llm),
		planner:        NewPlanner(llm, catalog, skillRetriever, cfg.MaxPlanTasks, cfg.SkillRetrievalTopK),
		replanner:      NewReplanner(llm, catalog, cfg.MaxPlanTasks),
		verifier:       NewVerifier(llm),
		executor:       exec,
		catalog:        catalog,
		llm:            llm,
		agentLoop:      agentLoop,
		cfg:            cfg,
		contextBuilder: contextBuilder,
	}
}

func (s *Service) Run(ctx context.Context, userMessage string, history []domain.Message, model string, policy domain.ToolPolicy, lang string) (domain.AgentResponse, error) {
	return s.RunMulti(ctx, userMessage, history, model, policy, lang)
}

func (s *Service) RunMulti(ctx context.Context, userMessage string, history []domain.Message, model string, policy domain.ToolPolicy, lang string) (domain.AgentResponse, error) {
	return s.runPipeline(ctx, userMessage, history, model, policy, lang, pipelineOpts{})
}

func (s *Service) RunSolo(ctx context.Context, userMessage string, history []domain.Message, model string, policy domain.ToolPolicy, lang string, constrainedAgentID uuid.UUID) (domain.AgentResponse, error) {
	return s.runPipeline(ctx, userMessage, history, model, policy, lang, pipelineOpts{constrainedAgentID: &constrainedAgentID})
}

func (s *Service) runPipeline(ctx context.Context, userMessage string, history []domain.Message, model string, policy domain.ToolPolicy, lang string, opts pipelineOpts) (domain.AgentResponse, error) {
	runID := uuid.Nil
	sessionID := registry.SessionIDFromContext(ctx)
	if rec := activity.FromContext(ctx); rec != nil {
		runID = rec.RunID()
	}
	if runID == uuid.Nil {
		return domain.AgentResponse{}, fmt.Errorf("orchestration requires an active run")
	}

	// Intake and the planner have no tools, so the workspace snapshot is their
	// only way to know which repositories and projects exist. Without it they
	// ask the stakeholder for facts the system already stores.
	workspaceFacts := s.workspaceFacts(ctx)

	plannerOpts := PlannerOptions{ConstrainedAgentID: opts.constrainedAgentID, Lang: lang, Workspace: workspaceFacts}

	intakeOpts := IntakeOptions{Lang: lang, Workspace: workspaceFacts}
	if opts.constrainedAgentID != nil {
		if agentRec, agentErr := s.catalog.GetAgent(ctx, *opts.constrainedAgentID); agentErr == nil {
			intakeOpts.SoloAgentName = agentRec.Name
			intakeOpts.SoloAgentDescription = agentRec.Description
			intakeOpts.ProviderType = agentRec.ProviderType
			plannerOpts.ProviderType = agentRec.ProviderType
			// The planner already receives the agent's rules and skills through
			// its catalog entries. Intake decides ask-vs-proceed before the
			// planner ever runs, so it needs the same policy or it asks
			// questions the agent's own rules forbid.
			if rules, ruleErr := s.catalog.ListEnabledRulesByAgent(ctx, agentRec.ID); ruleErr == nil {
				intakeOpts.SoloAgentRules = rules
			} else {
				log.Warn().Err(ruleErr).Str("agent", agentRec.Name).Msg("intake: solo agent rules unavailable")
			}
			if skills, skillErr := s.catalog.ListSkillsByAgent(ctx, agentRec.ID); skillErr == nil {
				intakeOpts.SoloAgentSkills = skills
			} else {
				log.Warn().Err(skillErr).Str("agent", agentRec.Name).Msg("intake: solo agent skills unavailable")
			}
		}
	}
	providerType := plannerOpts.ProviderType

	intake, err := s.intake.Extract(ctx, userMessage, history, model, intakeOpts)
	if err != nil {
		return domain.AgentResponse{}, err
	}

	if !intake.Ready {
		req := clarificationFromIntake(intake)
		if rec := activity.FromContext(ctx); rec != nil {
			rec.Step("clarification_requested", clarificationStepPayload(req, "intake"))
		}
		return buildClarificationResponse(req), nil
	}

	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("planner_start", map[string]string{"model": model})
	}

	var plannerContext []domain.Message
	if s.contextBuilder != nil && sessionID != uuid.Nil {
		ctxMsgs, ctxErr := s.contextBuilder.BuildPlannerContext(ctx, sessionID, userMessage)
		if ctxErr != nil {
			return domain.AgentResponse{}, ctxErr
		}
		plannerContext = ctxMsgs
	}

	output, err := s.planner.Generate(ctx, intake, userMessage, history, model, plannerContext, plannerOpts)
	if err != nil {
		return domain.AgentResponse{}, err
	}

	if !output.Ready {
		req := clarificationFromPlanner(output)
		if rec := activity.FromContext(ctx); rec != nil {
			rec.Step("clarification_requested", clarificationStepPayload(req, "planner"))
		}
		return buildClarificationResponse(req), nil
	}

	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("planner_complete", map[string]int{"task_count": len(output.Tasks)})
	}

	plan, orderedPlanTasks, err := s.persistPlan(ctx, runID, output, nil)
	if err != nil {
		return domain.AgentResponse{}, err
	}

	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("orchestration_plan_created", map[string]any{
			"plan_id": plan.ID, "summary": output.Summary, "task_count": len(output.Tasks),
		})
	}

	// A plan left at "running" is what the UI polls forever: it is the row that
	// decides whether the run is still live. Every exit from here on — including
	// a panic or a path added later — has to leave a status behind.
	//
	// "failed" is the right fallback and not PlanStatusIncomplete: an exit that
	// settled nothing is by definition the process stopping without a verdict,
	// which is exactly what "failed" means. "incomplete" claims the run reached
	// the end and only the verifier objected, and nothing here can know that.
	planSettled := false
	settlePlan := func(status string) {
		planSettled = true
		s.setPlanStatus(ctx, plan.ID, status)
	}
	defer func() {
		if !planSettled {
			s.setPlanStatus(ctx, plan.ID, domain.PlanStatusFailed)
		}
	}()

	finalContent, taskResults, err := s.executor.Execute(ctx, plan.ID, intake, output, orderedPlanTasks, history, model, policy, lang, sessionID, nil)
	if err != nil {
		var clarErr ClarificationNeededError
		if errors.As(err, &clarErr) {
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("clarification_requested", clarificationStepPayload(clarErr.Request, "executor"))
			}
			// Pending, not failed: the run resumes when the answer arrives. It is
			// still terminal for this process — nothing else will move the plan.
			settlePlan(domain.PlanStatusPending)
			return buildClarificationResponse(clarErr.Request), nil
		}
		// The executor stopped: a stage errored, the LLM gave up, the context was
		// cancelled. The run did not reach the end, so this is "failed" and not
		// PlanStatusIncomplete — no results are returned either.
		settlePlan(domain.PlanStatusFailed)
		return domain.AgentResponse{}, err
	}

	// verificationFailed is what decides the plan's terminal status below: the
	// verifier's last word on this run. It used to be ignored entirely — every
	// exit from the repair loop fell through to PlanStatusCompleted, so a run
	// whose issues were never repaired reported itself as done and the
	// stakeholder read unverified work as finished. The results are still
	// returned — they were really produced, and throwing them away is the
	// mistake the inconclusive-verification fallback below already refuses to
	// make — but the plan row does not get to claim the goal was met when the
	// only thing that judged it says otherwise.
	verificationFailed := false

	// verificationSkipped is set when the verifier could not run AT ALL for this
	// agent — a configuration fact, not a bad minute. It is carried down to the
	// response so the user is told the gate was skipped instead of being left to
	// infer it from a missing verdict.
	var verificationSkipped error

	var verification domain.VerificationResult
	if s.cfg.VerificationEnabled {
		verification, err = s.verifier.Evaluate(ctx, intake, userMessage, taskResults, model, providerType)
		// Verification is a quality gate over work that already happened. Failing
		// the whole run here (a single unparseable verifier reply is enough) threw
		// away every completed subtask and handed the user an error instead of the
		// output. Treat it as inconclusive and ship what was produced.
		evaluated := err == nil
		if err != nil {
			// A permanent refusal is a different animal from an unparseable
			// reply, and it used to be indistinguishable from one: the silent
			// provider fallback rerouted this call to the tenant's default HTTP
			// provider, so what arrived here was that provider's error and the
			// run shipped "unverified results" with a warning nobody connected
			// to an agent's engine choice. Now the refusal itself arrives, and
			// it says which step, why, and what to change.
			//
			// It still DEGRADES rather than failing the run, and deliberately:
			// verification is a gate over work that already happened, and
			// throwing away every completed subtask to report a misconfigured
			// judge is the trade this block was written to refuse. But it is
			// reported at ERROR and it is written onto the results, because a
			// step that can never run on this agent will be skipped on every
			// single run from now until somebody changes the setting — and a
			// warning line in a log is not how that gets noticed.
			if errors.Is(err, domain.ErrHostExecutedUnservable) {
				log.Error().Err(err).Str("plan_id", plan.ID.String()).
					Msg("verification cannot run for this agent at all; every run will ship unverified until an HTTP provider is configured or verification is turned off")
				verificationSkipped = err
			} else {
				log.Warn().Err(err).Str("plan_id", plan.ID.String()).
					Msg("verification failed to evaluate; returning unverified results")
			}
			verification = domain.VerificationResult{}
			err = nil
		} else {
			_ = s.updateStoredPlan(ctx, plan.ID, output, &verification)
		}

		for iter := 0; iter < s.cfg.MaxReplanIterations && evaluated && !verification.Passed; iter++ {
			repairOutput, replanErr := s.replanner.Generate(ctx, intake, verification.Issues, output, taskResults, userMessage, model, plannerOpts)
			if replanErr != nil {
				// Abandoning the repair loop is an allowed outcome — the plan
				// lands on PlanStatusIncomplete below either way, which is the
				// honest verdict. But WHY it was abandoned has to be readable,
				// and a permanent refusal is worth saying out loud: it means no
				// run on this agent will ever repair anything, which is a
				// different problem from a replanner that had one bad reply.
				permanent := errors.Is(replanErr, domain.ErrHostExecutedUnservable)
				ev := log.Warn()
				if permanent {
					ev = log.Error()
				}
				ev.Err(replanErr).Str("plan_id", plan.ID.String()).Bool("permanent", permanent).
					Msg("replan skipped; verification issues left unrepaired")
				break
			}
			output.Tasks = append(output.Tasks, repairOutput.Tasks...)
			output.Summary = repairOutput.Summary

			newTasks, appendErr := s.appendPlanTasks(ctx, plan.ID, repairOutput.Tasks)
			if appendErr != nil {
				log.Warn().Err(appendErr).Str("plan_id", plan.ID.String()).
					Msg("repair tasks could not be stored; verification issues left unrepaired")
				break
			}
			orderedPlanTasks = append(orderedPlanTasks, newTasks...)

			_, repairResults, execErr := s.executor.Execute(ctx, plan.ID, intake, domain.PlannerOutput{
				Purpose: output.Purpose,
				Goal:    output.Goal,
				Summary: repairOutput.Summary,
				Tasks:   repairOutput.Tasks,
			}, newTasks, history, model, policy, lang, sessionID, taskResults)
			if execErr != nil {
				log.Warn().Err(execErr).Str("plan_id", plan.ID.String()).
					Msg("repair execution failed; verification issues left unrepaired")
				break
			}
			for k, v := range repairResults {
				taskResults[k] = v
			}
			_ = s.updateStoredPlan(ctx, plan.ID, output, nil)

			verification, err = s.verifier.Evaluate(ctx, intake, userMessage, taskResults, model, providerType)
			if err != nil {
				// The repair ran, but nothing checked it. Evaluate returns a zero
				// result here, so the verdict below reads "not passed" — which is
				// the honest answer for a repair no verifier ever looked at.
				log.Warn().Err(err).Str("plan_id", plan.ID.String()).
					Msg("post-repair verification failed to evaluate; repair left unverified")
				err = nil
				break
			}
			_ = s.updateStoredPlan(ctx, plan.ID, output, &verification)
		}

		// The verifier's last word, however the loop ended. A repair round that
		// was abandoned halfway and one that spent every iteration without
		// satisfying the verifier are the SAME thing to the stakeholder — work
		// the only judge in the system says is not right — so the plan must not
		// report them differently. Splitting them by whether some internal call
		// returned an error would grade the run on a distinction nobody outside
		// this function can see.
		//
		// evaluated gates it: a verifier that never produced a verdict (three
		// unparseable replies) is inconclusive, not a fail. That path already
		// stores no verification at all, and convicting a run on a judge that
		// never spoke would fail perfectly good runs whenever the verifier model
		// has a bad minute.
		verificationFailed = evaluated && !verification.Passed
	}

	finalContent = formatTaskResults(output, taskResults)

	// "completed" is this row claiming the run did what it was asked. It is the
	// only place in the system that can say otherwise: the session run is marked
	// completed whenever the pipeline returns without a Go error (see
	// session.Service), which a verification failure deliberately does not do.
	//
	// The verdict lands on PlanStatusIncomplete, not PlanStatusFailed. Nothing
	// died on this path: every subtask ran, the results exist and are returned
	// below — the only judge in the system says the goal was not met. "failed"
	// is reserved for the process stopping (the two exits above, and
	// board.Reconciler settling a run whose pod is gone). They used to share one
	// status, which is why a plan outcome read wrong and why "failed" was not
	// actionable.
	planStatus := domain.PlanStatusCompleted
	if verificationFailed {
		planStatus = domain.PlanStatusIncomplete
	}
	settlePlan(planStatus)

	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("orchestration_complete", map[string]string{"plan_id": plan.ID.String(), "status": planStatus})
	}

	if s.cfg.SynthesisEnabled {
		synthesized, synErr := s.synthesize(ctx, userMessage, output.Summary, finalContent, model, providerType)
		switch {
		case synErr == nil && synthesized != "":
			finalContent = synthesized
		case synErr != nil:
			// Synthesis is genuinely optional — it rewrites results that are
			// already complete and correct — so a failure keeps the raw
			// formatted results. What it must not do is fail INVISIBLY, which
			// is what it did: the error was assigned and never read, so a
			// synthesis step that could not run left no trace at all.
			log.Warn().Err(synErr).Str("plan_id", plan.ID.String()).
				Bool("permanent", errors.Is(synErr, domain.ErrHostExecutedUnservable)).
				Msg("result synthesis skipped; returning the unsynthesized task results")
		}
	}

	// The skipped-gate notice rides with the results, after synthesis so a
	// rewrite cannot drop it. A run whose only judge never ran must say so where
	// the person reading the answer will see it.
	if verificationSkipped != nil {
		finalContent = strings.TrimRight(finalContent, "\n") +
			"\n\n---\n⚠️ Verification did not run for this plan: " + verificationSkipped.Error()
	}

	// The verdict travels with the results. Stamping the plan row was not enough:
	// the board hand-off judges a run from this response alone, so a verdict that
	// stayed on the plan meant work the verifier rejected was promoted to review
	// regardless. Only a verdict actually reached is attached — an inconclusive
	// verifier leaves it nil, which callers must read as "unjudged", never as a
	// pass.
	out := domain.AgentResponse{
		Message: domain.Message{Role: domain.RoleAssistant, Content: finalContent},
	}
	if verificationFailed {
		verdict := verification
		out.Verification = &verdict
	}
	return out, nil
}

func (s *Service) persistPlan(ctx context.Context, runID uuid.UUID, output domain.PlannerOutput, verification *domain.VerificationResult) (domain.OrchestrationPlan, []domain.PlanTask, error) {
	planJSON, err := marshalStoredPlan(output, verification)
	if err != nil {
		return domain.OrchestrationPlan{}, nil, err
	}

	planTasks := plannerTasksToPlanTasks(output.Tasks)
	plan, err := s.catalog.CreatePlan(ctx, domain.OrchestrationPlan{
		RunID: runID, Status: domain.PlanStatusRunning, Summary: output.Summary, PlanJSON: planJSON,
	}, planTasks)
	if err != nil {
		return domain.OrchestrationPlan{}, nil, err
	}

	orderedPlanTasks, err := s.orderPlanTasks(ctx, plan.ID, output.Tasks)
	if err != nil {
		return domain.OrchestrationPlan{}, nil, err
	}
	return plan, orderedPlanTasks, nil
}

func (s *Service) appendPlanTasks(ctx context.Context, planID uuid.UUID, tasks []domain.PlannerTask) ([]domain.PlanTask, error) {
	planTasks := plannerTasksToPlanTasks(tasks)
	stored, err := s.catalog.AppendPlanTasks(ctx, planID, planTasks)
	if err != nil {
		return nil, err
	}
	taskByKey := make(map[string]domain.PlanTask, len(stored))
	for _, t := range stored {
		taskByKey[t.TaskKey] = t
	}
	ordered := make([]domain.PlanTask, 0, len(tasks))
	for _, t := range tasks {
		pt, ok := taskByKey[t.ID]
		if !ok {
			return nil, fmt.Errorf("stored task not found: %s", t.ID)
		}
		ordered = append(ordered, pt)
	}
	return ordered, nil
}

func (s *Service) orderPlanTasks(ctx context.Context, planID uuid.UUID, tasks []domain.PlannerTask) ([]domain.PlanTask, error) {
	storedTasks, err := s.catalog.ListPlanTasks(ctx, planID)
	if err != nil {
		return nil, err
	}
	taskByKey := make(map[string]domain.PlanTask)
	for _, t := range storedTasks {
		taskByKey[t.TaskKey] = t
	}
	ordered := make([]domain.PlanTask, 0, len(tasks))
	for _, t := range tasks {
		pt, ok := taskByKey[t.ID]
		if !ok {
			return nil, fmt.Errorf("stored task not found: %s", t.ID)
		}
		ordered = append(ordered, pt)
	}
	return ordered, nil
}

// setPlanStatus writes a plan's terminal status on a context that outlives the
// run. The status lands exactly when the run is ending, which for a drained pod
// is exactly when its context is already cancelled — those writes were dropped
// and the plan stayed "running" for good, which is how a finished board run
// kept reporting itself as live.
func (s *Service) setPlanStatus(ctx context.Context, planID uuid.UUID, status string) {
	pctx, cancel := persistCtx(ctx)
	defer cancel()
	if err := s.catalog.UpdatePlanStatus(pctx, planID, status); err != nil {
		log.Warn().Err(err).Str("plan_id", planID.String()).Str("status", status).
			Msg("orchestration: plan status write failed")
	}
}

func (s *Service) updateStoredPlan(ctx context.Context, planID uuid.UUID, output domain.PlannerOutput, verification *domain.VerificationResult) error {
	planJSON, err := marshalStoredPlan(output, verification)
	if err != nil {
		return err
	}
	return s.catalog.UpdatePlanJSON(ctx, planID, planJSON)
}

func plannerTasksToPlanTasks(tasks []domain.PlannerTask) []domain.PlanTask {
	planTasks := make([]domain.PlanTask, 0, len(tasks))
	for _, t := range tasks {
		agentID, _ := uuid.Parse(t.AgentID)
		skillIDs := make([]uuid.UUID, 0, len(t.SkillIDs))
		for _, sid := range t.SkillIDs {
			id, _ := uuid.Parse(sid)
			skillIDs = append(skillIDs, id)
		}
		planTasks = append(planTasks, domain.PlanTask{
			TaskKey: t.ID, Title: t.Title, Description: t.Description,
			AgentID: &agentID, SkillIDs: skillIDs, ToolNames: t.ToolNames, DependsOn: t.DependsOn,
			Status: domain.TaskStatusPending,
		})
	}
	return planTasks
}

func formatTaskResults(output domain.PlannerOutput, results map[string]string) string {
	// Single-task plans are conversational answers (e.g. "how many projects do
	// we have?"). Return the agent's reply verbatim — prefixing the planner
	// summary and a "## Title" heading leaks internal planning ("Stakeholder
	// wants to know…") to the stakeholder as if it were the answer.
	if len(output.Tasks) == 1 {
		if r, ok := results[output.Tasks[0].ID]; ok && strings.TrimSpace(r) != "" {
			return strings.TrimSpace(r)
		}
	}
	var sb strings.Builder
	sb.WriteString(output.Summary)
	sb.WriteString("\n\n")
	for _, t := range output.Tasks {
		if r, ok := results[t.ID]; ok {
			sb.WriteString("## ")
			sb.WriteString(t.Title)
			sb.WriteString("\n")
			sb.WriteString(r)
			sb.WriteString("\n\n")
		}
	}
	return strings.TrimSpace(sb.String())
}

func marshalStoredPlan(output domain.PlannerOutput, verification *domain.VerificationResult) ([]byte, error) {
	doc := storedPlan{PlannerOutput: output, Verification: verification}
	return goccyjson.Marshal(doc)
}

func (s *Service) synthesize(ctx context.Context, userMessage, summary, fullResults, model string, providerType domain.LLMProviderType) (string, error) {
	resp, err := s.llm.Chat(ctx, domain.AgentRequest{
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: "Summarize the orchestration results concisely for the user. Preserve key outcomes and file paths. Respond in the same language as the user request."},
			{Role: domain.RoleUser, Content: strings.Join([]string{
				"User request: " + userMessage,
				"Plan summary: " + summary,
				"Task results:\n" + fullResults,
			}, "\n\n")},
		},
		Model:        model,
		ProviderType: providerType,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Message.Content), nil
}

func (s *Service) Enabled() bool {
	return s.cfg.Enabled
}

func (s *Service) FastPathEnabled() bool {
	return s.cfg.FastPath
}

func (s *Service) ShouldOrchestrate(_ string, force bool) bool {
	if !s.cfg.Enabled {
		return false
	}
	return ShouldOrchestrate(force, s.cfg.FastPath)
}
