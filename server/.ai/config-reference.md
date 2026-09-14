# Configuration Reference

Config path defaults to `resources/config.yml`; override with `-config`. `${VAR}` is
substituted in the YAML and again after unmarshaling for nested fields. Secrets:
see `.env.example`.

## `llm`

| Key | Type | Default | Description |
|---|---|---|---|
| `base_url` | string | `""` | Bootstrap-only OpenAI-compatible `/v1` endpoint; providers are added in the UI and stored in the DB |
| `model` | string | `""` | Bootstrap-only model; normally empty — each agent carries its own |
| `api_key` | string | `""` | Bootstrap-only key for the fallback endpoint |
| `max_iterations` | int | `10` | Max agent loop iterations |
| `run_max_total_tokens` | int | `0` | Mid-run circuit breaker over `PromptTokens+CompletionTokens` of one run; ends it at the next iteration boundary through the exhausted-budget path. `0` disables |
| `timeout` | duration | `120s` | LLM HTTP timeout |

**Provider catalog** (`domain.AllLLMProviderDefinitions`, not YAML-configured): every declared
provider is `Available:true` today, including the four host-executed CLIs (`claude_code`,
`cursor_agent`, `antigravity`, `opencode` — see their own sections below). `local_runner` is a
different case: a real, working embeddings-only client that reaches a tenant's Mac through the
control-plane tunnel, deliberately absent from this catalog because there is nothing to connect —
see `.ai/architecture.md`'s "Embeddings reach the tenant's Mac" for the wire path.

## `server`

| Key | Type | Default | Description |
|---|---|---|---|
| `port` | int | `8080` | Listen port |
| `api_key` | string | `""` | Legacy single API key (`${SERVER_API_KEY}`) |
| `api_keys[]` | array | `[]` | Legacy config-file client keys; new keys are created in the UI and stored hashed. Desktop and cloud modes clear both fields |

## `storage`

| Key | Type | Default | Description |
|---|---|---|---|
| `postgres.dsn` | string | `""` | PostgreSQL DSN (`${POSTGRES_DSN}`) |
| `postgres.max_conns` | int | `10` | Pool size |
| `sessions.ttl` | duration | `24h` | Session expiration |

Empty `postgres.dsn` disables sessions, jobs, audit and RAG persistence.

## `jobs`

| Key | Type | Default | Description |
|---|---|---|---|
| `max_concurrent` | int | `3` | Worker pool size |
| `timeout` | duration | `10m` | Per-job execution timeout |

## `rag`

| Key | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable file upload and RAG |
| `storage_dir` | string | `./data/files` | On-disk file storage |
| `chunk_size` | int | `1000` | Characters per chunk |
| `chunk_overlap` | int | `200` | Overlap between chunks |
| `top_k` | int | `5` | Chunks injected into chat context |

The embedding model is not configured here: the UI's LLM settings pick it and it is
pinned on the shared client (`MultiProviderClient.SetEmbeddingModel`).

## `tools.default_policy`

Merged with per-request and per-API-key policies.

| Key | Type | Description |
|---|---|---|
| `allow_mcp_servers` | []string | Whitelist MCP server IDs |
| `allow_tools` | []string | Whitelist tool names (wildcards supported) |
| `deny_mcp_servers` | []string | Blacklist MCP server IDs |
| `deny_tools` | []string | Blacklist tool names (deny wins) |

## `tools.terminal`

| Key | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable `run_terminal` |
| `working_dir` | string | `"/tmp"` | Default working directory |
| `timeout` | duration | `60s` | Command timeout |
| `sandbox.mode` | string | `off` | `allowlist`, `blocklist`, or `off` |
| `sandbox.allowed_commands` | []string | `[]` | First-token allowlist |
| `sandbox.blocked_patterns` | []string | `[]` | Substring blocklist |
| `sandbox.restrict_working_dir` | bool | `false` | Restrict `working_dir` to the configured path |

## `tools.search`

| Key | Type | Description |
|---|---|---|
| `enabled` | bool | Enable `web_search` |
| `max_results` | int | Max search results |

No provider key: `web_search` queries DuckDuckGo and falls back to Bing when
DuckDuckGo answers with a bot wall.

## `tools.web`

| Key | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable `fetch_url` |
| `max_response_bytes` | int | `1048576` | Max response size |

### Outbound URL guard (`ALLOW_LOOPBACK_TOOL_URLS`)

Every tool that dials an LLM-chosen URL — `fetch_url`, browser tools, search
providers, MCP HTTP endpoints, health probes, job callbacks — goes through
`internal/platform/urlguard`: it refuses loopback, link-local, RFC1918, ULA, CGNAT,
multicast and the embedded-IPv4 forms `net.ParseIP` misses, pins the dial to the
vetted IP (so a DNS rebind cannot slip past) and re-checks every redirect hop. Its
transport sets `Proxy: nil`, because a proxy hides the real destination —
self-hosted users behind an HTTP proxy lose these tools.

`ALLOW_LOOPBACK_TOOL_URLS` is a **process env var, not a config key**, so a hosted
tenant cannot reach it. It opens loopback only; metadata, RFC1918 and CGNAT stay shut.

| Surface | Unset | Effect |
|---|---|---|
| `fetch_url`, search, MCP, health probes, job callbacks | loopback **blocked** | set true to allow |
| browser tools | loopback **allowed** | set false to block |

The browser default is inverted because the QA agent boots the app it just built on
`127.0.0.1:PORT` and already holds `run_terminal` in that pod.

## `tools.mobile`

Gerçek cihazı Appium üzerinden `mobile_*` tool setine bağlar (`mobile_launch_app`,
`mobile_tap`, `mobile_type_text`, `mobile_swipe`, `mobile_screenshot`,
`mobile_read_ui`, `mobile_wait_for`, `mobile_press_button`, `mobile_rotate`,
`mobile_unlock_device`, `mobile_release_device`).

| Anahtar | Env | Açıklama |
|---|---|---|
| `hub_url` | `MOBILE_APPIUM_HUB_URL` | Appium sunucusu (cluster'da servis adresi, yerelde `http://127.0.0.1:4723`) |
| `device_udid` | `MOBILE_DEVICE_UDID` | Hangi cihaz; kablosuz adb'de `100.x.y.z:5555` |
| `platform_version` | `MOBILE_PLATFORM_VERSION` | Opsiyonel capability. Simülatörlerde kullanılmaz — sürüm simctl runtime'ından türetilir (`iOS 17.4` → `17.4`) |
| `device_pin` | `MOBILE_DEVICE_PIN` | Ekran kilidi PIN'i; yalnızca Android |
| `auth_token` | `MOBILE_APPIUM_TOKEN` | Hub'a bearer token |
| `bridge_url` | `MOBILE_BRIDGE_URL` | adb sidecar'ı (`cmd/device-agent`); yalnızca `remote_adb` cihazları için |
| `bridge_token` | `MOBILE_BRIDGE_TOKEN` | Sidecar'a bearer token |

### Cihaz türleri (`kind`, migration 102)

Ayarlar API'sinde `kind` opsiyoneldir; boş gelmesi `remote_adb` demektir.

| `kind` | Nedir | `device_addr` | `device_udid` | Nerede çalışır |
|---|---|---|---|---|
| `remote_adb` (varsayılan) | adb bridge üzerinden fiziksel telefon | Tailnet `host:port` | Bridge'in ayırdığı loopback adresi | Her yerde |
| `ios_simulator` | Bu makinedeki `xcrun simctl` simülatörü | Simülatör UDID | Aynı UDID | Yalnızca Xcode'lu macOS |
| `android_emulator` | Bu makinedeki AVD | AVD adı | Açılıştaki adb serial (`emulator-5554`) | adb'si olan her host |

- Yerel türler yeni env istemez: `xcrun`, `adb`, `emulator` PATH'te, sonra
  `ANDROID_HOME`/`ANDROID_SDK_ROOT` ve Android Studio dizininde aranır. Hiçbiri
  olmayan host (yani her Linux node'u) hiçbir yerel cihaz bildirmez.
- iOS reddi host başına koşullu: `remote_adb` için hâlâ reddedilir (XCUITest
  Xcode'lu macOS ister), `ios_simulator` host'un gerçekten simülatörü var mı diye
  bakar.
- `GET /v1/settings/mobile-devices/local-catalog` host'un sürebileceklerini döner:
  `{"ios":[{"udid","name","runtime"}],"android":["Pixel_7_API_34"]}`. Kayıt bu
  kataloğa karşı doğrulanır — yerel cihaz elle yazılamaz. Hiçbiri yoksa cevap iki
  boş dizi ile `200`, `404` değil.
- `enabled` anahtarı yok: `hub_url` ya da `device_udid` boşsa tool'lar hiç
  kaydedilmez.
- **Cihaz paylaşılır.** Kirayı Appium yapar; dolu cihaz `blocked` sayılır, task park
  edilir, `DeviceSweeperInterval` (10 dk) boşalınca kaldığı kolondan devam ettirir.
  Kira 5 dk işlemsizlikte düşer, `mobile_release_device` hemen bırakır. Telefon,
  simülatör ve emülatör aynı havuzda ayrı kiralardır.
- **Tool seti türe göre değişmez**; tek fark `capabilitiesFor`'daki capability seti
  (`ios_simulator` → XCUITest + `appium:bundleId`; Android → `appPackage`,
  `autoGrantPermissions`, PIN ile kilit açma).
- `mobile_launch_app` yalnızca deploy target'ta kayıtlı paketi açar
  (`repository_deploy_targets.app_package`, artefakt için `app_url`); bu alanları
  insan yazar — ajanın yazabildiği guard, guard değildir.

Kurulum: [deploy/k8s/appium-android.yaml](../deploy/k8s/appium-android.yaml).

## `tools.boilerplate_catalog`

| Key | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable `search_boilerplate_catalog` |

Which repo it searches is the admin setting `boilerplate_catalog_repo`
(`GET/PUT /v1/settings`), not config. Empty by default; accepts `owner/repo`, `github.com/owner/repo`
or a full URL. Reads `<repo>/.ai/catalog.yaml` off `main` (falls back to `master`) on
every call — no restart needed.

## `tools.max_tool_output_chars`

| Key | Type | Default | Description |
|---|---|---|---|
| `max_tool_output_chars` | int | `16000` | Max chars of a tool result sent back to the LLM |

## `orchestration`

| Key | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Enable multi-agent orchestration |
| `fast_path` | bool | `true` | Skip orchestration unless `orchestrate: true` |
| `max_parallel_tasks` | int | `3` | Parallel subtask limit |
| `max_plan_tasks` | int | `10` | Max tasks per plan |
| `skill_retrieval_top_k` | int | `5` | Top skills for the planner via embedding search |
| `subtask_history_mode` | string | `isolated` | `isolated` or `full` session history per subtask |
| `dependency_output_max_chars` | int | `4000` | Truncate dependency results in subtask prompts |
| `synthesis_enabled` | bool | `false` | LLM synthesis of the final orchestration output |

## `context`

See [context-management.md](context-management.md).

| Key | Type | Default | Description |
|---|---|---|---|
| `max_tokens` | int | `32000` | Context budget |
| `reserve_output` | int | `4096` | Reserved for completion |
| `summarize_threshold` | int | `24000` | Rolling summary threshold |
| `keep_recent_messages` | int | `10` | Messages kept during trim |

## `mapping` / `indexer` / `graph`

See [repository-mapping.md](repository-mapping.md),
[codebase-indexing.md](codebase-indexing.md),
[dependency-graph.md](dependency-graph.md).

| Key | Default | Description |
|---|---|---|
| `indexer.query_rewrite` | `false` | Rewrites the task text into 2 extra code-search queries (multi-query retrieval) |
| `indexer.allowed_roots` | `[]` | Roots a repository or session may be pointed at, **in addition to** the calling tenant's own subtree of `storage.sessions.workspace_root`, which is always allowed |

`allowed_roots` only ever WIDENS the set. Empty means nothing extra — not
"anywhere", which is what it used to mean and what let `POST /v1/repositories/open`
index any readable directory on the pod for whoever asked. Set it only on a
self-hosted install that keeps its checkouts outside the managed workspace.

pgvector: migration 036 tries `vector` + `pg_trgm` (image `pgvector/pgvector:pg16`).
With them, workspace chunk search uses an HNSW index and text search a trigram+RRF
hybrid; without, the in-Go cosine fallback stays on.

## `embedding`

Paces embedding calls for every caller (indexer, RAG upload, query rewrite) — they
share one `MultiProviderClient` and therefore one quota. Unpaced, a repository index
fires one call per chunk and a hosted provider answers `429`, failing the index at the
first rejected chunk.

| Key | Type | Default | Description |
|---|---|---|---|
| `requests_per_minute` | int | `0` | Cap on embedding calls; `0` = unthrottled. Provider allows N req/s → set `N*60` |
| `max_retries` | int | `5` | Retries after `429`/`503`; negative disables |
| `retry_backoff` | duration | `2s` | First wait after a `429`, doubled per attempt; used when no `Retry-After` is sent |
| `max_retry_wait` | duration | `60s` | Cap on one wait, `Retry-After` included |
| `request_timeout` | duration | `90s` | Budget for one call; the indexer's per-chunk deadline derives from this plus retry waits |
| `query_cache_entries` | int | `2048` | LRU of query vectors keyed by (provider, model, exact text). `0` = default, negative disables |

A `Retry-After` (or backoff wait) also delays the *following* calls, so one rejection
slows the stream instead of producing a burst of new ones. The query cache sits
outside `RecordingClient` (`CachingEmbedder` → `RecordingClient` → provider), so a hit
is never billed; index-time chunks share it but `indexer/incremental.go`'s SHA-256
dedup keeps them from evicting query entries.

Code defaults suit a local embedding model (unthrottled). The shipped `config.yml` is
tuned for Mistral — `requests_per_minute: 55`, `max_retries: 8`, `request_timeout: 60s`
— whose free tier allows 1 req/s and sends no `Retry-After`. Throttling counts
requests, not tokens (55 req/min with `chunk_max_lines: 150` stays under the 500k
tokens/minute limit).

**Cloud mode's local model.** `domain.PinnedLocalEmbeddingModel` = `nomic-embed-text-v1.5`
(`domain.PinnedLocalEmbeddingDimensions` = `768`) is what `local_runner` always asks LM Studio
for over the Mac tunnel; not YAML-configured, not per-tenant. `embedding_llm_provider`/
`embedding_llm_model` empty ("auto") resolves to it whenever
`llmprovider.Service.SetControlPlane` is wired — see `.ai/architecture.md`.

## `tools.mcp_servers[]`

| Key | Type | Required | Description |
|---|---|---|---|
| `id` | string | yes | Server ID; tools namespaced as `mcp_<id>_<tool>` |
| `enabled` | bool | yes | Connect on startup |
| `transport` | string | yes | `stdio` or `http` |
| `command` | string | stdio | Executable |
| `args` | []string | stdio | Command arguments |
| `env` | map | no | Subprocess environment |
| `url` | string | http | MCP HTTP endpoint |
| `headers` | map | no | HTTP headers |
| `allowed_tools` | []string | no | Tool whitelist; empty = all |

## Podman PostgreSQL

```bash
export POSTGRES_PASSWORD=local_llm_secret
podman compose -f podman-compose.yml up -d postgres
export POSTGRES_DSN=postgres://local_llm:local_llm_secret@localhost:5432/local_llm?sslmode=disable
```

## Evolution (`evolution`)

Periodic reflections, KPI evaluation, impact tracking.

| Key | Default | Description |
|---|---|---|
| `enabled` | `false` | Master switch for the evolution ticker |
| `allow_web_research` | `false` | Reflection runs with `web_search`/`fetch_url` |
| `tick_interval` | `10m` | Ticker cadence (impact sweep, KPI sweep, due-reflection check) |
| `reflect_interval` | `24h` | Minimum gap between periodic reflections per agent |
| `revision_debounce` | `30m` | Minimum gap between revision-triggered mini-reflections |
| `impact_window` | `168h` | Observation window before/after a change |
| `min_events_for_impact` | `3` | Fewer after-events → `insufficient_data` |
| `max_skill_changes` / `max_rule_changes` | `3` | Per-reflection change caps |
| `max_skills_per_agent` | `25` | Standing skill budget; at budget a `create` is rejected (merge/update/delete first). Also caps the agent's own `create_skill` |
| `max_rules_per_agent` | `15` | Standing rule budget; same behaviour |
| `golden_gate` | `false` (config.yml ships `true`) | Golden suite before AND after applied changes; an independent judge keeps or rolls back the whole set |
| `max_memory_changes` | `5` | Per-reflection memory change cap |
| `memory_max_count` | `200` | Per-agent memory cap; oldest evicted |
| `evidence_max_chars` | `24000` | Reflection evidence truncation |
| `model` / `provider_type` | _(boş)_ | Reflection + golden eval override (ucuz model önerilir) |
| `judge_model` / `judge_provider_type` | _(boş)_ | Golden gate kararını veren bağımsız model; boşsa reflection modeli |

## Board quality gates (`board`)

| Key | Default | Description |
|---|---|---|
| `verification_enabled` | `false` | Run sonrası build/vet doğrulaması + otomatik düzeltme döngüsü |
| `verify_max_fix_attempts` | `2` | Düzeltme denemesi; hâlâ kırıksa task `in_progress`'e döner + system comment |
| `require_criteria_complete` | `false` | AC gate: kriterleri açık task `ready_for_qa`/`done`/`released`'e taşınamaz |
| `task_type_models` | `{}` | Task tipine göre model override, ör. `{analiz: küçük-model}` |
| `pipeline_gate_timeout` | `45m` | `code_review`'da build/test sonucu beklenen üst sınır; sonra reviewer yine atanır (`gate_reason=timeout`). **`pipelineMaxWait`'ten (30m) büyük olmalı** — aksi halde cevap vermek üzere olan canlı pipeline'dan vazgeçilir. Kalan 15m pod değişimi ve Actions kuyruğu payıdır |
| `pipeline_gate_interval` | `2m` | `PipelineGateSweeper`'ın GitHub'a yeniden sorma sıklığı; her bitmemiş pipeline bir round-trip. Boot'ta hemen bir kez süpürür — poller kaybetmenin en yaygın yolu restart'tır |

### QA pipeline (admin settings / repo alanları, `config.yml` dışı)

| Key | Kapsam | Default | Description |
|---|---|---|---|
| `pipeline_container_runtime` | Admin settings | `""` (`auto`) | `podman`/`docker`/`auto`. İkisi de yoksa container build stage'i atlanır. Değişiklik restart ister — `PipelineRunner` bunu bir kez çözer |
| `verify_command` | Repository | `""` | Inline verify gate komutu; boşsa auto-detect |
| `build_command` | Repository | `""` | QA pipeline build override; boşsa auto-detect ya da (Dockerfile varsa) container build |
| `test_command` | Repository | `""` | QA pipeline test override; boşsa auto-detect |

## Prod ops (`prod_ops`)

| Anahtar | Varsayılan | Açıklama |
|---|---|---|
| `monitor_enabled` | `true` | Deploy target'ların `health_url` yoklaması |
| `probe_interval` | `1m` | Üst üste 2 başarısız yoklama incident açar, ilk başarılı yoklama kapatır |

## Deploy ops (`deploy_ops`)

| Anahtar | Varsayılan | Açıklama |
|---|---|---|
| `monitor_enabled` | `true` | Actions deploy koşularını `deployment_runs`'a aynalar, konsol dispatch'lerini eşler, başarısız deploy'u incident'a çevirir. Kapalıyken rollback dönecek commit'i bulamaz ve release atfı yapılamaz |
| `poll_interval` | `2m` | Sweep başına en fazla bir GitHub çağrısı, her (repository × environment) için |
| `health_window` | `15m` | Başarılı deploy sonrası incident'in release eden task'a atfedildiği süre; `auto_rollback` bu pencerede tetiklenir. Kasten kısa — belirli bir kartın release'ini geri almaya yetki verir. `prodops/remedy.go`'daki 45 dk'lık genel korelasyon bundan bağımsızdır ve yalnızca tavsiye yazar |

## Store ops (`storeops`)

| Anahtar | Varsayılan | Açıklama |
|---|---|---|
| `poll_interval` | `5m` | Store app satırlarını süpürür: onboarding checklist, review durumu, imzalama varlıklarının yenilenmesi. Cipher (`MCP_SECRETS_KEY`/`SERVER_API_KEY`) yoksa monitör başlamaz, sunucu yine kalkar |

## Claude Code executor (`claude_code`)

Agents whose `provider_type` is `claude_code` run in a headless CLI session
(`claude -p`) on this host, on the subscription that CLI is signed in with — board
tasks and chat alike. Everything around a board run is unchanged (clone, branch,
grounding, verify gate, commit/PR, column advance).

| Key | Env | Default | Description |
|---|---|---|---|
| `binary` | `CLAUDE_CODE_BIN` | `claude` | The CLI to run, resolved on PATH at boot |
| `max_turns` | — | `100` | Turn budget for one session; a session that hits it returns what it has |
| `max_concurrent` | — | `3` | Simultaneous sessions. A run without a slot **waits** 10 minutes and then fails plainly — it is not parked, and the reconciler re-dispatches it when there is capacity |
| `setting_sources` | — | `project,local` | Which CLI settings files a session loads. The operator's own `~/.claude` (hooks, plugins, permission rules) is out: none of it was chosen for TaskTrooper and all of it would otherwise run inside board tasks. Set `user,project,local` only if this host authenticates through a user-level apiKeyHelper |
| `run_timeout` | — | `1h` | Deadline for one session — the only thing that ever gives up on a wedged CLI, since a subprocess has no provider timeout and the run's heartbeat keeps the row fresh. A plain run failure, never a quota park |

- **No `enabled` flag**: the switch is whether the binary is on PATH. Absent, no
  executor is registered and a `claude_code` run — board or chat — fails with one
  sentence (`domain.ErrHostExecutedProvider`) naming where it *can* run.
- **Model** is a `--model` alias from the curated `domain.ClaudeCodeModels()`
  (`""`, `fable`, `opus`, `sonnet`, `haiku`, `opus[1m]`, `sonnet[1m]`); the CLI does
  not validate it, so a typo costs a run. Empty is a first-class choice — `--model`
  is omitted entirely. See [API → GET /v1/models](api-spec.md#get-v1models).
- **Chat is multi-turn**: the CLI conversation id lives on `sessions.cli_session_id`
  (migration 103) and each turn is `--resume <id>`. A spent usage limit parks a board
  task but becomes a message in chat, naming the local renewal time.
- **The provider cannot be connected, tested, activated or used for embeddings** —
  all of those mean "dial this base URL with this key" and there is neither. It is
  selectable on an agent, which is where it means something.
- **Tool policy** governs the MCP half at execution (`DefinitionsForPolicy`) and the
  CLI's native half through `--tools`; the session is pinned with
  `--strict-mcp-config` and told its exact `mcp__tasktrooper__*` names. See
  [Architecture → the tool endpoint](architecture.md#the-tool-endpoint-mcp).
- **The MCP endpoint has no configuration of its own**: mounted whenever the executor
  is registered, on the server's own port, with one bearer token per run (or per chat
  turn), minted at the start and revoked at the end.
- **Child environment** is `internal/platform/childenv` (no `DATABASE_URL`,
  `INTERNAL_AUTH_KEY` or `MCP_SECRETS_KEY`) plus an explicit passthrough of
  `CLAUDE_CONFIG_DIR`, `CLAUDE_CODE_OAUTH_TOKEN` and the proxy variables.
  `ANTHROPIC_API_KEY` is deliberately **not** forwarded: it would hand a child a
  secret it was never given and move the session onto metered billing.

## Antigravity executor (`antigravity`)

Agents whose `provider_type` is `antigravity` run in a headless `agy -p` session on this
host (`internal/adapter/agentcli/antigravity`). No `max_turns`/system-prompt flag exists on
the CLI, so a run's whole history is folded into one prompt; MCP tools reach the session via
`.agents/mcp_config.json`, written into the workspace only for the run's lifetime.

| Key | Env | Default | Description |
|---|---|---|---|
| `binary` | `ANTIGRAVITY_BIN` | `agy` | Resolved on PATH at boot |
| `max_concurrent` | — | `3` | Simultaneous sessions |
| `run_timeout` | — | `1h` | Deadline for one session |

No `enabled`/`max_turns` flags — same "binary on PATH is the switch" rule as `claude_code`.

## Cursor executor (`cursor_agent`)

Agents whose `provider_type` is `cursor_agent` run in a headless `cursor-agent -p --force`
session (`internal/adapter/agentcli/cursor`). Its catalog (role + skills) was already
rendered into `.cursor/rules/*.mdc` by `agentfs.FlavorCursor`; MCP tools reach the session by
merging into `.cursor/mcp.json` — the same file the Cursor IDE reads — restored to its exact
original bytes when the run ends, since a repository may already have one committed.

| Key | Env | Default | Description |
|---|---|---|---|
| `binary` | `CURSOR_AGENT_BIN` | `cursor-agent` | Resolved on PATH at boot |
| `max_concurrent` | — | `3` | Simultaneous sessions |
| `run_timeout` | — | `1h` | Deadline for one session |

Auth is probed via `cursor-agent status`, not a real turn — the CLI reports it directly.

## OpenCode executor (`opencode`)

Agents whose `provider_type` is `opencode` run in a headless `opencode run` session
(`internal/adapter/agentcli/opencode`). No agentfs flavor beyond the shared `.claude/skills`
directory: OpenCode's own docs confirm it reads that layout, but nothing confirms a custom
role file becomes the ACTIVE persona from `run`, so role + rules go into the prompt instead.
MCP tools reach the session via `OPENCODE_CONFIG_CONTENT` (inline JSON env var) — nothing is
written to the workspace at all.

| Key | Env | Default | Description |
|---|---|---|---|
| `binary` | `OPENCODE_BIN` | `opencode` | Resolved on PATH at boot |
| `max_concurrent` | — | `3` | Simultaneous sessions |
| `run_timeout` | — | `1h` | Deadline for one session |

A known upstream bug can end a run without its final `step_finish` event; a clean exit with
real output is treated as success rather than a hard failure.

## Cloud (`cloud`) — reaching the user's Mac

| Env | koanf | Meaning |
|---|---|---|
| `INTERNAL_AUTH_KEY` | `cloud.internal_auth_key` | shared HMAC secret; signs `X-Internal-Auth` outbound and verifies `X-Internal-Tenant`/`Role`/`Actor` inbound |
| `CONTROL_PLANE_URL` | `cloud.control_plane_url` | tenant-manager's origin, e.g. `https://tasktrooper.ai`. **The one route to a member's Mac** — the tunnel is reverse, so this process asks the control plane to carry a call rather than dialling a laptop |

`cloud.RemoteWorkspaces()` is `control_plane_url != ""`, and it — not `cloud.enabled` — is what
decides where a board run executes, whether workspaces are prepared on a Mac, and whether the
filesystem tools are registered. **Unset is a complete configuration for a NON-cloud process**:
self-hosted and the desktop bundle hold the working copy and the `claude` binary on this same host.

**In cloud mode all three of these are refused at boot** (`validateCloudRequirements`), not logged:

| Env | Missing ⇒ the process becomes |
|---|---|
| `INTERNAL_AUTH_KEY` | an unauthenticated server in front of every tenant's data |
| `CONTROL_PLANE_URL` | a self-hosted single-machine server: filesystem tools over an empty `/data`, a `claude` binary the image does not ship, embeddings falling through to a chat provider and writing incomparable vectors into one index |
| `PUBLIC_BASE_URL` | unreachable from outside: no MCP callback address for a Mac, no webhook target for GitHub |

A non-https `PUBLIC_BASE_URL` is a **warning**, not a refusal: it disables the MCP callback
only (the runner refuses a bearer token over http) while webhooks still work, and a localhost
dev stack cannot have https. `TENANT_UID` is gone (migration 114: there is no per-tenant pod
to pin to).
