---
name: incident-response
category: operations
description: How to work a production incident task — read the incident, correlate the deploy, propose a concrete remedy, and only then touch code.
---
# Incident response

A task titled `Incident: …` is a live production problem. Its description carries an
**incident id** and a first-pass hypothesis produced by the rules engine. That hypothesis
is a starting point, not a verdict.

## Order of work

1. **Read the incident first.** Call `get_incident` with the id from the task description.
   Read the raw payload, the occurrence count and the timeline before forming an opinion.
2. **Stop the bleeding before understanding it.** If a deploy landed shortly before the
   incident started, rolling back is the correct first action even when you do not yet
   know what the bad change was. Availability first, diagnosis second.
3. **Correlate.** Check `get_deploy_target` for the environment (provider, health URL,
   rollback command) and the recent deploy history. "What changed?" answers most incidents.
4. **Classify.** Config/credentials, dependency, capacity, or code defect. The classes
   need different fixes, and guessing the class wrong wastes the whole investigation.
5. **Write the remedy.** Call `propose_incident_remedy` with a summary (root cause + fix),
   executable steps (real commands, real file paths, real config keys), the evidence you
   relied on, and an honest confidence. If you are not sure, say so with a low confidence
   and list what you would need to check next — an honest "here is what I would check"
   is useful; a confident guess is not.

## Policy — this decides whether you are allowed to fix

- **suggest** (default): you must NOT change production code. Diagnose, record the remedy,
  and move the task to `human_uat` for the decision.
- **auto_fix**: record the remedy first, then implement it and take it through the normal
  pipeline. Tests and review still apply — a hotfix that skips them causes the next incident.

The task description states which policy is in force. When in doubt, treat it as suggest.

## Rules

- Never mark an incident resolved because you shipped a fix. Call `resolve_incident` only
  after checking the environment is actually healthy again, and say how you verified it.
- A recurring incident (occurrences > 1, or a prior resolved incident with the same
  fingerprint) means the previous fix did not hold: fix the cause, not the symptom, and
  say explicitly why this time is different.
- Never write a remedy you cannot execute step by step. "Investigate the database" is not
  a remedy; "connection pool is capped at 10 while the new worker opens 25 — raise
  `DB_MAX_CONNS` to 40 in the prod config" is.
- If the incident payload is not enough to conclude anything, say what specific log,
  metric or access you need. Do not invent a cause to have something to write.
