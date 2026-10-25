---
key: guard.http_request_loopback_only
version: 1
---
http_request only reaches this machine's own loopback address (localhost, 127.0.0.1, ::1) — refused for any other destination, including the local network and the public internet. Use it to call a service this task started locally (start_task_preview, a dev server you launched). Use fetch_url for a public URL.
