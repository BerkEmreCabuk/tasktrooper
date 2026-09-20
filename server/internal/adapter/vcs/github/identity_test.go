package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The numeric id is the half that used to be thrown away, and it is the half
// the noreply address cannot be built without.
func TestUserIdentityReadsLoginAndID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"login": "makifbaysal", "id": 12345})
	}))
	defer srv.Close()

	original := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = original })

	id, err := UserIdentity(context.Background(), "tok")
	if err != nil {
		t.Fatalf("UserIdentity: %v", err)
	}
	if id.Login != "makifbaysal" || id.ID != 12345 {
		t.Fatalf("identity = %+v", id)
	}
	if got := id.NoReplyEmail(); got != "12345+makifbaysal@users.noreply.github.com" {
		t.Fatalf("noreply email = %s", got)
	}
}

// An incomplete identity must not be written into a commit that can never be
// rewritten; callers fall back on "".
func TestNoReplyEmailEmptyWhenIncomplete(t *testing.T) {
	for _, id := range []Identity{{}, {Login: "makifbaysal"}, {ID: 1}} {
		if got := id.NoReplyEmail(); got != "" {
			t.Fatalf("identity %+v gave %q, want empty", id, got)
		}
	}
}
