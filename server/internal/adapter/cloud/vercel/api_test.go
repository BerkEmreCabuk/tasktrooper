package vercel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, HTTP: srv.Client()}
}

func TestUserRequiresBearer(t *testing.T) {
	var gotAuth string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v2/user" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"user":{"id":"u1","username":"akif","email":"a@b.c"}}`))
	}))
	u, err := c.User(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok" || u.Username != "akif" || u.ID != "u1" {
		t.Fatalf("user = %+v auth = %q", u, gotAuth)
	}
}

func TestUnauthorizedIsDetectable(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"Not authorized"}}`))
	}))
	_, err := c.User(context.Background(), "bad")
	if err == nil || !IsUnauthorized(err) {
		t.Fatalf("expected unauthorized, got %v", err)
	}
	if err.Error() != "vercel api: 403 Not authorized" {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestProjectsPaginatesAndMapsLink(t *testing.T) {
	calls := 0
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("teamId") != "team_1" {
			t.Errorf("teamId missing: %s", r.URL.RawQuery)
		}
		switch r.URL.Query().Get("until") {
		case "":
			// A full page with a cursor: the client must ask for the next one.
			var b strings.Builder
			b.WriteString(`{"projects":[`)
			for i := 0; i < 100; i++ {
				if i > 0 {
					b.WriteString(",")
				}
				b.WriteString(`{"id":"prj_` + string(rune('a'+i%26)) + `","name":"p","link":{"type":"github","org":"acme-org","repo":"web","productionBranch":"main"},"rootDirectory":"apps/web/","targets":{"production":{"url":"p-abc.vercel.app","alias":["p.vercel.app","app.example.com"]}}}`)
			}
			b.WriteString(`],"pagination":{"next":1700}}`)
			_, _ = w.Write([]byte(b.String()))
		case "1700":
			_, _ = w.Write([]byte(`{"projects":[{"id":"prj_last","name":"last","framework":"nextjs"}],"pagination":{"next":0}}`))
		default:
			t.Errorf("unexpected until %s", r.URL.Query().Get("until"))
		}
	}))
	projects, err := c.Projects(context.Background(), "tok", "team_1")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(projects) != 101 {
		t.Fatalf("calls=%d projects=%d", calls, len(projects))
	}
	first := projects[0]
	if first.Link == nil || first.Link.Slug() != "acme-org/web" || first.RootDirectory != "apps/web" {
		t.Fatalf("first = %+v link=%+v", first, first.Link)
	}
	if first.ProductionURL != "https://app.example.com" {
		t.Fatalf("production url = %s (custom domain must win over *.vercel.app)", first.ProductionURL)
	}
	last := projects[100]
	if last.ProductionURL != "https://last.vercel.app" || last.Framework != "nextjs" || last.TeamID != "team_1" {
		t.Fatalf("last = %+v", last)
	}
}

func TestProjectEscapesID(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/v9/projects/prj_1%2Fx" {
			t.Errorf("path = %s", r.URL.EscapedPath())
		}
		_, _ = w.Write([]byte(`{"id":"prj_1/x","name":"n"}`))
	}))
	p, err := c.Project(context.Background(), "tok", "", "prj_1/x")
	if err != nil || p.ID != "prj_1/x" {
		t.Fatalf("p=%+v err=%v", p, err)
	}
}
