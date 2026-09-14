package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/web"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
)

// fakePNG is a minimal binary payload with bytes UTF-8 would mangle — the
// exact class of content fetch_url cannot carry and this tool exists for.
var fakePNG = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0xFF, 0xFE}

type DownloadToolSuite struct {
	suite.Suite
	workspace string
	ctx       context.Context
}

func (s *DownloadToolSuite) SetupTest() {
	s.workspace = s.T().TempDir()
	s.ctx = registry.ContextWithWorkspaceDir(context.Background(), s.workspace)
}

func (s *DownloadToolSuite) TestName() {
	s.Equal("download_file", web.NewDownloadTool().Name())
}

func (s *DownloadToolSuite) TestSavesBinaryAssetVerbatim() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(fakePNG)
	}))
	defer srv.Close()

	tool := web.NewDownloadTool(web.WithURLPolicy(localPolicy()))
	result := tool.Execute(s.ctx, `{"url":"`+srv.URL+`/badge.png","path":"public/images/badge.png"}`)
	s.False(result.IsError, result.Content)
	s.Contains(result.Content, "public/images/badge.png")
	s.Contains(result.Content, "image/png")

	saved, err := os.ReadFile(filepath.Join(s.workspace, "public/images/badge.png"))
	s.Require().NoError(err)
	s.Equal(fakePNG, saved, "binary payload must be written byte-for-byte")
}

func (s *DownloadToolSuite) TestRefusesHTMLLandingPage() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body>not a badge</body></html>"))
	}))
	defer srv.Close()

	tool := web.NewDownloadTool(web.WithURLPolicy(localPolicy()))
	result := tool.Execute(s.ctx, `{"url":"`+srv.URL+`/badge.png","path":"badge.png"}`)
	s.True(result.IsError)
	s.Contains(result.Content, "HTML page")
	s.NoFileExists(filepath.Join(s.workspace, "badge.png"))
}

func (s *DownloadToolSuite) TestRefusesErrorStatus() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	tool := web.NewDownloadTool(web.WithURLPolicy(localPolicy()))
	result := tool.Execute(s.ctx, `{"url":"`+srv.URL+`/gone.png","path":"gone.png"}`)
	s.True(result.IsError)
	s.Contains(result.Content, "404")
	s.NoFileExists(filepath.Join(s.workspace, "gone.png"))
}

func (s *DownloadToolSuite) TestConfinesPathToWorkspace() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(fakePNG)
	}))
	defer srv.Close()

	tool := web.NewDownloadTool(web.WithURLPolicy(localPolicy()))
	result := tool.Execute(s.ctx, `{"url":"`+srv.URL+`","path":"../outside.png"}`)
	s.True(result.IsError)
	s.NoFileExists(filepath.Join(filepath.Dir(s.workspace), "outside.png"))
}

func (s *DownloadToolSuite) TestRefusesGitPath() {
	tool := web.NewDownloadTool(web.WithURLPolicy(localPolicy()))
	result := tool.Execute(s.ctx, `{"url":"http://example.com/a.png","path":".git/hooks/a.png"}`)
	s.True(result.IsError)
	s.Contains(result.Content, "protected")
}

func (s *DownloadToolSuite) TestRequiresWorkspaceContext() {
	tool := web.NewDownloadTool(web.WithURLPolicy(localPolicy()))
	result := tool.Execute(context.Background(), `{"url":"http://example.com/a.png","path":"a.png"}`)
	s.True(result.IsError)
	s.Contains(result.Content, "workspace directory")
}

func TestDownloadToolSuite(t *testing.T) {
	suite.Run(t, new(DownloadToolSuite))
}
