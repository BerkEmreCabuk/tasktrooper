---
key: tool.download_file
version: "1"
params:
    path: Destination file path, relative to the workspace root (e.g. public/images/google-play-badge.png)
    url: Direct URL of the asset to download (must start with http:// or https://)
---
Download a binary asset (image, font, icon, archive) from a URL and save it to a path inside the workspace. Use this — not fetch_url, not curl/wget — whenever the repository needs a real asset file: fetch_url returns text and corrupts binaries. The URL must point at the asset itself (ends in .png/.svg/.woff2/…), not at a page that shows it.
