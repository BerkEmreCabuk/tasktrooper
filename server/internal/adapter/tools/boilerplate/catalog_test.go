package boilerplate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/suite"
)

type fakeSettings struct {
	repo string
	err  error
}

func (f fakeSettings) Get(ctx context.Context) (domain.AppSettings, error) {
	if f.err != nil {
		return domain.AppSettings{}, f.err
	}
	return domain.AppSettings{BoilerplateCatalogRepo: f.repo}, nil
}

const sampleCatalog = `
version: 1
boilerplates:
  - id: go-fiber
    path: backend/go-fiber
    type: backend
    language: go
    framework: fiber-v3
    tags: [go, rest, grpc, graphql]
    description: Go REST/gRPC/GraphQL backend.
    status: available
  - id: flutter
    path: mobile/flutter
    type: mobile
    language: dart
    framework: flutter
    tags: [dart, mobile]
    description: Flutter Task CRUD app.
    status: available
`

func newToolWithServer(t *testing.T, srv *httptest.Server, repo string) *catalogTool {
	t.Helper()
	return &catalogTool{
		settings:   fakeSettings{repo: repo},
		httpClient: srv.Client(),
		maxBytes:   1048576,
		rawBaseURL: srv.URL,
	}
}

type CatalogToolSuite struct {
	suite.Suite
}

func (s *CatalogToolSuite) TestName() {
	tool := &catalogTool{settings: fakeSettings{}}
	s.Equal(ToolName, tool.Name())
}

func (s *CatalogToolSuite) TestDefinition() {
	tool := &catalogTool{settings: fakeSettings{}}
	def := tool.Definition()
	s.Equal("function", def.Type)
	s.Equal(ToolName, def.Function.Name)
}

func (s *CatalogToolSuite) TestExecuteListsAllWhenNoQuery() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Equal("/acme/boilerplates/main/.ai/catalog.yaml", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleCatalog))
	}))
	defer srv.Close()

	tool := newToolWithServer(s.T(), srv, "acme/boilerplates")
	result := tool.Execute(context.Background(), `{}`)
	s.False(result.IsError)
	s.Contains(result.Content, "go-fiber")
	s.Contains(result.Content, "flutter")
}

func (s *CatalogToolSuite) TestExecuteFiltersByQuery() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleCatalog))
	}))
	defer srv.Close()

	tool := newToolWithServer(s.T(), srv, "acme/boilerplates")
	result := tool.Execute(context.Background(), `{"query":"flutter"}`)
	s.False(result.IsError)
	s.Contains(result.Content, "flutter")
	s.NotContains(result.Content, "go-fiber")
}

func (s *CatalogToolSuite) TestExecuteFallsBackToMasterBranch() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/acme/legacy/main/.ai/catalog.yaml" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		s.Equal("/acme/legacy/master/.ai/catalog.yaml", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleCatalog))
	}))
	defer srv.Close()

	tool := newToolWithServer(s.T(), srv, "acme/legacy")
	result := tool.Execute(context.Background(), `{}`)
	s.False(result.IsError)
	s.Contains(result.Content, `"branch":"master"`)
}

func (s *CatalogToolSuite) TestExecuteUpstreamNotFoundOnBothBranches() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	tool := newToolWithServer(s.T(), srv, "acme/missing")
	result := tool.Execute(context.Background(), `{}`)
	s.True(result.IsError)
}

func (s *CatalogToolSuite) TestExecuteInvalidArguments() {
	tool := &catalogTool{settings: fakeSettings{}}
	result := tool.Execute(context.Background(), `not json`)
	s.True(result.IsError)
}

func (s *CatalogToolSuite) TestExecuteMalformedRepoSetting() {
	tool := &catalogTool{settings: fakeSettings{repo: "not-a-valid-repo-ref!!"}}
	result := tool.Execute(context.Background(), `{}`)
	s.True(result.IsError)
	s.Contains(result.Content, "must look like")
}

func (s *CatalogToolSuite) TestParseGitHubRepoVariants() {
	cases := map[string][2]string{
		"owner/repo":                        {"owner", "repo"},
		"github.com/owner/repo":             {"owner", "repo"},
		"https://github.com/owner/repo":     {"owner", "repo"},
		"https://github.com/owner/repo.git": {"owner", "repo"},
	}
	for input, want := range cases {
		owner, repo, err := parseGitHubRepo(input)
		s.NoError(err, input)
		s.Equal(want[0], owner, input)
		s.Equal(want[1], repo, input)
	}
}

func TestCatalogToolSuite(t *testing.T) {
	suite.Run(t, new(CatalogToolSuite))
}

func TestGroupTreeMatches(t *testing.T) {
	paths := []string{
		"go-fiber/sonar-project.properties",
		"go-fiber/main.go",
		"next-app/sonar-project.properties",
		"README.md",
	}
	out := groupTreeMatches(paths, "SONAR")
	if len(out) != 2 {
		t.Fatalf("want 2 boilerplates, got %v", out)
	}
	if len(out["go-fiber"]) != 1 || out["go-fiber"][0] != "go-fiber/sonar-project.properties" {
		t.Fatalf("go-fiber match wrong: %v", out["go-fiber"])
	}
	if len(out["next-app"]) != 1 {
		t.Fatalf("next-app match wrong: %v", out["next-app"])
	}
}

func TestGroupTreeMatchesRootFile(t *testing.T) {
	out := groupTreeMatches([]string{"catalog.yaml"}, "catalog")
	if len(out["(repo root)"]) != 1 {
		t.Fatalf("root file not grouped: %v", out)
	}
}
