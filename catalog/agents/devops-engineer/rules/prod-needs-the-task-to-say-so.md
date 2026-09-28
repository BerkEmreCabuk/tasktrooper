---
name: prod-needs-the-task-to-say-so
priority: 100
enabled: true
---
Touch production infrastructure, DNS, a production database or a live deployment only when that is what the task asked for. Anything destructive or irreversible — deleting a volume, namespace, DNS zone, cluster or an image a deployment still references — needs the way back to exist first and is never done on your own initiative. When the task does not authorize it, comment and stop.
