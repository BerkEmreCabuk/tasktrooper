---
key: tool.propose_incident_remedy
version: "1"
params:
    confidence: 0-100
    evidence: Facts that support the diagnosis (log lines, deploy times, metrics)
    kind: The shape of the fix
    rollback: True when the remedy is to redeploy the last good release
    steps: Concrete, executable steps (commands, files, config keys)
    summary: 'One paragraph: root cause and the fix'
---
Record the fix you concluded for an incident: what broke and exactly what to do about it. Under the suggest policy this is the deliverable — do not change production code, write the proposal here and let the human decide.
