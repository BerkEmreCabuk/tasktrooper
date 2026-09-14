# Orchestration Agents

Skills and orchestrator rules are **agent-scoped**. Each skill and rule belongs to exactly one agent via `agent_id` foreign key. There is no global skills or rules catalog.

## Data model

- `skills.agent_id` → `agents.id` (NOT NULL, ON DELETE CASCADE)
- `orchestrator_rules.agent_id` → `agents.id` (NOT NULL, ON DELETE CASCADE)
- Unique constraint per agent: `(agent_id, name)` on both tables
- `agent_skills` junction table removed; `Agent.skill_ids` in API responses is derived from `ListSkillsByAgent`

## Default role agents

On startup, `EnsureRoleAgents` creates any missing role agents (by name) and fills in missing skills/rules for partially seeded agents. Embedding failures during seed do not block skill creation.

| Name | Subagent type | Purpose |
|------|---------------|---------|
| `backend-developer` | `backend-engineer` | Go/Fiber/hexagonal API, DB, tests |
| `frontend-developer` | `frontend-engineer` | React/Vite/Tailwind UI |
| `mobile-developer` | `mobile-dev-engineer` | React Native / Wails / mobile UX |
| `product-manager` | `generalPurpose` | Backlog, requirements, board tools |
| `qa-agent` | `generalPurpose` | Test plans, QA columns, read-only code tools |

Each role agent is seeded with 6–17 skills and 3–10 rules. Skill embeddings are computed on insert via the catalog service. Seed reconciliation updates existing skill/rule content on restart when the markdown under `internal/application/catalog/seeddata/` changes; renamed/removed entries must be listed in `deprecatedRoleSkills`/`deprecatedRoleRules` (`seed.go`) to be deleted from existing installs. Tool policies are applied on agent CREATE only — admin customizations survive restarts, so policy additions reach existing installs via the admin UI.

### Seeded models

| Field | Value | Used for |
|-------|-------|----------|
| `provider_type` | `claude_code` | the CLI session a run is handed to |
| `model` | `sonnet` | every run and every subtask by default |
| `model_heavy` | `opus` | subtasks the planner rates `hard`, plus self-reflection and the golden judge |

Declared as one triple in `role_seed.go` (`roleAgentProvider` / `roleAgentModel` / `roleAgentModelHeavy`): a model name is only valid for the provider it was picked from, so the seed never writes a name without its provider. Both are aliases from `domain.ClaudeCodeModels()` rather than pinned ids, so a model release cannot stale them silently.

`fillRoleAgentModels` (`seed.go`) reaches existing installs on restart instead of a migration. It fills the pair only when **both** `model` and `model_heavy` are empty and `provider_type` is `claude_code` or still empty — either name set, or any other provider, and the agent is left untouched. The provider is stamped only where the `checkHostExecutor` probe says the CLI can run here; a host with no runner keeps empty models, because `CreateAgent` refuses that provider there and the seed would lose all six agents.

### QA test akışı (seed skill seti)

**Bu iterasyonda QA yalnız MANUEL test yapıyor (2026-08-12).** Otomasyon fazı ertelendi — silinmedi: 4 skill (`e2e-automation-project`, `automation-pipeline-integration`, `test-doubles-wiremock`, `test-database-seeding`) `mdSkillDisabled`, 2 kural (`e2e-automation-project`, `deterministic-test-env`) `disabledRule` ile **kapalı** seed'leniyor; dosyaları ve satırları yerinde, prompt'a girmiyorlar (builder yalnız enabled olanları enjekte eder). `manual-before-automation` kuralının yerini `manual-only-testing` aldı (eski isim `deprecatedRoleRules`'ta). Seed artık `Enabled` alanını da reconcile ediyor — yoksa yalnız bayrağı değişen bir skill mevcut kurulumlara hiç ulaşmıyordu; geri açmak için `mdSkillDisabled`→`mdSkill` yeterli, migration gerekmez. Yeni `mobile-manual-testing` skill'i mobil task'ların hangi katmanının bu ortamda gerçekten çalıştırılabildiğini (build, repo'nun kendi lint/test'leri, akışın API tarafı) ve çalıştırılamayanın nasıl **onaylanmadan** raporlanacağını tanımlıyor — Linux runner'da simulator yok. Sıradaki iş `todo.md`'de.

`qa-agent` aşağıdaki iki fazlı akış için tasarlandı; ikinci faz şu an kapalı. Prod'da asla test koşmaz (`never-test-in-prod` kuralı):

1. **Manuel doğrulama** — senaryolar koda bakılmadan task tanımı + AC'lerden çıkarılır (`scenario-plan-first`), ortam seçilir (`test-environment-selection`: varsayılan task workspace'te local boot; repo stage'e göre ayarlıysa `get_deploy_target` ile stage `base_url`). Backend: API + worker boot, gerçek istek + yan etki kontrolü (`backend-manual-testing`, `worker-job-testing`). Frontend: headless Chromium ile akış sürme + desktop/mobil ekran görüntüsü + görsel checklist (`frontend-manual-testing`, `ui-visual-evidence` kuralı).
2. **Otomasyon** — manuel geçişten sonra aynı senaryolar repo başına ayrı otomasyon test projesine (`qa-automation/`; api/worker/ui suite'leri) test olarak eklenir (`e2e-automation-project`), dış bağımlılıklar WireMock, DB Testcontainers (`test-doubles-wiremock`, `test-database-seeding`), ve suite repo'nun kendi pipeline'ına bağlanır (`automation-pipeline-integration`, repo `test_command`). Verdict üç bacak ister: manuel kanıt + suite'e eklenmiş testler + yeşil pipeline (`qa-verify-before-verdict`).

**QA run'ı çalıştırmadan bitemez (`isUngroundedQA`, 2026-08-12).** `analiz-read-code-first` kapısının QA karşılığı, aynı sebeple: bir QA run'ı senaryo listesini gelecek zamanda yazdı ("bu senaryoları uygulayacağım ve her birini doğrulayacağım"), tek board move'u dışında hiçbir şey çağırmadı, `completed` damgalandı — orchestration verifier de tutarlı bir plan gördüğü için `GEÇTİ` dedi.

- `in_qa`/`ready_for_qa` kolonundaki bir run, `domain.QAExecutionTools`'tan (`run_terminal` + `browser_*`) en az biri **başarıyla** çalışmadan tamamlanamaz; aksi hâlde run `failed` yazılır, task'a red gerekçesi + reddedilen metin yorum olarak düşer ve reconciler yeni bir QA denemesi dispatch eder (`maxConsecutiveFailedRuns`=3 ile sınırlı).
- `review_criterion` / `add_task_comment` / `get_pipeline_status` **kanıt sayılmaz**: verdict test edilen iddianın kendisidir, kendi kanıtı olamaz. Kod okuma araçları da sayılmaz — QA black-box.
- `analiz` task'ı ve soruyla biten run (`Clarification`) muaf.
- Kural katmanı aynı şeyi söylüyor: `qa-execute-in-this-run` (öncelik 100) + `qa-agent.md` 1. adım.

**Verifier artık planı sonuç saymıyor.** `buildVerifierSystemPrompt`'a eklenen bölüm: gelecek zamanda yazılmış sonuç plandır; hedef test/verify/review ise çalıştırılmış kanıt (koşulan komut + gözlenen çıktı, ekran görüntüsü) **deliverable'ın kendisidir** ve yokluğu material gap'tir. Board delegasyonu için yazılmış "analiz gibi okunan sonuç başarısızlık değil / kanıtlanamayan run'ı geçir, review chain yargılar" muafiyeti burada geçmiyor: bir QA run'ı için **review chain'in kendisi o run**.

**QA verdict kapısı artık ileri giden her çıkışı tutuyor** (`criteriaReviewGate`). Önce yalnız `ready_for_qa|in_qa → pm_uat` ve `pm_uat → human_uat|done|released` kapılıydı; QA `in_qa`'dan doğrudan `done`'a taşıyınca hiçbir kriter verdict'i aranmıyordu — kaçış buradan oldu. Artık `isForwardReviewExit` (pm_uat/human_uat/done/released) hedeflerinin hepsi, task'ın çıktığı review kolonunun rolüne göre (QA veya PM) tam verdict ister. `need_revision` ve geri dönüşler kapısız. Kriteri olmayan task ve `require_criteria_complete=false` yine no-op.

Mobil otomasyon şimdilik kapsam dışı. Tenant imajı (`deploy/docker/agent-server.Dockerfile`) bu akış için headless Chromium (`CHROME_BIN=/usr/bin/chromium`, `--no-sandbox` gerekir) içerir; Playwright/Puppeteer tarayıcı indirmez (skip-download env'leri set).

Legacy agents (`general-coder`, `shell-runner`, `code-explorer`) are removed by migration `017_agent_scoped_catalog`.

## Admin API

Nested under agents:

| Method | Path |
|--------|------|
| GET/POST | `/admin/agents/:id/skills` |
| GET/PUT/DELETE | `/admin/agents/:id/skills/:skillId` |
| GET/POST | `/admin/agents/:id/rules` |
| GET/PUT/DELETE | `/admin/agents/:id/rules/:ruleId` |
| GET/POST | `/admin/agents/:id/tech-stacks` |
| PUT/DELETE | `/admin/agents/:id/tech-stacks/:stackId` |

Global `/admin/skills` and `/admin/orchestrator-rules` are removed.

## Tech stacks

`agent_tech_stacks` (migration 124) is one agent's list of technologies, and
`skills.tech_stack_id` files each skill under at most one of them. NULL is a
**general** skill — it holds whatever the code is written in; a set one only
applies inside that technology. Deleting a stack does not delete its skills:
the FK nulls them back to general. `POST/PUT .../skills` carry `tech_stack_id`;
a stack belonging to another agent is refused with 400
`tech stack belongs to another agent`. `UpdateSkillRequest` replaces the field
like every other, so omitting it files the skill as general.

## Agent templates

`agent_templates` (migration 030) stores read-only agent snapshots: agent fields + `skills`/`rules` JSONB. The five role agents are seeded as built-in templates by `EnsureRoleAgents` (idempotent upsert by name).

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/admin/agent-templates` | List templates |
| POST | `/admin/agents?template_id=<uuid>` | Copy-on-create agent from template (body fields override name/prompt/model etc.) |
| POST | `/admin/agents/:id/template` | Save an existing agent as a template (upsert by name) |

Editing an agent created from a template never mutates the template. UI: AgentsPage offers "Şablondan / Sıfırdan" creation and a per-agent "şablon olarak kaydet" action.

## Planner and executor

- Planner loads per-agent skills and enabled rules into the system prompt
- Semantic skill search results include `agent_id`; planner validation rejects `skill_id` values that do not belong to the task's `agent_id`
- Executor injects **all enabled skills of the assigned agent** into every subtask's index (`enabledAgentSkills`), exactly like a board run. Disabled skills reach neither the index nor `load_skill`. The plan's `skill_ids` survive only as an emphasis line in the task prompt (`plannedSkillFocus`); ids naming a disabled or foreign skill are ignored, not fatal

### Subtask yetkisi: `tool_names` yalnız board yazımını çitler

`tool_names` eskiden `IntersectToolPolicy` ile agent'ın tüm policy'sini kesiyordu. `[claim_board_task, move_board_task, add_task_comment]` bildiren bir subtask böylece repoyu okuyamadan çalıştı, ajan insana "dosyalar nerede" diye sordu — `clarificationGate` de tam bu durumda (okuma aracı yok) soruyu geçiriyor.

Artık `domain.RestrictToPlannedTools`: bildirim **sadece** board-write araçlarına (`domain.BoardWriteTools`: `create_board_task` / `move_board_task` / `update_board_task`) karar verir; ajanın yapılandırılmış diğer araçları (kod okuma, shell, web, `load_skill`) her subtask'ta kalır. Duplicate-record garantisi bozulmaz, çünkü bildirilmeyen board-write aracı hâlâ verilmez. `startedNotFinishedReason` niyet sinyali olarak hâlâ planın bildirimini okur, çözülmüş policy'yi değil — "DE-1'i board'a taşı" subtask'ı taşımayı yapınca biter.

### Subtask çalışma dizini: checkout varsa onun içinde çalışılır

Kod araçları ve `run_terminal` kökü `registry.EffectiveWorkspaceDir` ile çözer; subtask workspace'i set edilmişse **o** kazanır. Board run repoyu `task-<id>` altına klonlayıp task branch'ini ajan başlamadan çıkarıyor, ama executor bu checkout'un **içine** `task-<id>/<TASK_KEY>` diye boş bir klasör açıp araçları oraya bağlıyordu. Sonuç: `get_repo_tree` boş dönüyor, ajan board runner'ın prompt'undaki gerçek repo yolunu kullanıp `find <repo>/src` diye tahmin yürütüyor, 30 iterasyon sonunda "proje yapısını inceleyemedik, zamanımız kalmadı" diyordu. İki farklı `SubtaskWorkspaceNote` (runner'ınki repo kökü, executor'ınki boş klasör) aynı prompt'ta çelişiyordu.

Klonun kendisi zaten doğru geliyordu: `ensureWorkingCopy` çalışma kopyası yoksa `Repository.RemoteURL`'den `CloneRepo` ile (token auth) çekiyor, sonra `EnsureTaskWorkspace` task branch'ini çıkarıyor — bu bir hard gate, başarısız olursa ajan hiç başlamıyor. Eksik olan klon değil, araçların hangi dizine baktığıydı.

`resolveSubtaskWorkspace`: `workspace.IsRepoCheckout` (`.git` var mı) doğruysa subtask doğrudan checkout'ta çalışır; yoksa (bare workspace üzerinde sohbet orkestrasyonu) eski per-subtask izolasyonu korunur. Yan fayda: ajanın yazdığı dosyalar artık klonun kendisinde, yani board runner'ın commit'lediği ağaçta.

### Bir istek = bir board kaydı (duplicate guards)

Sohbette tek bir istek için birden fazla board task açılmasının dört ayrı yolu vardı; her biri ayrı bir kapıyla kapatıldı:

| Yol | Kapı |
|---|---|
| Intake/planner, konuşmada zaten açılmış kaydı görmüyordu → "taşı" isteği "oluştur" olarak planlanıyordu | `pipelineConversationHistory` artık session action ledger'ı (`domain.IsSessionActionDigest` olan system mesajı) koruyor; intake ve planner prompt'ları da "bu kayıtlardan biriyse o id üzerinde işlem yap" kuralını taşıyor |
| Planner tek işi "konumu belirle" → "görseli belirle" → "ekle" diye bölüp her adıma bir task açtırıyordu | `validateSingleTaskCreator`: plan **genelinde** en fazla bir subtask board task açabilir (`validateDisjointWrites` yalnız aynı `parallel_group` içine bakar, `depends_on` ile zincirleyince deliniyordu). Prompt'ta ayrıca "bir subtask karar/bilgi değil DEĞİŞİKLİK üretir" kuralı var: sadece stakeholder'ın bilebileceği şey → `ready=false` + `questions`; repo/board/canlı siteden bakılabilecek şey → uygulayan subtask kendi araçlarıyla bakar |
| `tool_names` boş bırakılan subtask hiçbir kontrole görünmüyordu ama executor ona agent'ın tüm policy'sini veriyordu | `subtaskMayCreateTasks` boş `tool_names`'i agent'ın `ToolPolicy`'sine karşı çözüyor (`domain.ToolAllowedByPolicy`). Seed'de yalnız `product-manager`'da `create_board_task` var, dolayısıyla developer/QA subtask'ları etkilenmiyor |
| Verifier, doğru delege edilmiş run'ı "özellik hâlâ canlıda değil" diye `passed:false` işaretliyor, replanner de ikinci (analiz) task'ı açıyordu | `buildVerifierSystemPrompt` artık run'ın teslimatının **kayıt** olduğunu, board gecikmesinin (canlıda değil / kod değişmedi / QA koşmadı) bulgu sayılmadığını söylüyor |
| Repair planı kendi başına geçerli olduğu için ikinci creator ekleyebiliyordu | `validatePlannerOutput` artık `priorTasks` alıyor; replanner orijinal planın subtask'larını geçiriyor, yani creator sayımı run genelinde yapılıyor |

Ek olarak board araçlarının `task_id` parametresi artık UUID **veya** board key (`DE-1`) kabul ediyor (`ToolKit.resolveTaskRef`). Eskiden key gönderen model `invalid task_id` alıyor, task'a ulaşamadığı sonucuna varıp yenisini açıyordu.

### "Başladım" ≠ "bitirdim" (subtask completion)

Subtask durumu, agent loop hatasız döndüğü için `completed` damgalanıyordu. Sonuç: kendi çıktısı "Görevi talep ettim ve in_progress'e taşıdım, şimdi proje yapısını inceleyeceğim" olan bir subtask arayüzde yeşil `completed` görünüyordu.

`Executor.runTask` artık subtask'ın **kendi** tool defterine bakıyor:

- Chat orchestration'da `ToolUsage` tracker'ı yoktu (yalnız `board/runner.go` kuruyordu) — subtask ölçülemiyordu. Tracker yoksa executor kuruyor; varsa board run'ın paylaşımlı tracker'ına dokunmuyor, `registry.UsageDelta` ile loop öncesi/sonrası fark alıyor (analiz grounding kapısının semantiği değişmiyor).
- `startedNotFinishedReason`: delta boş değil **ve** tamamı `domain.BoardProgressTools` (`claim_board_task`, `move_board_task`) **ve** subtask'ın elinde bunların dışında iş yapacak araç varsa → bitmemiş sayılıyor.
- İlk tespitte hata `lastErr` olarak geri besleniyor (mevcut "Previous attempt failed: …" mekanizması), agent bir deneme daha yapıyor. Denemeler biterse subtask `failed` damgalanıyor **ama sonuç döndürülüyor** — run patlamıyor, kullanıcı çıktıyı görüyor, etiket dürüst oluyor.
- Bilerek geçen iki durum: hiç tool çağırmayan subtask (bağlamdan cevaplayan konuşma adımları) ve elinde yalnız claim/move olan subtask ("DE-1'i board'a taşı" — orada taşıma zaten teslimatın kendisi).

Planner prompt'una eşlik eden kural: subtask'a açıklamasının gerektirdiği araçlar verilir. Yalnız `claim_board_task` / `move_board_task` / `add_task_comment` tutan bir subtask kodu değiştiremez, sadece board'u günceller.

### Bir task üzerinde aynı anda tek run

Board'da bir task'ın kaydı ile üzerinde çalışan ajanlar üç ayrı yerden çoğalıyordu; üçü de kapatıldı:

| Yol | Kapı |
|---|---|
| `add_task_comment` ile ajanın kendi task'ına bıraktığı not `task.commented` üretiyor, bu da assignee'ye — yani ajanın kendisine — çözülüyordu. `actor_agent_id` yalnız move payload'ında vardı, self-dispatch kapısı yorumu hiç görmüyordu | `actorAgentIDFromPayload` artık yorumun `author_type=agent` + `author_id` alanlarını da aktör olarak okuyor |
| Ajan çalışırken bırakılan bir yorum (PM'in kickoff notu) ikinci bir run kuyruğa alıyordu: `HasPendingForTask` sadece `pending` satırlara bakıyor, `running` olana bakmıyordu. O ikinci dev run'ı sonradan başlayıp task'ı claim ediyor ve **code_review'dan in_progress'e geri çekiyordu** | `HasLiveForTask` (`pending`+`running`) eklendi; `Dispatcher.alreadyWorkingOn` yorum event'leri için onu kullanıyor. Move'lar eski davranışta kaldı — kolon değişimi gerçek bir haber, verify gate'in task'ı in_progress'e geri itmesi fix turunu planlayan tek şey |
| Tek bir create/update iki event yayınlıyor (`task.created`/`task.moved` + `task.assigned`). İlki ajanı dispatch ediyor, ikincisi yalnız `HasPendingForTask` ile korunuyordu — worker ilk run'ı iki emit arasında `running`'e çevirdiğinde ikinci event kuyrukta run göremeyip **duplicate** açıyordu. Runner'ın task başına tek run kuralı bunu park edip ilki biter bitmez çalıştırıyordu: "todo'ya alınan task in_progress'e taşındıktan hemen sonra ajan 2. kez tetikleniyor" şikâyeti | `alreadyWorkingOn` artık `task.assigned` için de `HasLiveForTask` (`pending`+`running`) kullanıyor. Ayrıca `task.assigned` payload'ına `actor_agent_id` eklendi — move payload'ında zaten vardı, assign'da yoktu, yani ajanın kendi atamasıyla başkasınınki ayırt edilemiyordu |
| Runner aynı task için iki run'ı paralel çalıştırabiliyordu: code_review'a dispatch edilen system-architect, developer run'ının **kuyruğunu** (verification, fix turları, commit, push) beklemeden başlıyor ve hâlâ yazılmakta olan ağacı inceliyordu | `Runner.beginTask`/`endTask`: task başına tek run. Meşgul task'a düşen job park edilir, task boşalınca en eski park edilmiş job tekrar kuyruğa girer. Worker bloklanmaz, başka task'lara devam eder |

### Kolon sahipliği: hand-off kapıları ve terminal kolonlar

`ready_for_qa` **kuyruk** kolonu (girişte pipeline + stage deploy tetiklenir), `in_qa` ise **testin yapıldığı** kolon. Akış hiçbir yerde `in_qa`'ya taşımadığı için QA `ready_for_qa`'da test edip doğrudan `pm_uat`/`need_revision`'a atıyordu — `in_qa` ölü kolondu, board hiçbir zaman "şu an test ediliyor" bilgisini göstermiyordu. Düzeltme dört katmanda:

| Katman | Değişiklik |
|---|---|
| `catalog/seeddata/prompts/qa-agent.md` + `role_seed.go` | QA akışına 0. adım: test etmeden önce `ready_for_qa` → `in_qa`. Yeni kural `qa-enter-in-qa-before-testing` (öncelik 100); `qa-pass-to-pm-uat` / `qa-fail-to-need-revision` artık çıkışı `in_qa`'dan yapıyor |
| `board/runner.go` `columnInstruction` | `ready_for_qa` ve `in_qa` için ayrı case'ler. Genel preamble "kolona taşıma iş değildir, planın ilk maddesi olmasın" diyor; `ready_for_qa` talimatı bunu bozmadan taşımayı **ilk test adımının açılış hareketi** olarak konumluyor |
| `catalog/seed.go` + migration 070 / 104 | `qa-agent` artık `ready_for_qa`, `in_qa` **ve** `done`'a abone (`done` = PR merge, test değil). Seed yalnız aboneliği hiç olmayan ajana yazdığı için mevcut kurulumlar migration ile backfill ediliyor |
| `board/dispatcher.go` `isHandoffGateColumn` | `in_qa` + `human_uat` eklendi |

Hand-off kapısı olmayan kolonda `task.moved` **assignee'ye** çözülür. Bu üç yerde yanlış ajanı uyandırıyordu:

| Kolon | Eski davranış | Yeni |
|---|---|---|
| `in_qa` | QA test etmeye başladığı anda **developer** dispatch ediliyordu — ikinci run test edilen branch'e yazmaya başlıyor | Kolon aboneliğine (QA) çözülüyor |
| `human_uat` | İnsan onayı bekleyen task'ta developer'a run açılıyordu | Hand-off kapısı, abonesi yok → hiç run açılmıyor (`analiz_review` ile aynı desen) |
| `done` / `released` | Biten task'ın assignee'sine run açılıyor, `columnInstruction` default'u ona "sonraki kolona taşı" diyordu — `done` task'ları deploy olmadan `released`'a kayıyordu | `isDispatchSuspendedTask`: `released` her zaman, `done` ise **analiz dışı** task tiplerinde suspend. Analiz'de `done` insanın onayıdır; mimar oradan implementation task'larını açar. Tek istisna `doneMergeWake` — aşağıya bakın |

Aynı mantık yorumlara da uygulandı: hand-off kapısı kolonundaki bir task'a düşen `task.commented` artık task'ı **tutan** ajana (QA, mimar, PM) gidiyor. Önceden assignee'ye gidiyordu; `in_qa`/`code_review`'daki bir task'a yazılan yorum developer'ı uyandırıyor, işi fiilen yürüten ajan yorumu hiç görmüyordu.

### `done` = PR'ın merge edildiği kolon (`doneMergeWake`)

`done` hiç kimseyi dispatch etmiyordu ve bunun bedeli görünmezdi: board'un onayladığı task'ın kodu branch'te bekliyor, merge'ü bir insanın elle yapması gerekiyordu. Üstelik task PR'ları **draft** açılıyor ve hiçbir yerde draft'tan çıkarılmıyordu — GitHub draft PR'ı merge etmeyi reddettiği için o PR'lar tanım gereği merge edilemezdi.

İki değişiklik:

1. **Task PR'ları artık ready-for-review açılıyor** (`github.CreatePullRequest`, `git.EnsurePullRequest` — eski adları `CreateDraftPR` / `EnsureDraftPR`). Draft'ın hiçbir karşılığı yoktu: PR, iş review'a devredildiği anda açılıyor. Kodda draft durumuna bakan tek yer kalmadı; `get_task_pull_request` hâlâ `draft` alanını raporluyor ve merge aracı **eski** draft PR'ları onarım yolu olarak draft'tan çıkarıyor (`MarkPullRequestReady`).
2. **`done`'a düşen task QA'yı uyandırıyor** — merge etmesi için, `merge_task_pull_request` ile (squash + branch silme). Eski hatayı geri getirmemek için dört şart birden aranıyor (`board/dispatcher.go` `doneMergeWake`): kolon `done`, task tipi kod gönderiyor (analiz hariç), olay `task.moved`/`task.created` (yorum/atama/güncelleme değil — `UpdateTask` zaten yalnız gerçek kolon değişiminde `task.moved` yayıyor) ve task'ta kayıtlı, **henüz merge edilmemiş** bir PR var (`pr_url` dolu, `merge_commit_sha` boş).

Uyanan ajan **kolon aboneliğinden** çözülüyor (yani QA), asla assignee'den: `resolveAgents` `done`'da implementer'a döner, ki tam olarak `released`'a kaymayı üreten davranış odur. Tek seferliğini `merge_commit_sha` garanti ediyor (migration 104) — merge onu yazar, sonraki her olay "yapacak bir şey yok" bulur. Ek olarak: ajan kendi olayına dispatch edilmez, `alreadyWorkingOn` bu yolda **çalışan** run'a da bakar (iki eşzamanlı merge olmasın diye), reconciler sweep'leri `done`'u zaten atlar.

Run tarafında üç uyarlama: `producesADiff` (`board/review.go`) `done`'u da dışarıda bırakıyor — merge sonrası post-run commit, silinen branch'i saniyeler sonra origin'e geri açardı; `domain.verdictColumns`'a `done` eklendi, yani merge run'ı dosya yazıcılarını ve `commit_task_changes`'i kaybediyor (`merge_task_pull_request` bu kolonda muaf, başka hiçbir kolonda değil); ve `columnInstruction`'ın `done` case'i genel default'un "sonraki kolona taşı" cümlesini bu kolondan siliyor (sonraki kolon `released`).

Merge'ün kendi reddetme matrisi ve neyi neden kontrol ettiği: `.ai/tool-reference.md` → `merge_task_pull_request`.

### Board yorumu = harekete geçirilecek bir şey (2026-08-28)

Kart yorumu artık yalnız **birinin bir şey yapması gerektiğinde** yazılıyor: red + gerekçesi, hata, blocker, cevaplanamayan soru, yapılmayan iş. Geçen her şey sessiz — kolon, kriterler, PR alanı, pipeline paneli ve task history zaten söylüyor.

| Kaldırılan yorum | Yerine bakılacak yer |
|---|---|
| developer'ın yeşil run kapanış özeti (`runner.go`, build gate sonrası) | run mesajı + diff/PR |
| `merge_task_pull_request` başarı yorumu (`merge_pr.go`) | `board_tasks.merge_commit_sha` (UI'da PR bloğunda) |
| "Prod deploy succeeded — task released" (`pipeline.go` `moveTask`) | `MoveReasonDeployReleased` ile board history |
| QA senaryo planı, QA/PM "passed" yorumu, architect approve yorumu | `review_criterion` notları + kolon |

Kural üç yerde birden yazılı: `add_task_comment` tool açıklaması, `skills/shared/board-comment-style`, ve `columnInstruction`'ın kolon cümleleri. PR linki/numarası hiçbir yorumda tekrarlanmıyor — task'ın alanı ve UI sağ kolonunda duruyor.

### `done` / `released` = deploy'un izlendiği kolonlar (`deployWatchWake`, migration 105)

Merge kodu default branch'e koydu; production'ın onunla ne yaptığını kimse sormuyordu. "Merge edildi" ile "canlıda ve çalışıyor" bir karta bakınca aynı görünen iki ayrı olgu.

QA aynı sırayı `done`'da tamamlıyor — **merge → izle → geri al** — ve `released` aboneliğini de alıyor (migration 105 backfill'i), çünkü prod workflow'u olan bir repoda kart yeşil deploy'da `released`'a taşınırken push-to-deploy repoda hâlâ `done`'da duruyor.

**İkinci carve-out, ilkinden daha dar.** `deployWatchWake` task'ın *durumuna* değil, yalnızca sweeper'ın (ve rollback dispatcher'ının) yazdığı bir payload anahtarına bakıyor: `domain.EventPayloadResumedResource == "deploy_watch"`. Durum tabanlı bir koşul — "done'da, merge edilmiş, deploy doğrulanmamış" — karta atılan her yorumla, her güncellemeyle yeniden oluşur ve kolon QA'yı sonsuza kadar uyandırırdı; payload anahtarını ise yalnızca onu yazan tek çağıran üretebilir. Kolon kontrolü yine de yapılıyor (`done`/`released`), tip kontrolü de (analiz kod göndermez).

**Bekleyen deploy park ediyor, bloklamıyor.** `get_task_deploy_status` sonuç yerine `domain.ResourceBlock{Resource: "deploy_watch"}` döndürüyor; loop turu bitiriyor, runner kartı park ediyor, `board.DeploySweeper` (2 dk) GitHub deploy'un bittiğini söylediğinde geri alıyor. Cihaz park'ının mekanizması, tek yapısal farkla: cihaz bir KUYRUK (tek telefon, boşalan herkesi serbest bırakır, en eskisini al), iki park edilmiş deploy ise iki ayrı şey ve herhangi biri önce bitebilir — bu yüzden sweeper park edilenleri **claim etmeden** listeliyor, her birini soruyor, yalnız hazır olanı claim ediyor. Beklemenin bedeli park başına pass başına bir API çağrısı ve sıfır token; ajan tarafında bir retry döngüsü ise poll başına bir model turu harcar ve deploy boyunca eşzamanlılık slotu tutar.

**Run tarafı:** `columnInstruction`'ın `done` case'i artık merge + izleme sırasını anlatıyor, ve `released` kendi case'ini kazandı (genel default'un "sonraki kolona taşı" cümlesi oraya asla ulaşmamalı — `released`'dan sonra kolon yok). `domain.RestrictToolsForVerdictColumn` `rollback_task_release`'i `done` dışındaki her verdict kolonundan çıkarıyor; `released` verdict kolonu olmadığı için orada kalıyor.

Araçların kendisi, reddetme matrisleri ve iki rollback mekanizması: `.ai/tool-reference.md` → `get_task_deploy_status` / `get_deploy_logs` / `rollback_task_release`, ve `.ai/architecture.md` → "Watching the deploy, and rolling it back".

### Deploy edilemiyorsa need_revision değil (2026-08-28)

Actions dispatch'i **hesap yüzünden** reddedildiğinde (402, spending limit, Actions disabled) kod hatalı değildir; `reportPipelineFailure` artık o durumda kartı taşımıyor, yalnız nedenini karta yazıyor (`githubapi.IsCIUnavailableText`, deploy trigger'ları için). Karar `done`'daki QA run'ında:

| Durum | Hareket |
|---|---|
| repo'nun kendi deploy adımı var (script / `make` target / README·`.ai` adımları) | `run_terminal` ile o adımları uygula, ortamın cevap verdiğini doğrula |
| deploy yolu yok (veya adımlar da çalışmıyor) | task `blocked`'a çekilir, tek yorumla: merge edildi, deploy EDİLMEDİ, sebep |
| merge conflict (`dirty`/`behind`) | task `need_revision` — rebase developer'ın işi |

Merge edilmiş ama deploy edilmemiş task asla `done`'da bırakılmıyor.

### `blocks` artık gerçekten bloklar (work_order park, migration 106)

`task_relations`'ın `blocks` tipi migration 022'den beri duruyordu ve onu okuyan tek şey `repository.Service.validateMoveAllowed`'dı: `todo`/`in_progress`'e yapılan bir **taşımayı** reddediyordu. Bu yalnız kartı sürükleyen insanı yakalıyor. Doğrudan `todo` kolonunda **yaratılan** bir task, reconciler'ın hiç başlamamış task'ı süpürmesi, bir sweeper'ın kartı geri vermesi, bir atama eventi — hepsi taşımadan geçmeden dispatcher'a ulaşıp, açıkça beklemesi söylenmiş işi başlatıyordu.

Kapı bu yüzden **dispatcher'a** taşındı (`board.WorkOrder`, `Dispatcher.Dispatch` içinde board event yazıldıktan hemen sonra): run başlatan tek yer orası. Sadece işin BAŞLADIĞI iki kolonda çalışıyor — `todo` ve `in_progress`, `validateMoveAllowed`'ın koruduğu çiftin aynısı, ki taşıma reddi ile dispatch park'ı "bu başlayabilir mi" sorusunda ayrı düşemesin. `code_review`'a ulaşmış bir task'ın kodu zaten yazılmıştır; orada park etmek biten bir değişikliği artık var olmayan bir bağımlılığın arkasında bırakırdı.

**Reddetmek değil park etmek**, çünkü reddetmek görünmez: kart `todo`'da hiç başlamamış bir kartla birebir aynı görünür ve kimsenin neden almadığını söyleyecek hiçbir şey olmaz. Park bunu söylüyor — `blocked` kolonu, kartın üstünde "waiting for T-1 (API migration) [in_progress] to finish", ve her blocker'ı adıyla sayan bir sistem yorumu.

Serbest bırakan `board.WorkOrderSweeper` (1 dk — dördün en kısası, çünkü sorduğu şey aynı veritabanındaki indeksli tek sorgu; cihaz sweeper'ı ev tünelinin ucuna, deploy sweeper'ı GitHub'a gidiyor). Şekli `DeploySweeper`'ın şekli, `DeviceSweeper`'ınki değil: work-order park'ı task BAŞINA — iki park edilmiş task iki ayrı blocker bekliyor ve en son park eden ilk çözülen olabilir. Sorduğu şey ilişki grafiğinin kendisi: `ListBlockingSources` bir task'ın `blocks` satırlarının **bitmemiş** kaynaklarını döndürüyor, yani boş cevap "beklediği her şey done/released" demek. Aynı sorgu blocker'ın bitmek yerine yok olmasının iki yolunu da kapsıyor — silinen task `task_relations`'ı cascade ile götürüyor (iptal edilen blocker bağımlılarını kimse hatırlamadan serbest bırakıyor), `in_progress`'e geri dönen blocker ise sorguda yine görünüyor ve bağımlı park'ta kalıyor.

Sweeper'ın kendi geri verişi `domain.EventPayloadResumedResource == "work_order"` payload'ıyla geliyor ve `workOrderGateApplies` onu görünce soruyu tekrar sormuyor — sweep'in az önce cevapladığı şeyi en iyi ihtimalle doğrular, en kötüsünde blocker'ın güncellemesiyle yarışan bir okumayla kartı yeniden park ederdi.

Okunamayan grafik dispatch'i **durduruyor** (fail closed): prerequisite'i var olmayabilecek bir kod tabanına yazılan değişiklik geri alınamaz, atlanan dispatch'i ise reconciler bir sonraki sweep'te alıyor.

Yön: `blocks` satırı BLOCKER'ı `source_task_id` olarak saklıyor — `deploy_depends_on`'un tersi ok, ve öyle kalıyor çünkü çevirmek saklanmış her satırı ters çevirirdi. Hiçbir araç ajandan bu yönde düşünmesini istemiyor: `create_board_task` / `update_board_task` `blocked_by` alıyor ("bu task onları bekler"), tıpkı `deploy_depends_on` gibi. Döngü yazıldığı yerde, döngüyü kapatan zincirle birlikte reddediliyor.

### Analiz referansı run context'e giriyor (`derived_from`, migration 106)

Analiz artık repoya hiçbir şey commit etmiyor: mimarın spec'i ve planı yalnızca analiz task'ına iliştirilmiş `task_documents` olarak var. Bu, onaydan sonra açılan implementation task'larını spesifikasyonuna ulaşamayan işler haline getirmişti — başlık, açıklama, ve kendisi için yazılmış plana giden hiçbir yol yok.

`derived_from` o yol: kaynak implementation task, hedef analiz task'ı (yön `deploy_depends_on` ile aynı, ikisi de `task.Relations`'tan aynı şekilde okunuyor). Sıralama ifadesi değil ve hiçbir şeyi geçitlemiyor — `blocks` analizi "implementation ne zaman başlayabilir"in kapısı yapardı (oysa o kapı zaten geçildi: insan `analiz_review`'da onayladı), `deploy_depends_on` ise kod göndermeyen bir analiz task'ı production'a ulaşana kadar implementation'ı yayınlamayı reddederdi.

Run tarafı: `Runner.analysisContext` (`runner.go`) `repository.Service.AnalysisReferences` üzerinden ilişkiyi çözüp analiz task'ının dokümanlarını bir system mesajına yazıyor ve bu bloğu **trigger mesajının hemen ardına, diff ve PR bloklarından önce** koyuyor — o ikisi task'a ne yapıldığını söylüyor, bu ise task'ın ne olması gerektiğini; ters sırada okuyan bir run, tatmin etmesi gereken spesifikasyonu bilmeden değişikliği inceler. Her run'da veriliyor, sadece ilkinde değil: revision run'ı aynı spec'e karşı düzeltiyor, diff'i yargılayan reviewer da aynı spec'e karşı yargılıyor.

Blok, dokümanları okuyan aracı da adıyla söylüyor — `list_task_documents` (yeni; `add_task_document`'ın okuma yarısı, tıpkı `list_task_comments`'ın `add_task_comment`'a olduğu gibi). O araç olmadan "yorumları okuyayım" deyip yorum yazan modelin aynısı, doküman okumak isteyip doküman yazardı. Migration 106 onu `list_task_comments`'ı olan her ajana backfill ediyor.

### Rol ajanının kod okuma araçları policy'den bağımsız gelir

`UpliftWorkspaceTools`, allowlist'inde `_board_` geçen bir ajanı "role-scoped" sayıp kod araçlarının tamamını atlıyordu. Rol ajanlarının `ToolPolicy`'si ise **yalnız agent CREATE anında** yazılıyor (admin özelleştirmesi restart'ta korunsun diye), yani rol kod araçlarını sonradan kazandıysa mevcut kurulumdaki satır onlarsız kalıyor. Sonuç: code_review'a dispatch edilen `system-architect` inceleyeceği diff'i okuyacak hiçbir araca sahip olmadan çalıştı ve run'ını task'a "gerekli araçlara sahip değilim" yorumu yazarak harcadı.

Artık `domain.CodeExplorationTools` (`codebase_search`, `grep_code`, `get_repo_tree`, `get_symbol_skeleton`, `expand_symbol_context`) `workspaceReadAlwaysTools` gibi her tool-scoped ajana veriliyor — hepsi salt okuma, ajanın **değiştirebileceğini** genişletmiyor. `run_terminal` ve `mcp_filesystem_*` role-scoped ajanlar için hâlâ operatörün policy'sine bağlı.

### `code_review` = PR'ı okumak, çalıştırmak değil

Review kolonundaki run başka birinin diff'ine hüküm veriyor; onu build edip düzeltmesi gereken run değil. Eskiden `columnInstruction`'ın genel default'u mimara "rolünün bu kolonda istediği işi yap" diyordu, mimar da bunu "klonla, derle, testleri koştur" diye okuyup girişte zaten koşmuş pipeline'ı tekrar üretiyordu.

| Katman | Değişiklik |
|---|---|
| `board/runner.go` `columnInstruction` | `code_review` case'i: PR diff'ini **oku**, uygulamayı/build'i/testi çalıştırma, kendin düzeltme. Üç eksen: (1) istenen iş yapılmış mı (AC), (2) kodun kendisi sağlam mı, (3) değişiklik domainde başka neyi kırıyor — üçüncüsü için diff'in dokunduğu çevre kodu serbestçe okunur |
| `board/review.go` `isReviewColumn` | `code_review` / `analiz_review` / `pm_uat` run'larında build gate (`verifyAndFix`) ve commit/push atlanıyor. Review run'ı fix turuna girince mimar hem implementer oluyor hem de kendi yamasını aynı kapıda onaylıyordu; verify başarısızlığı task'ı review edenin adına `in_progress`'e geri itiyordu |
| `board/review.go` `reviewDiffMessage` | Review run'ının diff bütçesi 8 KB yerine 24 KB. "Diff'in tamamını oku" talimatı 8 KB'lık kesikle zaten yerine getirilemiyordu |
| `catalog/seeddata/prompts/system-architect.md` + `code-review-rubric.md` + `role_seed.go` | Prompt/skill/rule aynı şeyi söylüyor: review okumaktır, "Domain impact" başlığı diff dışındaki çağıranları okumayı ve etkilenen `file:line`'ı isimlendirmeyi zorunlu kılıyor. Yeni kural: `code-review-reads-never-runs` |

### PR'sız code review yok

Review PR üzerinden yapılıyor, ama PR'ı açan yol asenkron ve best-effort'tu: developer'ın branch'i run'ın **sonunda** push ediliyor, kolon geçişinde tetiklenen PR denemesi bu push'a yarışı kaybedince ortada hiç PR kalmıyordu.

- `git.PushBranch` (yeni port metodu): commit etmeden yalnız branch'i yayınlar. `CommitAndPush`'tan farkı bilerek: review workspace'indeki ağaç incelenen developer'a ait, `git add -A` onun artıklarını branch'e katardı.
- `board/review.go` `ensureReviewPR`: `code_review` run'ı başlamadan PR'ı garanti eder — `EnsurePullRequest` başarısızsa branch'i push edip bir kez daha dener. PR yoksa run `ErrReviewPRMissing` ile fail eder ve task'a nedenini yazar (task `code_review`'da kalır, reconciler tekrar dener). Origin'i olmayan repo'da PR mümkün değildir, orada diff ile review edilir.
- `board/pipeline.go` `resolveGitInfo` aynı push-then-retry'ı yapıyor: pipeline kolon girişinde koştuğu için PR en erken orada açılıyor.
- `adapter/git` `EnsureTaskWorkspace`: fetch sonrası `origin/<branch>` varsa `checkout -B branch origin/<branch>`. Öncesinde taze bir workspace (yeni pod, silinmiş disk, başka host'taki reviewer) branch'i default'tan kesiyor ve **boş diff** görüyordu — origin'de duran işi review edecek run'ın eline hiçbir şey geçmiyordu.

### Ajan sustuktan sonraki faz artık görünür

Board run'ı ajanın son mesajıyla bitmiyor: build/vet doğrulaması, N fix turu, commit ve push arkadan geliyor. Bu faz hiç step yazmadığı için aktivite panelinde en yeni kayıt ajanın son tool çağrısı kalıyor, tamamlanmış bir plan donmuş `run_terminal running…` etiketinin altında takılmış görünüyordu. Runner artık `build_verification_start` / `build_verification_passed` / `build_verification_failed` step'lerini yazıyor.

## UI routes

```
/orchestration/agents
/orchestration/agents/:agentId/settings
/orchestration/agents/:agentId/skills
/orchestration/agents/:agentId/rules
```

Legacy `/orchestration/skills` and `/orchestration/rules` redirect to the agents list.

## Self-evolution, memory, KPIs (migrations 031–035)

- `agents.self_evolution_enabled` (031): gates whether the reflection engine may apply skill/rule changes for the agent. Toggle in AgentSettingsPage. Templates carry the flag + `kpis` JSONB (035).
- **Memory** (`agent_memories`, 032 + 065): memories with JSONB embeddings, in four buckets from two nullable columns — `agent_id NULL` = team memory, `repository_id NULL` = global (see [Memory scopes](#memory-scopes-migration-065)). Inline tools for all role agents: `save_memory`, `search_memory`, `delete_memory` (`internal/adapter/tools/memory`). Injected into board runs and agent chats via `memory.Recall` + `prompt.MemoryContextMessage` (8 project + 8 global, rendered as separate sections). Service: `internal/application/memory`.
- **Lazy skills**: `BuildSystemPrompt` now injects a skill **index** (name + description) instead of full content; agents fetch full instructions at use time via the `load_skill` tool (`internal/adapter/tools/skill`).
- **KPIs** (`agent_kpis` + `agent_kpi_results`, 035): definitions live on the agent, measured per agent per period (daily/weekly ISO/monthly). Only metrics in the Go registry (`internal/application/kpi/registry.go`) are accepted: `tasks_completed`, `revisions_received`, `uat_failures`, `failed_runs`, `bugs_assigned`, `first_pass_rate`, plus the column-time metrics below. Attainment: full target → 1.0, half target → 0.5, else 0. Composite = weighted mean × 100. KPI context is injected into agent prompts ("your objective: meet these KPIs"). Admin CRUD: `/admin/agents/:id/kpis`; registry list: `GET /v1/kpi-metrics`. Role agents get default KPIs on seed (`defaultRoleKPIs`).

### Memory scopes (migration 065)

Two nullable columns on `agent_memories` produce four buckets:

| | `repository_id NULL` (global) | `repository_id` set (project) |
|---|---|---|
| `agent_id` set | `agent_global` — the agent's habits and preferences, valid anywhere | `agent_project` — what the agent learned inside that repository |
| `agent_id NULL` | `team_global` — workspace conventions every agent reads | `team_project` — that repository's shared facts (build command, deploy flow) |

- **Writing**: `save_memory` takes `scope` (`project` \| `global`) and `shared` (team vs personal). Unset `scope` means "wherever I am" — project-scoped when a repository is in context, global otherwise. `scope=project` with no repository in context is an error, not a silent global write.
- **What is refused** (`domain.MemoryRunLogReason`, applied by `save_memory` and by the reflection job): content anchored to one card — a task key, `PR #n`, a commit SHA, a column move (`code_review→ready_for_qa`), "this task/run". Run narration belongs in the task's comments; the tool answers with the reason and the durable rewrite. A save whose wording already matches a memory in the same bucket (`domain.MemoryDuplicateOf`, Jaccard ≥ 0.6) returns `saved:false` with `duplicate_of` instead of adding a second copy.
- **Reading**: `search_memory` defaults to the visible set (this repository + global, own + team) and accepts `scope` (`all`\|`project`\|`global`) and `owner` (`all`\|`self`\|`team`). Query building lives in `buildMemoryListQuery` (`adapter/store/postgres/memory.go`) and is unit-tested there.
- **Repository in context**: board runs get it from `job.RepositoryID`, chats from `session.ProjectID` (the column is `sessions.repository_id`; the Go field kept its old name). With no repository, project memories are unreachable — another repo's lessons never leak into a run.
- **Eviction**: `memory.max_count` applies per (agent, repository) bucket, so a busy repository cannot evict what the agent learned elsewhere. Team memories are exempt.
- **Reflection** writes global memories only: it reasons over runs from every repository at once and has no single project to bind a lesson to.
- **API**: agent and shared memory listings accept `repository_id` + `repo_scope` (`any` default \| `project` \| `global` \| `visible`); create bodies accept `repository_id`. Scope is fixed at creation — updates never move a memory between buckets.
- **UI**: `MemoryManager` has a scope filter (all / global / per repository), a scope select in the create form (locked while editing), and a scope badge per row.

### Column-time KPIs and defect attribution (migration 057)

`task_column_spans` records one row per uninterrupted stay of a task in a column, with the agent that worked it. Spans are written from `Dispatcher.Dispatch` — the only place a board event is created — and the agent is claimed by the first run created in that span. A rework loop produces a second span for the same column with a higher `visit_no`.

Time metrics (all lower-better, median hours, weekly by default):

| Key | Columns | Seeded target (full / half) |
|---|---|---|
| `clean_time_in_progress` | `in_progress` | dev 6h / 16h · architect analiz 4h / 12h |
| `clean_time_code_review` | `code_review` | 1h / 4h |
| `clean_time_in_qa` | `ready_for_qa` + `in_qa` | 3h / 8h |
| `clean_time_pm_uat` | `pm_uat` | 2h / 6h |
| `review_escapes` | — (score events) | 0 / 1, weight 2 |
| `tool_error_rate` | — (`task_agent_runs.tool_calls/tool_errors`, migration 071) | lower-better, %, min sample 20 calls |

Two rules keep speed from being bought with quality:

- **Clean-only sample.** Only tasks that reached `done`/`released` without ever entering `need_revision` are measured (`board_tasks.clean_completion`, stamped by `CompletionStamper`). A rushed task that bounced leaves the sample entirely — hurrying removes the reward rather than increasing it.
- **Minimum sample of 3.** Below that the resolver returns `ErrInsufficientData` and no result row is written, so `CompositeScore` drops the KPI from the weight sum. Writing a zero would score 1.0 on a lower-better metric and make idleness look like maximum speed.

Time in `blocked`, `human_uat`, `analiz_review`, `backlog` and `todo` is never charged to an agent: those are human latency or nobody's work.

Penalties are charged to **span owners**, not to `task.assignee_agent_id`:

| Rejection | Charged | Event (delta) |
|---|---|---|
| `code_review` / `ready_for_qa` / `in_qa` → `need_revision` | dev | `revision_requested` (−10) |
| `pm_uat` → `need_revision` | dev + QA | `pm_uat_failed` (−5 each) |
| `human_uat` → `need_revision` | dev + QA + PM | `human_uat_failed` (−8 each) |
| Human rejects in `code_review` where the reviewer's verdict was `approve` | architect | `review_escape` (−10) |

The later a defect is caught, the more it costs. An agent that held two of the charged stages is charged once.

### Review mode: `repositories.require_human_review` (migration 047, semantics changed in 057)

The flag used to mean **human instead of agent**: with it on, the architect and PM were never dispatched into `code_review`/`pm_uat`. It now means **human after agent**:

- The reviewing agent always runs. When it tries to advance the task, `board.ReviewGate` converts the move into a recorded verdict (`review_verdict = approve` on the open span), leaves the task in place, and waits for the human.
- When the reviewing agent moves the task to `need_revision`, the verdict is `reject` and the move **proceeds** — human approval gates letting work through, not sending it back.
- When the human then rejects a span whose verdict is `approve`, the reviewer is charged `review_escape`.

Moves carry an explicit actor (`UpdateBoardTaskRequest.Actor`, never parsed from the request body): `agent` from the board tools, `human` from the HTTP API, and the zero value `system` from the pipeline and verification steps — so an automated move to `need_revision` after a red build is not mistaken for a human rejection.

**Cost note:** repositories with the flag on now spend tokens on the architect and PM, which they did not before.
- **Evolution engine** (`agent_reflections` + `agent_evolution_events`, 033; `internal/application/evolution`): triggers = periodic ticker (reflect_interval), task moved to `need_revision` (debounced), manual `POST /v1/agents/:agentId/reflect`. Incremental: each reflection covers only the window since the previous one and compares against the prior reflection's `performance_snapshot` baseline. Evidence = window chat messages, task runs, revision comments, score events, KPI attainment, current skills/rules/memories, and a regression report. Output = strict JSON (skills/rules/memories/reverts + self-assessment); skills/rules applied only when `self_evolution_enabled`, with before/after snapshots recorded per change. With `evolution.allow_web_research` the reflection runs through agent.Loop with `{web_search, fetch_url}`.
- **Golden gate (auto-revert)**: `evolution.golden_gate` açıkken, reflection skill/rule değişikliği önerdiyse golden suite değişiklikten **önce** ve **sonra** koşar. Kararı reflection'ı yazan model değil, bağımsız bir hakem LLM verir (`evolution.judge_model` / `judge_provider_type`; boşsa reflection modeli): before/after geçme oranı + hangi golden görevin neyi kaçırdığı + uygulanan değişiklik listesi verilir, `{"keep":bool,"reason":string}` döner. Oran düştüyse hakeme bakılmadan revert; hakem erişilemezse "gerilemediyse tut" kuralına düşülür. Revert **hep-ya-da-hiç**: setteki her skill/rule değişikliği ters sırada geri alınır, her biri için `change_type=revert` event'i yazılır ve orijinal event `impact=regressed` işaretlenir (impact penceresini beklemez). Sonuç reflection summary'sine `Golden gate: %X → %Y | verdict ...` satırı olarak düşer; `performance_snapshot.golden_pass_rate_before` de saklanır.
- **Skill/rule bütçesi**: `evolution.max_skills_per_agent` (25) ve `max_rules_per_agent` (15) ajan başına toplam tavan. Reflection prompt'u "önce birleştir/güncelle" talimatı + kullanılan/toplam bütçeyi taşır; uygulama tarafında bütçe doluyken `create` reddedilir (applied log'a "rejected" satırı düşer), aynı isimli `create` otomatik olarak o skill'in `update`'ine çevrilir. Ajanın kendi `create_skill` tool'u da aynı tavanı uygular.
- **Versiyonlama / geri dönüş**: skill ve rule'a yapılan **her** yazım `catalog_versions`'a (095) append edilir — create/update/delete/restore, kaynağıyla birlikte (`user` | `evolution` | `seed`, evolution ise `reflection_id`). Silinen içerik de yazıldığı için geri getirilebilir. API: `GET /admin/agents/:id/skills/:skillId/versions`, `POST .../skills/:skillId/restore` `{"version":N}` (rule'lar için aynısı `rules/:ruleId`). Restore de yeni bir versiyon üretir; geçmiş asla yeniden yazılmaz.
- **Impact tracking**: pending events older than `impact_window` are classified `effective` / `regressed` / `neutral` / `insufficient_data` by comparing score events before vs after the change. Regressed unreverted changes are surfaced to the agent's next reflection; the agent decides to revert (before-snapshot restored, `change_type=revert`). No auto-revert.

### Lifecycle gates: `repositories.require_review_chain` / `require_release_deploy` (migration 080)

Two more per-repository flags, both `BOOLEAN NOT NULL DEFAULT false`, independent of `require_human_review` and of each other. `PUT /v1/repositories/:id/lifecycle-gates` arms/disarms either or both (an omitted field is left unchanged); see [api-spec.md](api-spec.md). Code: `internal/domain/lifecycle_gate.go`, `internal/application/repository/lifecyclegate.go`.

- **`require_review_chain`** blocks a move into `done` — and into `released` when that would skip `done`, but not the ordinary `done → released` promotion, since `done` already asserted the chain — unless the task has actually visited every stage `domain.ReviewChainForType` lists for its type: `code_review` → `in_qa` → `pm_uat` for `task`/`bug`, `analiz_review` alone for `analiz`. Evidence is whether the task's span history (`task_column_spans`, migration 057) ever contains that column, **not** the current column and **not** a verdict field — so rework through `need_revision` and back is never punished, it just re-earns the stage. Where a verdict *was* recorded (only while `require_human_review` is on), a stage whose most recent visit ended in a recorded `reject` blocks too, even if the column was visited. A stage whose column is missing from this board's `board_columns` is skipped rather than counted as failed — a customized board cannot route a task through a column it does not have.
- **`require_release_deploy`** blocks a move into `released` unless `task_pipelines` (migration 040) has a `prod_deploy` run with `status = success` for the task, or a `preprod_deploy` success on a repository with no prod workflow mapped (the same fallback `PipelineRunner` itself uses to call preprod the release). `status = skipped` (migration 067: no workflow mapped, nothing ran) is never accepted as evidence. `analiz` tasks are exempt (`domain.TaskTypeShipsCode`): they ship no code, and their own workflow drives `done → released` once the implementation tasks they produced exist.
- **Both are opt-in and default off** for the same reason `require_criteria_complete` and `require_human_review` are: each is only honest on a board actually wired for it. A repository with no QA agent subscribed to `in_qa`, or a customized board missing that column, could never satisfy `require_review_chain`. A repository with no prod deploy workflow mapped always records its prod deploy as `skipped`, which `require_release_deploy` never accepts — turning it on there parks every task in `done` forever, since nothing can ever earn `released`. Enabling either flag is the repository owner asserting their board can actually clear it.
- **Both fail closed** on evidence they cannot read (span store or pipeline store unavailable, or a lookup error) — the same direction every other board gate in this file fails, since a check that passes when its input is missing is not a check. The block error names the missing/rejected stage (or the missing deploy) and the remedy move, the same way `ErrMigrationNotStaged` does for the migration gate.

### Kuyruk kolonundan çalışma kolonuna geçiş sistem tarafından yapılır (`Runner.enterWorkingColumn`)

Kolona girmek tamamen ajanın kendi `move_board_task` çağrısına bırakılmıştı. Board, model o tool'a gelene kadar `todo` diyordu — hiç gelmediğinde de sonsuza kadar: DE-1, üzerinde çalışan bir `frontend-developer` run'ı varken `todo`'da durdu. Run başlarken, prompt kurulmadan önce artık sistem taşıyor.

- **Yalnız assignee'nin kendi run'ı.** `todo` bir task kolonun çözdüğü her ajana dağılır ve her biri "senin işin değilse dokunma" der; onlar adına claim etmek task'ı dispatcher'ın ilk ulaştığı ajana verirdi. **Atanmamış task hâlâ ajanın claim'ini bekler.**
- **Move ajana atfedilir** (`Actor=agent`, `ActorAgentID`), böylece dispatcher'ın `actorAgentIDFromPayload` guard'ı bu event için ikinci bir run açmaz — ajanın kendi yapacağı move'dan ayırt edilemez.
- Board yazımı patlarsa run devam eder; ajanın kendi move'u kolonu yine düzeltir.
- `job.Task` güncellenmiş hâliyle taşınır, dolayısıyla `columnInstruction` `in_progress` dalını render eder ve planner ilk adımını zaten olmuş bir move'a harcamaz.

`ready_for_qa → in_qa` de aynı fonksiyonda, aynı anda (2026-08-12). Ajana bırakıldığı sürece aynı hatayı üretiyordu: QA run'ı "önce `in_qa`'ya taşıyacağım" diye yazıp hiç test etmeden `completed` bitiyor, kart `ready_for_qa`'da kalıyordu — ve review chain kanıtı `in_qa` span'i olduğu için o task asla `done`'a çıkamıyordu.

- **Kim taşınır farkı kuyruğun dispatch şeklinden gelir.** `todo` assignee'ye fan-out olur (yukarıdaki kural), `ready_for_qa` ise bir hand-off kapısı kolonudur (`isHandoffGateColumn`): dispatcher onu **kolon aboneliğiyle** çözer, assignee hâlâ developer'dır. Dolayısıyla oraya düşen her run zaten o kolonun QA ajanıdır, assignee kontrolü aranmaz.
- `analiz` task'ı hariç — onun QA aşaması yok.
- Move yine ajana atfedilir, yani **ikinci bir QA run'ı açılmaz**: run başladığı yerden `in_qa` talimatıyla devam eder, test aynı run'da yapılır.
- Otomatik taşıma reddedilirse `columnInstruction`'ın `ready_for_qa` dalı kalır ve ajandan move'u kendisinin yapmasını ister; normal akışta ajan `in_qa` dalını okur.
- `qa-agent.md` 0. adımı buna göre yazıldı: "zaten `in_qa`'dasın, o move için adım planlama, ikinci run bekleme".

## Agent loop guards (`internal/application/agent`)

Bir run üç ayrı şekilde ilerlemeyi durdurur; her birinin ayrı guard'ı var. Hepsi `runToolCalls` içinde, `callTracker` üzerinden.

| Guard | Tetik | Etki |
|---|---|---|
| **Repeat** | Aynı tool + aynı args + aynı sonuç | 2. tekrarda sonuç yerine `repeatNudgeMessage`; 4'te run `stuck` |
| **Skip** | Yukarıdakinin **hemen ardından** gelen aynı çağrı | Tool **hiç çalıştırılmaz**, nudge döner (`RunStats.SkippedRepeats`) |
| **Error streak** | Üst üste 3 hatalı tool çağrısı (args farklı olsa da) | Sonucun önüne `errorStreakMessage`; 8'de run `deadEnd` |
| **Per-tool errors** | Tek tool run boyunca 5 kez hata verdi | Sonuca `toolErrorMessage` eklenir |

- Repeat guard yalnız çağrıyı **kendisiyle** eşleştirir; beş farklı dosyada aynı `sed` hatası ona görünmez — error streak onun için var.
- Skip kuralı dar tutuldu: çağrı bir öncekinin **birebir aynısı** olmalı ve sonucu zaten iki kez aynı çıkmış olmalı. Arada başka bir çağrı varsa yeniden çalıştırılır, çünkü o çağrı cevabı değiştirmiş olabilir.
- Başarılı her çağrı error streak'i sıfırlar; build'i düzeltirken defalarca hata almak normal, aralarda başarı vardır.

### Context bütçesi loop içinde de uygulanır

`Loop.SetHistoryBudget` ile verilen `context.Budget` **her iterasyonda** (ve wrap-up turunda) uygulanır. Önceden bütçe yalnız loop'a girmeden önce bir kez uygulanıyordu; loop'un kendi tool sonuçları isteği tekrar bütçenin kat kat üstüne çıkarıyordu (80 iterasyon × ≤`max_tool_output_chars`). Sonuç, sağlayıcının bağlantıyı düşürmesiydi — ağ hatası gibi görünen, aslında kimsenin karşılayamayacağı bir istek.

### Sağlayıcı hatası sınıflandırması

`domain.LLMHTTPError` üç sınıfı ayırır ve `chatWithRetry` her birine tek çalışan tedaviyi uygular:

| Sınıf | Örnek | Davranış |
|---|---|---|
| `retryStop` | 400, 401, 403, 404, 422 | Hiç tekrar denenmez |
| `retryBackoff` | 408, 409, 429, 5xx, transport hatası | 500ms → 1s → 2s (tavan 8s), context'e duyarlı |
| `retryShrink` | 413, ya da 400/422 + "context length" / "prompt is too long" | Konuşma **yarıya indirilip** yeniden gönderilir |

Küçültülmüş konuşma çağırana geri döner (`chatWithRetry` mesajları da döndürür), yoksa bir sonraki iterasyon reddedilen isteği yeniden kurardı.

### Hata bilgisi run'ları aşar

- **Attempt'ler arası** (`orchestrator.priorAttempt`): önceki denemenin tool sayıları, son çağrıları ve hangi tool'ların ısrarla patladığı bir sonraki denemenin prompt'una girer. Önceden yalnız hata metni taşınıyordu, mesajlar sıfırdan kurulduğu için her deneme aynı keşfi baştan yapıyordu.
- **Run'lar arası** (migration 071): `task_agent_runs.tool_calls / tool_errors / error_pattern`. Aynı task'ın bir sonraki run'ı, önceki başarısız run'ın `error_pattern`'ini system mesajı olarak okur. `tool_error_rate` KPI'sı da aynı sütunlardan hesaplanır.
- `registry.ToolUsage` artık hataları da sayar (`RecordError`/`Failures`/`Totals`), ama **ayrı bir map'te**: `Count`/`UsedAny` üzerine kurulu kapılar "ajan bunu gerçekten yaptı mı" diye sorar ve başarısız çağrı bunun kanıtı değildir.

## Kalite kapıları ve yeni araçlar (migrations 036–037)

- **Verification gate** (`board.verification_enabled`): board run bitince task workspace'inde `go build ./...` + `go vet` (+ package.json build script) koşar; hata çıktısı agent'a geri beslenir (`verify_max_fix_attempts` tur). Kalıcı hata → task `in_progress`'e döner + system comment (`internal/application/board/verify.go`).
- **AC gate** (`board.require_criteria_complete`): kriterli task, tüm kriterler tamamlanmadan ready_for_qa/done/released'e taşınamaz. Yeni board araçları: `list_acceptance_criteria`, `set_criterion_completed`.
- **Per-role AC verdicts** (`task_criterion_checks`, migration 073): implementer'ın `completed` işareti bir iddiadır; QA (`ready_for_qa`/`in_qa`) ve PM (`pm_uat`) her kriter için kendi approve/reject kararını `review_criterion` aracıyla ayrıca kaydeder — rol, ajanın beyanından değil task'ın o anki kolonundan türetilir, reject not zorunludur. `criteriaReviewGate` (aynı `require_criteria_complete` bayrağı): tüm kriterler QA onayı taşımadan in_qa→pm_uat, PM onayı taşımadan pm_uat→human_uat/done geçilemez; need_revision'a gidiş hiç kapılanmaz. Task `ready_for_qa`'ya yeniden girince verdicts sıfırlanır (in_qa→ready_for_qa hariç — aynı tur). Drawer her kriterde QA/PM rozetini ve reject notunu gösterir; mevcut kurulumlara araç policy'si migration ile backfill edilir.
- **Kriter iptali** (`task_acceptance_criteria.canceled/cancel_reason`, migration 131): kriterin üçüncü hali. `cancel_criterion` (gerekçe zorunlu) kriteri kapsam dışına alır, gerekçeyi task yorumu olarak da yazar; kapılar iptal edilmiş kriteri "settled" sayar (asla "karşılandı" değil), QA/PM verdict beklemez. Tool policy'si `set_criterion_completed` verili her role `catalog.grantMissingRoleTools` ile boot'ta eklenir — migration 114'ten sonra migration satır backfill'i yapamıyor (FORCE RLS, tenant yok).
- **Run bitiş döngüsü** (`board/criteria_sweep.go`): run kapanmadan önce ticklenmemiş kriterler aynı konuşmada agent'a geri verilir, en çok `criteriaSweepRounds` (3) tur. Cevap üç şıktan biri: tickle, gerekçeyle iptal et, ya da **işi şimdi yap** ve tickle. 1. turdan sonra prompt sertleşir ("özet yazma, değişikliği yap"). Tur bütçesi biterse kart açık kriterle park eder ve sebebini yazan bir system comment düşer.
- **Test senaryoları** (`task_test_cases`, migration 130): QA'nın turu kartta durur — kabul kriterlerinin yanında ayrı bir alan. Her case: `title`, `category`, `status` (planned/passed/failed/skipped/invalid), `expected`/`actual`, `evidence`, `notes`, opsiyonel `criterion_id` (boş = kriterin yazmadığı, istekten türetilmiş case). Araçlar: `record_test_cases`, `set_test_case_result`, `list_test_cases`. `testCaseGate`: `ready_for_qa`/`in_qa`'dan ileri çıkış, hiç case yoksa veya `planned` kalmış case varsa reddedilir; `failed` engellemez (çıkışı need_revision). Drawer kriterlerin altında gösterir.
- **Task branch diff**: workspace'li her board run'ın prompt'una branch diff'i (base'e karşı, 8K char) enjekte edilir — revizyon ve QA runları değişen kodu doğrudan görür (`git.TaskDiff`).
- **Takım-paylaşımlı memory**: `agent_memories.agent_id NULL` = takım hafızası; `save_memory` aracında `shared: true`. Prompt'ta `[team]` etiketiyle görünür; paylaşımlı kayıtlar eviction'dan muaf.
- **Golden eval** (`agent_golden_tasks` + `agent_golden_results`, 037): skill/rule değişikliği uygulayan her reflection sonrası agent'ın golden görevleri güncel skill/rule setiyle tool-suz replay edilir (beklenen alt dizgiler kontrolü). Geçme oranı reflection summary + `performance_snapshot.golden_pass_rate`'e yazılır. CRUD: `GET/POST /admin/agents/:id/golden-tasks`, `PUT/DELETE .../golden-tasks/:goldenId`, `GET .../golden-results`.
- **Retrieval**: chunk araması pgvector HNSW (varsa) + pg_trgm trigram RRF hibrit; injector `indexer.query_rewrite` ile multi-query; skeleton enjeksiyonu sembol grafi fan-in sırasıyla (en çok referans alan dosyalar önce).

## QA pipeline (migration 040)

- **task_pipelines + task_pipeline_jobs**: task `ready_for_qa`'ya taşınınca `board.PipelineRunner` arka planda (worker pool) build/test pipeline'ını async tetikler (`Trigger`, `PipelineTriggerReadyForQA`); aynı task için bekleyen bir pipeline varsa yenisi onu supersede eder. `POST .../pipelines` ile manuel yeniden tetikleme (`PipelineTriggerManual`).
- **Stage seçimi** (`commands.go`): repo'da tanımlı `verify_command`/`build_command`/`test_command` varsa onlar kullanılır; yoksa çok-dilli auto-detect marker dosyasına göre çalışır — go (`go.mod`), node (`package.json` npm script), rust (`Cargo.toml`), python (`pyproject.toml`/`requirements.txt`), maven (`pom.xml`), gradle (`build.gradle[.kts]`). Repo kökünde `Dockerfile`/`Containerfile` varsa ve bir container runtime çözülebiliyorsa build stage'i konteyner build'e döner (`podman build .` / `docker build .`); runtime admin ayarı `pipeline_container_runtime` (`podman`/`docker`/`auto` — auto PATH'te önce podman'ı dener).
- **QA gate**: `dispatcher.go`'daki `pipelineGate`, task `ready_for_qa`'ya taşındığında normalde hemen QA agent'larına dağıtılacak `task.moved` event'ini pipeline sonucuna kadar erteler (event yine de kaydedilir, board geçmişi eksiksiz kalır). Pipeline başarılıysa `PipelineRunner` → `DispatchQA` (`SkipPipelineGate: true`) QA agent run'larını oluşturur. Pipeline hata verirse task `need_revision`'a döner + başarısız stage log'unun tail-truncate edilmiş hali (stage başına 4000, toplam 3000 karakter) sistem yorumu olarak eklenir.
- **Hiç check tanımlı değilse (migration 067)**: repo'da validate/build/test job mapping'i yoksa `finishNoChecks` çalışır — provider `none`, tek `skipped` job ve pipeline status `skipped`. `success` DEĞİL: kapıyı yine açar (`domain.PipelineStatusOpensGate` → `DispatchQA` çalışır) ama UI'da yeşil rozet göstermez, çünkü hiçbir şey derlenmedi/test edilmedi. Eskiden `success` yazılıyordu ve "Success + Did not run" çelişkisi görünüyordu.
- **Agent görünürlüğü**: `get_pipeline_status` board tool'u (repo-scoped; task'ın son pipeline'ını + job sonuçlarını, hatalı job'ların tail-truncate log'uyla döner) ve `need_revision`'a düşen task'ların run prompt'una — son pipeline `failed` ise — otomatik "Pipeline failure" raporu enjekte edilir (`runner.go`).
- **API**: `GET/POST /v1/repositories/:id/tasks/:taskId/pipelines`, `GET .../pipelines/:pipelineId`; `BoardTask.latest_pipeline_status` alanı task listeleme endpoint'lerinde tek toplu sorguyla (N+1 yok) enjekte edilir, board kartında ikon olarak gösterilir.
- **Repo silme**: `RepositoriesPage` kart menüsü + repo ayarları "Tehlikeli Alan"ından silinebilir; `task_pipelines`/`task_pipeline_jobs` dahil `repositories`/`board_tasks`'a bağlı tüm FK'ler `ON DELETE CASCADE` — cascade audit'te eksik bulunmadı (Task 13).
- **Task workspace yok fallback'i**: `execute` önce `WorkspaceRoot/task-<id>`'yi dener, yoksa repo kökünü (`ResolveRootPath`) kullanır; ikisi de çözülemezse (ör. pipeline kuyrukta beklerken repo silinmişse) pipeline yan etkisiz `failed`/`interrupted` olarak kapatılır — komut çalıştırılmaz, QA tetiklenmez, task taşınmaz.
