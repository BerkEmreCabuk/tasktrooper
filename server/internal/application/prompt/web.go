package prompt

var webDownloadGitProtectedKey = Define[struct{}]("guard.web_download_git_protected", struct{}{})
var webDownloadHTMLNotBinaryKey = Define[struct{}]("guard.web_download_html_not_binary", struct{}{})

var httpRequestLoopbackOnlyKey = Define[struct{}]("guard.http_request_loopback_only", struct{}{})
var httpRequestFailedKey = Define[struct{}]("guard.http_request_failed", struct{}{})

type httpRequestTruncatedInput struct{ LimitKB int }

var httpRequestTruncatedKey = Define("tool_results.http_request_truncated", httpRequestTruncatedInput{LimitKB: 16})

type webDownloadSizeLimitInput struct{ LimitMB int }

var webDownloadSizeLimitKey = Define("guard.web_download_size_limit", webDownloadSizeLimitInput{LimitMB: 10})

type webDownloadSavedInput struct {
	Path        string
	Bytes       int
	ContentType string
}

var webDownloadSavedKey = Define("tool_results.web_download_saved", webDownloadSavedInput{Path: "public/logo.png", Bytes: 4096, ContentType: "image/png"})

// WebDownloadGitProtectedText is download_file's refusal for a destination
// path inside .git.
func WebDownloadGitProtectedText() string { return Text(webDownloadGitProtectedKey) }

// WebDownloadHTMLNotBinaryText is download_file's refusal when the URL
// answered with an HTML page instead of the binary asset it named.
func WebDownloadHTMLNotBinaryText() string { return Text(webDownloadHTMLNotBinaryKey) }

// WebDownloadSizeLimitText is download_file's refusal for a body over
// downloadMaxBytes.
func WebDownloadSizeLimitText(limitMB int) string {
	return webDownloadSizeLimitKey.Render(webDownloadSizeLimitInput{LimitMB: limitMB})
}

// WebDownloadSavedText is download_file's success message.
func WebDownloadSavedText(path string, bytes int, contentType string) string {
	return webDownloadSavedKey.Render(webDownloadSavedInput{Path: path, Bytes: bytes, ContentType: contentType})
}

// HTTPRequestLoopbackOnlyText is http_request's refusal for a destination
// that is not this machine's own loopback address. It names no part of the
// rejected URL — see fetchFailed's comment in web.go for why.
func HTTPRequestLoopbackOnlyText() string { return Text(httpRequestLoopbackOnlyKey) }

// HTTPRequestFailedText is http_request's refusal when a loopback-validated
// request could not be built, sent or read back.
func HTTPRequestFailedText() string { return Text(httpRequestFailedKey) }

// HTTPRequestTruncatedText is appended to http_request's result when the
// response body exceeded httpRequestMaxBodyBytes.
func HTTPRequestTruncatedText(limitKB int) string {
	return httpRequestTruncatedKey.Render(httpRequestTruncatedInput{LimitKB: limitKB})
}
