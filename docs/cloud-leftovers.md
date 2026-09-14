# Cloud leftovers

Code from the multi-tenant/control-plane era that is **inert** in local mode
rather than deleted: removing it would cascade further than the open-sourcing
pass was willing to go, and none of it can run — the values that would activate
it are never set. One line each, with why it stays.

## server

| Path / symbol | Why it stays |
|---|---|
| `internal/port/runner.go` | The Mac-transport contract (`RunnerTransport`, `WorkspacePreparer`, `ToolchainDetector`, `EmbeddingHostProbe`). No adapter implements it any more; the board still names the interfaces. |
| `internal/application/board/remote_workspace.go` | `Runner.prepareRemoteWorkspace`, reachable only when `SetWorkspacePreparer` has been called, which nothing does. |
| `internal/application/board/runner_sweeper.go` | Releases tasks parked on an offline Mac. `NewRunnerSweeper` has no caller. |
| `internal/application/llmprovider` — `SetControlPlane`, `SetControlPlaneEndpoint`, `ControlPlaneEmbeddingEntry`, `SetEmbeddingHost`, `EmbeddingsOnMemberMac`, `EmbeddingNeedsMemberMac` | Guarded by `runnerBaseURL`, which is never set, so every branch reads false. Unpicking them touches `Resolve`, `EmbeddingStatus`, `SetEmbedding` and `ListEmbeddingModels` at once. |
| `domain.LLMProviderLocalRunner` + `PinnedLocalEmbeddingModel` | Declared and listed, unreachable: nothing synthesises an entry for it. The factory returns a client that fails with a sentence pointing at `EMBEDDINGS_BASE_URL`. The pinned model name is still the one `EMBEDDINGS_BASE_URL` bootstraps. |
| `internal/application/notify` + `port/push.go` + `domain/push.go` + `adapter/store/postgres/{push,live_activity}.go` | The APNs adapter and the `/v1/push/*` routes are gone (iOS app is a non-goal); the service and its stores have no sender to reach and no route to fill them. |
| `tenant.Identity.ControlPlane`, `tenant.Role` / `RoleAdmin` / `RoleMember`, `roleMiddleware` | Every request is `RoleOwner`, so the role gate always passes. Kept because the tool policies and the board's role-scoped catalogs are written in terms of roles. |
| `internal/application/tenantboot` member mirror (`tenant_members`, `Members`, `IsMember`) | The roster endpoint is deleted and `Identity.UserID` is always empty, so nothing is mirrored. The board's assignee validation still reads the table. |
| RLS migrations (`114_multi_tenant_rls`, and the `tenant_id` columns/keys after it) | Kept as schema history. The embedded cluster runs as a superuser and bypasses the policies, which is correct with one tenant. `SET LOCAL app.tenant_id` still runs per store call and the `DEFAULT current_setting('app.tenant_id')` still fills the column. |
| `domain.BillingStatus.Managed`, `/admin/billing/*` | No control plane pushes a plan, so the plan is whatever this install wrote. The budget gate still reads it. |
| `.ai/*.md` | Written for the cloud deployment; still accurate about the domain model, the tool surface and the orchestration agents, stale wherever they mention tenants, pods or the gateway. |
