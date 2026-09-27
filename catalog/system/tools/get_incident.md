---
key: tool.get_incident
version: "1"
params:
    incident_id: Incident UUID (it is printed in the task description)
---
Read one production incident in full: raw alert payload, timeline, occurrence count and the current remedy hypothesis. Call this first when working an incident task.
