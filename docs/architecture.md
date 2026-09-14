# Architecture

One machine, three processes, one window.

```
TaskTrooper.app (Electron main)
 ├─ embedder      node child · onnxruntime-web · nomic-embed-text-v1.5 · 127.0.0.1:<p1>
 ├─ agent-server  Go child   · HTTP API + board + agent loop · 127.0.0.1:<p2>
 │    └─ postgres  started by agent-server from the zonky binaries · 127.0.0.1:<p3>
 │    └─ claude    one headless `claude -p` per running task, MCP back to <p2>/mcp
 └─ appium        optional, only when installed · 127.0.0.1:4723
 window ── app://tasktrooper (ui/dist) ── fetch → http://127.0.0.1:<p2>
```

## Boot

| step | who | signal |
|---|---|---|
| register `app://` scheme, serve `ui/dist` | shell | — |
| load or create `local.bin` (`api_token`, `mcp_secrets_key`) | shell | Keychain via `safeStorage` |
| spawn embedder | shell | stdout `EMBEDDER_LISTENING <port>` |
| spawn agent-server with `PORT=0 DATA_DIR SERVER_API_KEY MCP_SECRETS_KEY EMBEDDINGS_BASE_URL CLAUDE_CODE_BIN …` | shell | stdout `LISTENING http://127.0.0.1:<port>` |
| start embedded Postgres, run migrations, seed the local tenant | agent-server | `GET /health` → 200 |
| attach the web view, hand the page `apiBase` + `apiToken` | shell | `window.__tasktrooperDesktop` |

Stop order is the reverse of start: agent-server drains its Claude sessions on
SIGTERM (30 s budget), then Postgres, then appium; the embedder lives until quit.

## Identity

There is one tenant (`tenant.LocalTenantID`) and one role (owner). Every
request is stamped with it after the bearer check; every store call runs
`SET LOCAL app.tenant_id` so the schema's row-level-security columns keep their
defaults. Postgres runs as a superuser locally, so RLS is not enforced — it is a
multi-tenant guard the local product does not need.

## Data

`DATA_DIR` (the app: `~/Library/Application Support/TaskTrooper/data`; `make dev`:
`server/data`):

```
postgres/       cluster (pgdata)
postgres-bin/   zonky download + extracted runtime
workspaces/     git checkouts, one per task
files/          RAG uploads
```

Delete `DATA_DIR` to start over.

## Contracts that move together

- `desktop/src/ipc/host.ts` ↔ `desktop/ui/src/lib/desktop-bridge.ts`
- `server/README.md` (env + stdout) ↔ `desktop/src/main/supervisor/env.ts`, `server-log.ts`
- `desktop/embedder` (`POST /v1/embeddings`, `GET /v1/models`) ↔ the server's OpenAI-compatible embedding provider
