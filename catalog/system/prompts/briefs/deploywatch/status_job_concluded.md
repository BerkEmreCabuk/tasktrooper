---
key: briefs.deploywatch.status_job_concluded
version: 1
inputs: [JobName, SHA, Conclusion]
---
The deploy job {{printf "%q" .JobName}} of {{.SHA}} concluded {{printf "%q" .Conclusion}}.
