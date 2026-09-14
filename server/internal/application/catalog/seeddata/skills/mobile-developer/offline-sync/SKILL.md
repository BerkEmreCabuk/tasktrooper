---
name: offline-sync
category: mobile
description: Offline resilience and sync
---

# Offline and Sync

- Queue mutations when offline and replay on reconnect; every queued action is idempotent or deduplicated.
- Show sync status honestly: pending, syncing, failed — never pretend an unsynced change is saved.
- Resolve conflicts with last-write-wins or explicit user choice; pick one per data type and document it in the task.
- Reads degrade gracefully: cached data with a staleness indicator beats a spinner that never resolves.
