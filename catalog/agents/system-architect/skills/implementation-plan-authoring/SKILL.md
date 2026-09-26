---
name: implementation-plan-authoring
category: architecture
description: Write the analysis report's implementation plan section - bite-sized, testable steps that assume the implementer has zero context
---
# Implementation Plan Authoring

## Overview

From the design sections, write the `plan` section of the ONE analysis report (analiz-html-report) — the same HTML document attached with `add_task_document`, `format: "html"`, titled `analiz: <YYYY-MM-DD> <topic>`. Never a separate `plan: …` document and never a file in the repo (you do not write or commit files during analysis). Write for a skilled developer who knows NOTHING about this codebase or problem domain and has questionable taste: document every file to touch, every interface, every command. DRY. YAGNI. TDD. Frequent commits — those commits are the implementer's, made later against the plan; the plan itself is never committed.

## File Structure First

Before defining tasks, map every file the plan creates or modifies and what each is responsible for — this locks in the decomposition:

- One clear responsibility per file; prefer smaller focused files over catch-alls.
- Files that change together live together; split by responsibility, not technical layer.
- In existing code follow established patterns — don't unilaterally restructure.

## Task Right-Sizing

A task is the smallest unit that carries its own test cycle and an independently testable deliverable. Fold setup/scaffolding into the task whose deliverable needs it. Split only where a reviewer could reject one task while approving its neighbor.

## Plan Header (mandatory)

The `plan` section opens with the goal, architecture and constraints before its first step:

```html
<section id="plan">
  <h2>Implementation plan</h2>
  <p><strong>Goal:</strong> one sentence. <strong>Architecture:</strong> 2–3 sentences. <strong>Tech stack:</strong> key technologies.</p>
  <h3 id="plan-constraints">Global constraints</h3>
  <ul><li>Project-wide rules copied VERBATIM from the design — version floors, locale/copy rules, layer boundaries — one line each. Every step implicitly includes this list.</li></ul>
  <ol class="steps"> … </ol>
</section>
```

## Task Structure

Each task is one step — `<li id="step-N">` in `<ol class="steps">` — and lists:
- **Files:** exact paths — Create / Modify (with line ranges when known) / Test.
- **Interfaces:** Consumes (what it uses from earlier tasks — exact signatures) and Produces (what later tasks rely on — exact names, parameter and return types). An implementer sees only their own task; this block is how they learn neighboring names.
- **Steps** as checkboxes, one action each (2–5 min): write the failing test (show the test code) → run it, expect FAIL with the expected message → write minimal implementation (show the code) → run it, expect PASS → commit (show the command).

## No Placeholders

These are plan failures — never write them:
- "TBD", "TODO", "implement later", "fill in details"
- "Add appropriate error handling" / "handle edge cases"
- "Write tests for the above" without the actual test code
- "Similar to Task N" — repeat the code; tasks may be read out of order
- References to types or functions not defined in any task

If a step changes code, the step shows the code.

## Worked Example (one task, bite-sized)

```html
<li id="step-2">
  <h3>TaskExporter service <span class="tag">backend-api</span></h3>
  <p><strong>Files:</strong> create <code>internal/application/export/service.go</code>; test <code>internal/application/export/service_test.go</code></p>
  <p><strong>Consumes:</strong> <code>TaskRepository.ListByProject(ctx, id) ([]Task, error)</code> (step 1) · <strong>Produces:</strong> <code>Export(ctx, projectID uuid.UUID) ([]byte, error)</code></p>
  <ol>
    <li>Failing test:
<pre><code>func (s *ExportSuite) TestExport_encodesHeaderAndRows() {
    s.repo.EXPECT().ListByProject(mock.Anything, s.pid).Return([]Task{{Title:"A"}}, nil)
    csv, err := s.svc.Export(s.ctx, s.pid)
    s.NoError(err); s.Contains(string(csv), "id,title,status,created_at"); s.Contains(string(csv), "A")
}</code></pre></li>
    <li>Run <code>go test ./internal/application/export/... -run TestExport</code> → FAIL (Export undefined)</li>
    <li>Minimal implementation (constructor + Export encoding header+rows)</li>
    <li>Run → PASS</li>
    <li>Commit: <code>feat: add TaskExporter service</code></li>
  </ol>
</li>
```

Every step shows the actual code/command — no "add error handling", no "similar to Task 1".

## Self-Review (mandatory)

1. **Design coverage** — for each requirement in the design sections, point to the step that implements it; add steps for gaps.
2. **Placeholder scan** — search the plan for the patterns above; fix them.
3. **Type consistency** — names/signatures used in later tasks match their definitions in earlier tasks (`clearLayers()` in Task 3 but `clearFullLayers()` in Task 7 is a bug).

Fix issues inline, make the `split` section match the steps, then the report is ready for the human; task-decomposition comes only after approval.
