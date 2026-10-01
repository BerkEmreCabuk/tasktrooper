package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/web"
)

type HTTPRequestToolSuite struct {
	suite.Suite
}

func TestHTTPRequestToolSuite(t *testing.T) {
	suite.Run(t, new(HTTPRequestToolSuite))
}

func (s *HTTPRequestToolSuite) TestName() {
	tool := web.NewHTTPRequestTool()
	s.Equal("http_request", tool.Name())
}

// Production's own policy (urlguard.LoopbackOnly, no override) must already
// admit an httptest server — it binds 127.0.0.1, unlike fetch_url's default
// which refuses loopback until ALLOW_LOOPBACK_TOOL_URLS is set.
func (s *HTTPRequestToolSuite) TestLoopbackAllowedByDefault() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Equal("GET", r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	tool := web.NewHTTPRequestTool()
	result := tool.Execute(context.Background(), `{"method":"GET","url":"`+srv.URL+`"}`)
	s.False(result.IsError)
	s.Contains(result.Content, "200 OK")
	s.Contains(result.Content, `{"ok":true}`)
	s.Contains(result.Content, "Content-Type: application/json")
}

func (s *HTTPRequestToolSuite) TestMethodHeadersAndBodyAreSent() {
	var gotMethod, gotHeader, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotHeader = r.Header.Get("X-Task")
		buf := make([]byte, 64)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	tool := web.NewHTTPRequestTool()
	args := `{"method":"POST","url":"` + srv.URL + `","headers":{"X-Task":"T-12"},"body":"payload"}`
	result := tool.Execute(context.Background(), args)
	s.False(result.IsError)
	s.Contains(result.Content, "201 Created")
	s.Equal("POST", gotMethod)
	s.Equal("T-12", gotHeader)
	s.Equal("payload", gotBody)
}

// External destinations are refused even as a literal IP — no DNS lookup is
// needed to reject it, so this stays hermetic.
func (s *HTTPRequestToolSuite) TestExternalDestinationRefused() {
	tool := web.NewHTTPRequestTool()
	result := tool.Execute(context.Background(), `{"method":"GET","url":"http://93.184.216.34/"}`)
	s.True(result.IsError)
	s.Contains(result.Content, "loopback")
	s.NotContains(result.Content, "93.184.216.34")
}

func (s *HTTPRequestToolSuite) TestPrivateLANDestinationRefused() {
	tool := web.NewHTTPRequestTool()
	result := tool.Execute(context.Background(), `{"method":"GET","url":"http://192.168.1.1/"}`)
	s.True(result.IsError)
	s.Contains(result.Content, "loopback")
}

// A redirect that leaves loopback must be refused too — CheckRedirect
// re-validates every hop against the same LoopbackOnly policy.
func (s *HTTPRequestToolSuite) TestRedirectToExternalRefused() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://93.184.216.34/evil", http.StatusFound)
	}))
	defer srv.Close()

	tool := web.NewHTTPRequestTool()
	result := tool.Execute(context.Background(), `{"method":"GET","url":"`+srv.URL+`"}`)
	s.True(result.IsError)
	s.Contains(result.Content, "could not complete")
}

func (s *HTTPRequestToolSuite) TestResponseBodyTruncated() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("a", 20<<10)))
	}))
	defer srv.Close()

	tool := web.NewHTTPRequestTool()
	result := tool.Execute(context.Background(), `{"method":"GET","url":"`+srv.URL+`"}`)
	s.False(result.IsError)
	s.Contains(result.Content, "truncated at 16 KB")
	s.LessOrEqual(len(result.Content), 16<<10+2048)
}

func (s *HTTPRequestToolSuite) TestTimeoutExceeded() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(80 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tool := web.NewHTTPRequestTool()
	result := tool.Execute(context.Background(), `{"method":"GET","url":"`+srv.URL+`","timeout_seconds":0.01}`)
	s.True(result.IsError)
	s.Contains(result.Content, "could not complete")
}

func (s *HTTPRequestToolSuite) TestTimeoutSecondsOverMaxRefused() {
	tool := web.NewHTTPRequestTool()
	result := tool.Execute(context.Background(), `{"method":"GET","url":"http://127.0.0.1:1","timeout_seconds":31}`)
	s.True(result.IsError)
	s.Contains(result.Content, "timeout_seconds must be between 0 and 30")
}

func (s *HTTPRequestToolSuite) TestMethodRequired() {
	tool := web.NewHTTPRequestTool()
	result := tool.Execute(context.Background(), `{"method":"","url":"http://127.0.0.1:1"}`)
	s.True(result.IsError)
	s.Contains(result.Content, "method and url are both required")
}

func (s *HTTPRequestToolSuite) TestMethodNotAllowed() {
	tool := web.NewHTTPRequestTool()
	result := tool.Execute(context.Background(), `{"method":"TRACE","url":"http://127.0.0.1:1"}`)
	s.True(result.IsError)
	s.Contains(result.Content, `"TRACE" is not one of`)
}

func (s *HTTPRequestToolSuite) TestInvalidArguments() {
	tool := web.NewHTTPRequestTool()
	result := tool.Execute(context.Background(), `not json`)
	s.True(result.IsError)
	s.Contains(result.Content, "invalid arguments")
}
