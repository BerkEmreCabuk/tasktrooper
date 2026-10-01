---
key: tool.http_request
version: "1"
params:
    method: "HTTP method: GET, POST, PUT, PATCH, DELETE, HEAD or OPTIONS"
    url: "The full URL to call — must resolve to this machine's loopback address (localhost, 127.0.0.1, ::1); any other destination is refused"
    headers: "Optional request headers as a flat string map"
    body: "Optional request body, sent as-is (set a Content-Type header to say what it is)"
    timeout_seconds: "Optional request timeout in seconds, 30 at most; defaults to 10"
---
Send one HTTP request to a service running on THIS machine (a preview you started with start_task_preview, a dev server you launched) and return its status line, a few response headers and its body (truncated past 16 KB). Loopback only — any other host is refused. Use it to check what an endpoint actually returns: happy path, invalid input, unauthorized, each acceptance criterion. Not for the public internet — use fetch_url for that.
