package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/web"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
)

// TestDownloadFileGuardProseUnchanged pins download_file's exact refusal
// wording, ahead of moving it into catalog/system.
func TestDownloadFileGuardProseUnchanged(t *testing.T) {
	ws := t.TempDir()
	ctx := registry.ContextWithWorkspaceDir(context.Background(), ws)

	// .git protected path.
	tool := web.NewDownloadTool(web.WithURLPolicy(localPolicy()))
	res := tool.Execute(ctx, `{"url":"https://example.com/x.png","path":".git/hooks/x"}`)
	if want := `".git" is protected: download_file may not write into it`; res.Content != want {
		t.Errorf(".git protection = %q, want %q", res.Content, want)
	}

	// HTML instead of binary asset.
	htmlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer htmlSrv.Close()
	res2 := tool.Execute(ctx, `{"url":"`+htmlSrv.URL+`/badge.png","path":"badge.png"}`)
	want2 := "the URL returned an HTML page, not a binary asset — nothing was saved. " +
		"You are probably holding the page that SHOWS the asset. Find the direct asset URL " +
		"(it usually ends in .png, .svg, .jpg or .woff2) and call download_file with that."
	if res2.Content != want2 {
		t.Errorf("HTML-not-binary = %q, want %q", res2.Content, want2)
	}

	// Success message.
	pngSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 0x50, 0x4E, 0x47})
	}))
	defer pngSrv.Close()
	res3 := tool.Execute(ctx, `{"url":"`+pngSrv.URL+`/badge.png","path":"badge.png"}`)
	if !strings.Contains(res3.Content, "That IS the confirmation — do not re-download or re-read it. "+
		"Reference it from the code, then verify the page actually renders it (browser_screenshot reports broken images).") {
		t.Errorf("saved message = %q", res3.Content)
	}

	// Over the size limit.
	bigSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(make([]byte, 10<<20+1))
	}))
	defer bigSrv.Close()
	res4 := tool.Execute(ctx, `{"url":"`+bigSrv.URL+`/big.bin","path":"big.bin"}`)
	want4 := "the asset is larger than the 10 MB download limit — nothing was saved. This is not the kind of file to vendor into the repository."
	if res4.Content != want4 {
		t.Errorf("size-limit message = %q, want %q", res4.Content, want4)
	}
}
