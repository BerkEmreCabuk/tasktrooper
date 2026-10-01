---
name: security-review
category: quality
description: Use in code_review when the diff touches input handling, auth, queries, files, outbound requests, secrets, dependencies or LLM calls, and in analysis for any new endpoint or data flow
source: getsentry/skills find-bugs (Apache-2.0), anthropics/claude-plugins-official (Apache-2.0), github/awesome-copilot se-security-reviewer (MIT), adapted; OWASP category names linked only
---
# Security Review

## Overview

Security is not a separate pass over the whole repo — it is one more lens on the diff (code_review) or the design (analysis) you are already reading. Report only what is reachable by an attacker; a theoretical issue with no path to it is noise that trains people to skip your findings.

## 1. Risk Tier First

- **High** — auth, payments, admin actions, file upload, outbound URL fetch, anything that calls an LLM with tool access. Run the full checklist below.
- **Medium** — user data, external API calls. Run Injection, Authorization, Exceptional Conditions, Secrets.
- **Low** — pure UI, utilities, pure config/copy. Run Secrets and Supply Chain only.

## 2. Map the Attack Surface (of the diff, not the repo)

List, from the diff: every new input (params, headers, body, path segment, uploaded file, webhook payload, LLM output used downstream), every new or changed query, every auth/authorization check, every outbound call, every secret touched.

## 3. Report Only Attacker-Reachable Findings

Trace the value to its source with `grep_code`/`expand_symbol_context` before flagging it. **Not attacker-controlled:** config values, environment variables, constants, server-set values, test fixtures. **Not a finding:** framework auto-escaping doing its job (React `{x}`, an ORM's parameterized query) — that is the control working, not a gap.

## 4. Checks

**Authorization per new route** — does it carry the same middleware as its neighbours, and does the query filter by owner/tenant (IDOR)?
- ❌ `func (h *Handler) GetInvoice(w, r) { id := r.PathValue("id"); inv, _ := h.repo.Get(ctx, id); json.NewEncoder(w).Encode(inv) }` — any authenticated user reads any invoice by guessing an id.
- ✅ `inv, err := h.repo.Get(ctx, id); if inv.OrgID != authctx.OrgID(ctx) { return ErrForbidden }` — the query result is checked against the caller's scope before it is returned.

**Injection sinks** — string-built SQL, `exec.Command("sh","-c",…)` / `child_process.exec`, template `|safe`, `dangerouslySetInnerHTML`, `v-html`.
- ❌ `db.Exec(fmt.Sprintf("SELECT * FROM tasks WHERE title = '%s'", title))`
- ✅ `db.Exec("SELECT * FROM tasks WHERE title = $1", title)`

**Path traversal, SSRF, open redirect** — a path or URL built from user input without an allowlist or a resolved-path check.
- ❌ `http.ServeFile(w, r, filepath.Join(baseDir, r.URL.Query().Get("file")))` — `../../etc/passwd` escapes `baseDir`.
- ✅ resolve the joined path with `filepath.Clean`/`EvalSymlinks` and reject anything outside `baseDir`.

**Secrets** — in code, logs, error bodies, or **argv** (this repo's own rule: secrets never go on argv; a child process gets them via env, not a command-line flag).
- ❌ `exec.Command("curl", "-H", "Authorization: Bearer "+token, url)`
- ✅ pass the token through an env var the child reads, never a flag.

**Crypto** — `math/rand`/`Math.random` used for a token or session id; MD5/SHA1 for a password hash.

**CSRF / CORS** — a cookie-authenticated state change with no CSRF check; `Access-Control-Allow-Origin: *` alongside credentialed requests.

**Supply chain** — a new dependency: pinned in the lockfile, maintained, license-compatible, not a typo-squat of a popular name, and actually in scope (an out-of-scope list that forbids unrequested dependency changes still applies).

**Exceptional conditions** — an error swallowed into a 200/success response; auth that fails open (an unreachable check defaults to "allowed" instead of "denied").
- ❌ `if err != nil { log.Error(err) }` then falls through and returns 200 anyway.
- ✅ the error is propagated and mapped to the right status code.

**LLM-specific** (any diff that calls a model or exposes a tool to one) — model output used directly as a shell command, a SQL fragment, or a URL; a tool callable on untrusted content (e.g. a webhook body) with no confirmation step. See OWASP Top 10 for LLM Applications, LLM01 Prompt Injection.

## 5. Severity

- **Critical** — exploitable without authentication, an auth bypass, an injection sink reachable by input, or a leaked secret.
- **Critical when it reaches data, Important otherwise** — exploitable only with conditions (IDOR on a specific id, stored XSS, SSRF to an internal service).
- **Minor** — defence-in-depth gaps (a check that is redundant with one already upstream).

## 6. Before the Verdict

State to yourself, not in the comment: which areas you could not verify and why (e.g. "didn't trace the mobile client's handling of this field — out of this diff's repo"). An unverifiable area is not a finding; it's a boundary of the review.

## References (names and links only — OWASP content is CC BY-SA, not reproduced)

[OWASP Top 10:2025](https://top10.owasp.org/2025/): A01 Broken Access Control · A03 Software Supply Chain Failures · A04 Cryptographic Failures · A05 Injection · A06 Insecure Design · A07 Authentication Failures · A08 Software/Data Integrity Failures · A09 Security Logging and Alerting Failures · A10 Mishandling of Exceptional Conditions. [OWASP Top 10 for LLM Applications](https://genai.owasp.org/): LLM01 Prompt Injection.

## Common Mistakes

- Flagging a value from `os.Getenv` or a hardcoded constant as attacker-controlled.
- Flagging React's `{value}` interpolation or an ORM's parameter binding as an injection risk — that is the escaping working.
- A Critical for a low-tier diff (pure copy/UI change) that has no reachable input at all.
- Running the full checklist on a low-risk diff instead of just Secrets and Supply Chain.

## Red Flags

- A finding with no traced source for the "attacker-controlled" claim.
- A new outbound call (`fetch_url`-equivalent, `http.Client`) with no mention of where the URL comes from.
- A new dependency with no lockfile entry checked.
- "Might be exploitable" with no file:line and no input trace — rewrite it with both, or drop it.
