package domain

// BehaviourKey names one unit of engine behaviour a stage (or, for the three
// marked "(type)" below, a task type itself) can carry, replacing a hardcoded
// branch the engine used to read literally.
type BehaviourKey string

const (
	BehaviourDispatchSuspended        BehaviourKey = "dispatch_suspended"
	BehaviourRouteToSubscribers       BehaviourKey = "route_to_subscribers"
	BehaviourMergePROnEnter           BehaviourKey = "merge_pr_on_enter"
	BehaviourWatchDeployOnResume      BehaviourKey = "watch_deploy_on_resume"
	BehaviourBlockOnDependencies      BehaviourKey = "block_on_dependencies"
	BehaviourWaitForCI                BehaviourKey = "wait_for_ci"
	BehaviourEnsurePROnEnter          BehaviourKey = "ensure_pr_on_enter"
	BehaviourDetectMigrationOnEnter   BehaviourKey = "detect_migration_on_enter"
	BehaviourStageDeployOnEnter       BehaviourKey = "stage_deploy_on_enter"
	BehaviourHoldForHumanApproval     BehaviourKey = "hold_for_human_approval"
	BehaviourAutoEnter                BehaviourKey = "auto_enter"
	BehaviourAdvanceOnDiff            BehaviourKey = "advance_on_diff"
	BehaviourAdvanceOnDocument        BehaviourKey = "advance_on_document"
	BehaviourBuildVerify              BehaviourKey = "build_verify"
	BehaviourCommitOnFinish           BehaviourKey = "commit_on_finish"
	BehaviourReviewOnly               BehaviourKey = "review_only"
	BehaviourRequirePRForReview       BehaviourKey = "require_pr_for_review"
	BehaviourCriteriaSweep            BehaviourKey = "criteria_sweep"
	BehaviourRequireCriteriaComplete  BehaviourKey = "require_criteria_complete"
	BehaviourCriterionVerdict         BehaviourKey = "criterion_verdict"
	BehaviourForwardExit              BehaviourKey = "forward_exit"
	BehaviourRequireTestCases         BehaviourKey = "require_test_cases"
	BehaviourReviewVerdictSweep       BehaviourKey = "review_verdict_sweep"
	BehaviourRequireExecutionEvidence BehaviourKey = "require_execution_evidence"
	BehaviourRequireProductCheck      BehaviourKey = "require_product_check"
	BehaviourShowAllCriteria          BehaviourKey = "show_all_criteria"
	BehaviourReviewChainStage         BehaviourKey = "review_chain_stage"
	BehaviourEnforceReviewChain       BehaviourKey = "enforce_review_chain"
	BehaviourRequireReleaseDeploy     BehaviourKey = "require_release_deploy"
	BehaviourStripWriters             BehaviourKey = "strip_writers"
	BehaviourNoCodeReading            BehaviourKey = "no_code_reading"
	BehaviourNoReadFile               BehaviourKey = "no_read_file"

	// The three (type)-scoped behaviours live on TaskTypeDef.Behaviours.
	BehaviourDocumentDeliverable  BehaviourKey = "document_deliverable"
	BehaviourNoWorkspaceWrites    BehaviourKey = "no_workspace_writes"
	BehaviourRequireRepoGrounding BehaviourKey = "require_repo_grounding"
)

// BehaviourScope says whether a behaviour attaches to a stage or the task type
// itself.
type BehaviourScope string

const (
	BehaviourScopeStage BehaviourScope = "stage"
	BehaviourScopeType  BehaviourScope = "type"
)

// ParamType is the shape a behaviour param's value must parse as (params are
// plain strings on the wire).
type ParamType string

const (
	// ParamTypeColumn is a board column slug; UpdateColumns must not silently
	// orphan it.
	ParamTypeColumn ParamType = "column"
	ParamTypeString ParamType = "string"
	ParamTypeEnum   ParamType = "enum"
	ParamTypeBool   ParamType = "bool"
)

// ParamSpec describes one named parameter a behaviour reference may (or must)
// carry.
type ParamSpec struct {
	Name     string
	Type     ParamType
	Options  []string
	Required bool
}

// BehaviourSpec is one entry of BehaviourRegistry: everything validation and
// the UI's behaviour picker need without the engine package in scope.
type BehaviourSpec struct {
	Scope       BehaviourScope
	Label       string
	Description string
	Params      []ParamSpec
}

// BehaviourRegistry is the single source of truth for behaviour keys, scopes
// and params; workflow.ValidateStages and the behaviours endpoint read it.
var BehaviourRegistry = map[BehaviourKey]BehaviourSpec{
	BehaviourDispatchSuspended: {
		Scope: BehaviourScopeStage, Label: "Dispatch suspended",
		Description: "The dispatcher never starts a run for a task sitting in this stage.",
	},
	BehaviourRouteToSubscribers: {
		Scope: BehaviourScopeStage, Label: "Route to subscribers",
		Description: "A task entering this stage wakes every agent subscribed to the column instead of only its assignee.",
	},
	BehaviourMergePROnEnter: {
		Scope: BehaviourScopeStage, Label: "Merge PR on enter",
		Description: "Entering this stage wakes the merge flow for the task's pull request.",
	},
	BehaviourWatchDeployOnResume: {
		Scope: BehaviourScopeStage, Label: "Watch deploy on resume",
		Description: "A task resuming into this stage is watched for its production deploy.",
	},
	BehaviourBlockOnDependencies: {
		Scope: BehaviourScopeStage, Label: "Block on dependencies",
		Description: "The task parks here while an unfinished blocker (`blocks`) exists.",
		Params:      []ParamSpec{{Name: "refuse_move", Type: ParamTypeBool}},
	},
	BehaviourWaitForCI: {
		Scope: BehaviourScopeStage, Label: "Wait for CI",
		Description: "Dispatch into this stage waits for the pipeline gate to open.",
	},
	BehaviourEnsurePROnEnter: {
		Scope: BehaviourScopeStage, Label: "Ensure PR on enter",
		Description: "Entering this stage opens the task's pull request if it does not exist yet.",
	},
	BehaviourDetectMigrationOnEnter: {
		Scope: BehaviourScopeStage, Label: "Detect migration on enter",
		Description: "Entering this stage checks the branch diff for a schema migration.",
	},
	BehaviourStageDeployOnEnter: {
		Scope: BehaviourScopeStage, Label: "Stage deploy on enter",
		Description: "Entering this stage triggers a stage deploy, per the repository's test strategy.",
		Params:      []ParamSpec{{Name: "when", Type: ParamTypeEnum, Options: []string{"qa", "per_step"}, Required: true}},
	},
	BehaviourHoldForHumanApproval: {
		Scope: BehaviourScopeStage, Label: "Hold for human approval",
		Description: "An agent's approving move out of this stage is held for a human when the repository requires human review.",
	},
	BehaviourAutoEnter: {
		Scope: BehaviourScopeStage, Label: "Auto-enter",
		Description: "The dispatcher moves the task straight into the named column on assignment/wake.",
		Params: []ParamSpec{
			{Name: "to", Type: ParamTypeColumn, Required: true},
			{Name: "assignee_only", Type: ParamTypeBool},
		},
	},
	BehaviourAdvanceOnDiff: {
		Scope: BehaviourScopeStage, Label: "Advance on diff",
		Description: "A run that ends with a green build and a real diff is moved to the named column automatically.",
		Params:      []ParamSpec{{Name: "to", Type: ParamTypeColumn, Required: true}},
	},
	BehaviourAdvanceOnDocument: {
		Scope: BehaviourScopeStage, Label: "Advance on document",
		Description: "A run that ends with a document attached is moved to the named column automatically.",
		Params:      []ParamSpec{{Name: "to", Type: ParamTypeColumn, Required: true}},
	},
	BehaviourBuildVerify: {
		Scope: BehaviourScopeStage, Label: "Build verify",
		Description: "A run in this stage runs the build-gate fix round before finishing.",
	},
	BehaviourCommitOnFinish: {
		Scope: BehaviourScopeStage, Label: "Commit on finish",
		Description: "A run in this stage commits its diff on the way out.",
	},
	BehaviourReviewOnly: {
		Scope: BehaviourScopeStage, Label: "Review only",
		Description: "This stage is a review column: no diff is expected from a run here.",
	},
	BehaviourRequirePRForReview: {
		Scope: BehaviourScopeStage, Label: "Require PR for review",
		Description: "A run here refuses without an open pull request to review.",
	},
	BehaviourCriteriaSweep: {
		Scope: BehaviourScopeStage, Label: "Criteria sweep",
		Description: "This stage participates in the acceptance-criteria sweep loop.",
	},
	BehaviourRequireCriteriaComplete: {
		Scope: BehaviourScopeStage, Label: "Require criteria complete",
		Description: "Entering this stage is refused while an acceptance criterion is still open.",
	},
	BehaviourCriterionVerdict: {
		Scope: BehaviourScopeStage, Label: "Criterion verdict",
		Description: "A verdict recorded in this stage is attributed to the named review channel.",
		Params:      []ParamSpec{{Name: "channel", Type: ParamTypeEnum, Options: []string{"qa", "pm"}, Required: true}},
	},
	BehaviourForwardExit: {
		Scope: BehaviourScopeStage, Label: "Forward exit",
		Description: "A move out of this stage counts as a forward review exit for scoring.",
	},
	BehaviourRequireTestCases: {
		Scope: BehaviourScopeStage, Label: "Require test cases",
		Description: "This stage requires the task's generated test cases to be scored.",
	},
	BehaviourReviewVerdictSweep: {
		Scope: BehaviourScopeStage, Label: "Review verdict sweep",
		Description: "A finished review round in this stage is swept to a verdict and passed on.",
		Params:      []ParamSpec{{Name: "pass_to", Type: ParamTypeColumn, Required: true}},
	},
	BehaviourRequireExecutionEvidence: {
		Scope: BehaviourScopeStage, Label: "Require execution evidence",
		Description: "A QA round in this stage is rejected as ungrounded without evidence the product was actually run.",
	},
	BehaviourRequireProductCheck: {
		Scope: BehaviourScopeStage, Label: "Require product check",
		Description: "A UAT round in this stage is rejected without evidence the product was checked.",
	},
	BehaviourShowAllCriteria: {
		Scope: BehaviourScopeStage, Label: "Show all criteria",
		Description: "The prompt for this stage lists every acceptance criterion, not only open ones.",
	},
	BehaviourReviewChainStage: {
		Scope: BehaviourScopeStage, Label: "Review chain stage",
		Description: "This stage is a mandatory step of the type's review chain, required before done/released.",
		Params: []ParamSpec{
			{Name: "label", Type: ParamTypeString, Required: true},
			{Name: "remedy", Type: ParamTypeString, Required: true},
		},
	},
	BehaviourEnforceReviewChain: {
		Scope: BehaviourScopeStage, Label: "Enforce review chain",
		Description: "Entering this stage is refused unless the type's whole review chain has passed.",
	},
	BehaviourRequireReleaseDeploy: {
		Scope: BehaviourScopeStage, Label: "Require release deploy",
		Description: "Entering this stage is refused without a recorded successful production deploy.",
	},
	BehaviourStripWriters: {
		Scope: BehaviourScopeStage, Label: "Strip writers",
		Description: "Write/verdict tools are stripped from the policy in this stage, except any named in allow.",
		Params:      []ParamSpec{{Name: "allow", Type: ParamTypeString}},
	},
	BehaviourNoCodeReading: {
		Scope: BehaviourScopeStage, Label: "No code reading",
		Description: "Code-reading tools are stripped from the policy in this stage.",
	},
	BehaviourNoReadFile: {
		Scope: BehaviourScopeStage, Label: "No read file",
		Description: "read_file is stripped from the policy in this stage.",
	},
	BehaviourDocumentDeliverable: {
		Scope: BehaviourScopeType, Label: "Document deliverable",
		Description: "This task type's deliverable is a document, not a diff.",
	},
	BehaviourNoWorkspaceWrites: {
		Scope: BehaviourScopeType, Label: "No workspace writes",
		Description: "Runs on this task type never get workspace write tools.",
	},
	BehaviourRequireRepoGrounding: {
		Scope: BehaviourScopeType, Label: "Require repo grounding",
		Description: "A run on this task type is rejected as ungrounded without evidence the repository was actually read.",
	},
}
