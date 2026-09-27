---
key: guard.web_download_html_not_binary
version: 1
---
the URL returned an HTML page, not a binary asset — nothing was saved. You are probably holding the page that SHOWS the asset. Find the direct asset URL (it usually ends in .png, .svg, .jpg or .woff2) and call download_file with that.
