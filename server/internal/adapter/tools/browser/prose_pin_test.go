package browser

import "testing"

// TestGuardProseUnchanged pins the exact wording the destination guard hands
// back to the model, ahead of moving it into catalog/system/guards — see
// catalog/system/README.md. A mismatch here means the migration changed what
// an LLM reads, not just where the sentence lives.
func TestGuardProseUnchanged(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"chrome_not_found", chromeNotFoundMsg, "chromium not found — this tool requires the tools image (TENANT_IMAGE)"},
		{"nav_failed", navFailedMsg, "could not open that URL"},
		{"page_unavailable", pageUnavailableMsg, "could not use the current page"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}
