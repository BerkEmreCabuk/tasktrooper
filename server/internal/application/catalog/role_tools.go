package catalog

import (
	"slices"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var (
	roleCodeTools = []string{
		"codebase_search",
		"grep_code",
		"get_repo_tree",
		"get_symbol_skeleton",
		"expand_symbol_context",
		"read_file",
	}
	roleWebTools = []string{
		"web_search",
		"fetch_url",
	}
	// Tool names are fixed contract with the browser adapter — the definitions
	// live there; this list must match them verbatim.
	roleBrowserTools = []string{
		"browser_navigate",
		"browser_screenshot",
		"browser_click",
		"browser_fill",
		"browser_read_dom",
		"browser_wait_for",
		"browser_set_viewport",
	}
	// The device twin of roleBrowserTools, and the same verbatim contract with
	// the mobile adapter. Held only by the roles that actually put hands on the
	// app: QA, the mobile developer, and PM for its UAT sign-off. Nothing here
	// is registered at all unless an operator attached a device, so granting it
	// to a role on an installation with no phone costs nothing.
	roleMobileTools = []string{
		"mobile_launch_app",
		"mobile_screenshot",
		"mobile_read_ui",
		"mobile_tap",
		"mobile_type_text",
		"mobile_swipe",
		"mobile_wait_for",
		"mobile_press_button",
		"mobile_rotate",
		"mobile_unlock_device",
		"mobile_release_device",
	}
	// The shell and the file writers travel together: an agent that may run
	// `sed -i` may call edit_file, and one that may not, may not either.
	roleShellTools = append([]string{
		"run_terminal",
	}, domain.WorkspaceWriteTools...)
	// What a black-box tester may read, and no more: enough to find the start
	// command, the port and the route it has to open, and nothing that invites
	// reading the implementation instead of exercising it.
	//
	// QA held the full roleCodeTools set and used it exactly as the tools
	// suggest: a round on a UI task opened with get_repo_tree and four
	// read_file calls into src/app, and the "test" that came out was a reading
	// of the diff. codebase_search / get_symbol_skeleton /
	// expand_symbol_context exist to understand code, which is the reviewer's
	// job — QA's evidence is a running product.
	roleQALookupTools = []string{
		"get_repo_tree",
		"grep_code",
		"read_file",
	}
	roleBoardReadTools = []string{
		"list_board_tasks",
		// The unblocked queue: which of list_board_tasks' backlog/todo rows is
		// actually startable right now, so a role choosing its next task does
		// not have to re-derive that from BlockedBy itself.
		"list_ready_tasks",
		"move_board_task",
		"update_board_task",
		"add_task_comment",
		// The read half. Without it an agent told to read the reviewer's
		// feedback reached for the only comment tool it had and posted one.
		"list_task_comments",
		// The same argument for documents, and a stronger one: since an
		// analysis stopped committing its spec to the repository, the spec and
		// the plan exist only as documents on the analiz task. Every role that
		// can be handed a task derived from one needs to be able to read them.
		"list_task_documents",
		"list_acceptance_criteria",
		"set_criterion_completed",
		// The other half of the same decision: a criterion that is deliberately
		// not being done leaves the list by saying why, instead of sitting
		// unticked and parking the task in front of the criteria gate.
		"cancel_criterion",
		// Read-only for every role: what QA actually exercised is context for
		// the developer fixing a rejection and for the PM signing the task off.
		"list_test_cases",
		"list_projects",
		"list_repositories",
		"get_board_summary",
		"list_team",
	}
	roleBoardClaimTools = []string{
		"claim_board_task",
	}
	roleBoardCreateTools = []string{
		"create_board_task",
		"add_task_document",
		// Paired with add_task_document deliberately: a role allowed to attach a
		// spec is the role that will later be asked to change it, and without the
		// update call the only answer it can give is a second document.
		"update_task_document",
		"attach_task_file",
	}
	// roleBoardDeleteTools: removing a task from the board for good. The PM owns
	// the backlog, so the PM owns what should not be in it — a duplicate, a task
	// opened by mistake, three tasks the user asked to be merged into one. Until
	// this existed the PM could only ever add: asked to delete, it created a
	// fourth task and left the three it was told to remove standing.
	roleBoardDeleteTools = []string{
		"delete_board_task",
	}
	roleWorkspaceManageTools = []string{
		"create_project",
		"update_project",
		"set_repository_projects",
	}
	roleMemoryTools = []string{
		"save_memory",
		"search_memory",
		"delete_memory",
	}
	roleSkillTools = []string{
		"load_skill",
		"create_skill",
	}
	// roleProfileTools: the code-facing roles maintain the per-repository
	// project profile (matches the 082 migration backfill for existing installs).
	roleProfileTools = []string{
		"update_project_profile",
	}
	// rolePRReadTools: reading the pull request a task is reviewed in — its
	// state, changed files, review comments and diff. Read-only, so every role
	// that reviews or reports on a task holds it.
	rolePRReadTools = []string{
		"get_task_pull_request",
	}
	// rolePRWriteTools: answering a reviewer on the PR, and pushing a change into
	// it. commit_task_changes goes to implementer roles only — a reviewer that
	// could commit would put its own name on the branch it is judging, which is
	// the same reason runner.go skips the post-run commit in review columns.
	// Matches the 088 migration backfill for existing installs.
	rolePRReplyTools = []string{
		"comment_on_pull_request",
	}
	rolePRCommitTools = []string{
		"commit_task_changes",
	}
	// rolePRMergeTools: landing the change. QA holds it and nobody else — the
	// developer must not merge its own branch, the architect reviews it, and the
	// PM signs off on the product rather than on the git history. QA is the last
	// role that actually ran the built thing, and `done` is the one column where
	// it may use this. Matches the 104 migration backfill for existing installs.
	rolePRMergeTools = []string{
		domain.MergePullRequestToolName,
	}
)

func developerToolPolicy() domain.ToolPolicy {
	tools := make([]string, 0, len(roleShellTools)+len(roleWebTools)+len(roleCodeTools)+len(roleBrowserTools)+len(roleBoardReadTools)+len(roleBoardClaimTools)+len(roleMemoryTools)+len(roleSkillTools))
	tools = append(tools, roleShellTools...)
	tools = append(tools, roleWebTools...)
	tools = append(tools, roleCodeTools...)
	// A green build is not the same claim as "the screen works", and until now a
	// developer could only make the first one: the browser tools were QA's and
	// the PM's, so a UI change was handed to review having never been rendered.
	// The developer already holds run_terminal in the same pod, so it can start
	// its own dev server on loopback — the browser guard allows 127.0.0.1 for
	// exactly this — open the changed page and look at it before handing it on.
	tools = append(tools, roleBrowserTools...)
	tools = append(tools, roleBoardReadTools...)
	tools = append(tools, roleBoardClaimTools...)
	tools = append(tools, roleMemoryTools...)
	tools = append(tools, roleSkillTools...)
	tools = append(tools, roleProfileTools...)
	// The developer is who a human talks to about "the PR you opened", and the
	// only role that may push a change into it.
	tools = append(tools, rolePRReadTools...)
	tools = append(tools, rolePRReplyTools...)
	tools = append(tools, rolePRCommitTools...)
	return domain.ToolPolicy{AllowTools: tools}
}

func productManagerToolPolicy() domain.ToolPolicy {
	tools := make([]string, 0, len(roleWebTools)+len(roleCodeTools)+len(roleBrowserTools)+len(roleBoardReadTools)+len(roleBoardCreateTools)+len(roleBoardDeleteTools)+len(roleWorkspaceManageTools)+len(roleMemoryTools)+len(roleSkillTools)+2)
	tools = append(tools, roleWebTools...)
	// Read-only code tools: the PM must verify real file/endpoint names before
	// filling technical_description on a task — no shell, reading only.
	tools = append(tools, roleCodeTools...)
	// pm_uat: the PM walks the critical flows on stage in person instead of
	// approving from QA's evidence alone; get_deploy_target resolves the stage
	// base_url (prod is off-limits, the rule layer says so separately).
	tools = append(tools, roleBrowserTools...)
	// The same walk on a phone when the deliverable is an app rather than a page.
	tools = append(tools, roleMobileTools...)
	// update_deploy_target only writes base_url/health_url: when UAT finds the
	// stage address recorded nowhere, the PM records the one it actually
	// browsed instead of leaving the next role to rediscover it.
	tools = append(tools, "get_deploy_target", "update_deploy_target")
	tools = append(tools, roleBoardReadTools...)
	tools = append(tools, roleBoardCreateTools...)
	tools = append(tools, roleBoardDeleteTools...)
	tools = append(tools, roleWorkspaceManageTools...)
	// pm_uat: the PM records an own verdict per acceptance criterion instead
	// of trusting the implementer's checkmarks.
	tools = append(tools, "review_criterion")
	// Read-only: the PM answers "what actually shipped in this task?" from the
	// PR rather than from the implementer's summary of it.
	tools = append(tools, rolePRReadTools...)
	tools = append(tools, roleMemoryTools...)
	tools = append(tools, roleSkillTools...)
	return domain.ToolPolicy{AllowTools: tools}
}

func architectToolPolicy() domain.ToolPolicy {
	tools := make([]string, 0)
	tools = append(tools, roleShellTools...)
	tools = append(tools, roleWebTools...)
	tools = append(tools, roleCodeTools...)
	tools = append(tools, roleBoardReadTools...)
	tools = append(tools, roleBoardCreateTools...)
	tools = append(tools, roleBoardClaimTools...)
	tools = append(tools, "get_pipeline_status")
	// The architect IS the code reviewer: it reads the PR it is judging and
	// answers threads on it. It does not get commit_task_changes — see
	// rolePRCommitTools.
	tools = append(tools, rolePRReadTools...)
	tools = append(tools, rolePRReplyTools...)
	tools = append(tools, roleMemoryTools...)
	tools = append(tools, roleSkillTools...)
	tools = append(tools, roleProfileTools...)
	return domain.ToolPolicy{AllowTools: tools}
}

func qaToolPolicy() domain.ToolPolicy {
	tools := make([]string, 0, len(roleShellTools)+len(roleWebTools)+len(roleQALookupTools)+len(roleBrowserTools)+len(roleMobileTools)+len(roleMemoryTools)+len(roleSkillTools)+10)
	tools = append(tools, roleShellTools...)
	tools = append(tools, roleWebTools...)
	// Not roleCodeTools: QA is black box. See roleQALookupTools.
	tools = append(tools, roleQALookupTools...)
	tools = append(tools, roleBrowserTools...)
	tools = append(tools, roleMobileTools...)
	tools = append(tools, "list_board_tasks", "list_ready_tasks", "move_board_task", "add_task_comment", "list_task_comments", "list_task_documents", "list_acceptance_criteria", "review_criterion", "list_repositories")
	// The round itself, written on the card: every case QA derived from the
	// request (not only from the criteria), its verdict and its evidence —
	// including the cases considered and rejected as invalid. QA is the only
	// role that WRITES these; everyone else reads them.
	tools = append(tools, "list_test_cases", "record_test_cases", "set_test_case_result")
	// Pipeline sonucu ve stage adresi: otomasyon suite'inin yeşil olduğunu
	// doğrulamak (get_pipeline_status) ve stage'de test ederken base_url'i
	// çözmek (get_deploy_target) için. Prod'a istek atmak her koşulda yasak —
	// kural katmanı bunu ayrıca söylüyor. update_deploy_target yalnızca
	// base_url/health_url yazar: ilk deploy'dan sonra ortamın gerçekte
	// cevapladığı adres kayda geçsin diye.
	tools = append(tools, "get_pipeline_status", "get_deploy_target", "update_deploy_target")
	// QA tests the change the PR carries, so it reads the PR. Read-only.
	tools = append(tools, rolePRReadTools...)
	// …and, once the board has signed the task off, merges it. The one
	// non-read-only thing QA does to code, available to it only in `done`:
	// RestrictToolsForVerdictColumn takes it away in in_qa/ready_for_qa, where
	// QA's job is a verdict on the change rather than the landing of it.
	tools = append(tools, rolePRMergeTools...)
	// …and then watches what the merge shipped, and undoes it when it breaks.
	// QA and nobody else, for the same reason the merge is QA's: it is the role
	// that last exercised the built product, it is the role `done` wakes, and
	// the deploy it watches is the deploy of the merge it just made. A developer
	// that could roll production back could undo a release it was never asked
	// about. Two of the three only read; RestrictToolsForVerdictColumn takes the
	// third (the rollback) away everywhere except `done`/`released`.
	tools = append(tools, roleReleaseWatchTools...)
	tools = append(tools, roleMemoryTools...)
	tools = append(tools, roleSkillTools...)
	return domain.ToolPolicy{AllowTools: tools}
}

// roleReleaseWatchTools are the after-the-merge tools: what happened in
// production to the commit this task merged, the log behind it, and the
// rollback. Backfilled to existing installs by migration 105.
var roleReleaseWatchTools = []string{
	domain.DeployStatusToolName,
	domain.DeployLogsToolName,
	domain.RollbackReleaseToolName,
}

// roleToolGrant is one tool that was added to a role policy AFTER installs
// existed, plus the tool whose presence proves the role already does that job.
//
// It exists because the two ways a new tool used to reach an existing install
// are both closed. ToolPolicy is not reconciled on restart — an admin's
// customization has to survive — and before migration 133 a migration could
// not backfill it either: `agents` carried FORCE ROW LEVEL SECURITY and the
// migration runner had no tenant in scope, so migration 073's UPDATE pattern
// matched zero rows in every tenant. Without this, an install that upgraded
// would read a refusal naming `record_test_cases` from an agent that does not
// hold it.
//
// `requires` is what keeps this from being policy reconciliation by another
// name: the grant lands only where the paired capability is already granted, so
// a policy an admin narrowed (QA without review_criterion, a developer without
// set_criterion_completed) is left exactly as narrow as they made it. A grant
// is never a removal, and nothing here touches a tool the seed did not add.
type roleToolGrant struct {
	tool     string
	requires string
	// roles empties to "every role that satisfies requires".
	roles []string
}

var roleToolGrants = []roleToolGrant{
	// Cancelling a criterion is the other half of ticking one (migration 131).
	{tool: "cancel_criterion", requires: "set_criterion_completed"},
	// Reading the round QA recorded: context for the developer fixing a
	// rejection and for the PM signing the task off (migration 130).
	{tool: "list_test_cases", requires: "list_acceptance_criteria"},
	// Writing it is QA's alone — the role whose deliverable IS the round.
	{tool: "record_test_cases", requires: "review_criterion", roles: []string{"qa-agent"}},
	{tool: "set_test_case_result", requires: "review_criterion", roles: []string{"qa-agent"}},
}

// grantMissingRoleTools adds the post-install tool grants this agent qualifies
// for, and reports whether it changed anything.
func grantMissingRoleTools(agent *domain.Agent) bool {
	var changed bool
	for _, grant := range roleToolGrants {
		if len(grant.roles) > 0 && !slices.Contains(grant.roles, agent.Name) {
			continue
		}
		if !slices.Contains(agent.ToolPolicy.AllowTools, grant.requires) {
			continue
		}
		if slices.Contains(agent.ToolPolicy.AllowTools, grant.tool) {
			continue
		}
		agent.ToolPolicy.AllowTools = append(agent.ToolPolicy.AllowTools, grant.tool)
		changed = true
	}
	return changed
}

func toolPolicyEqual(a, b domain.ToolPolicy) bool {
	return slices.Equal(sortedCopy(a.AllowTools), sortedCopy(b.AllowTools)) &&
		slices.Equal(sortedCopy(a.AllowMCPServers), sortedCopy(b.AllowMCPServers))
}

func sortedCopy(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := append([]string(nil), items...)
	slices.Sort(out)
	return out
}

// mobileDeveloperToolPolicy is the developer policy plus the device tools.
//
// It exists because the three developer roles share developerToolPolicy, and
// the device is one shared phone: handing it to the backend and frontend
// developers as well would put three roles in a queue for hardware only one of
// them has any use for. The mobile developer needs it for the same reason QA
// does — a layout bug reported against a real screen is fixed by looking at
// that screen, not at a widget test.
func mobileDeveloperToolPolicy() domain.ToolPolicy {
	base := developerToolPolicy()
	tools := make([]string, 0, len(base.AllowTools)+len(roleMobileTools))
	tools = append(tools, base.AllowTools...)
	tools = append(tools, roleMobileTools...)
	base.AllowTools = tools
	return base
}
