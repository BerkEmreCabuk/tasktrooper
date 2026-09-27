package mobile

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestGuardProseUnchanged pins the exact wording mobile_* tools hand back to
// the model, ahead of moving it into catalog/system — see
// catalog/system/README.md. A mismatch here means the migration changed what
// an LLM reads, not just where the sentence lives.
func TestGuardProseUnchanged(t *testing.T) {
	if got, want := notConfiguredMsg, "no mobile device is configured — set tools.mobile.hub_url and tools.mobile.device_udid"; got != want {
		t.Errorf("notConfiguredMsg = %q, want %q", got, want)
	}
	if got, want := deviceBlock("x").Content, "Every shared test device is in use by another run. This task is parked and will resume automatically when one frees up — stop working on it now."; got != want {
		t.Errorf("deviceBlock content = %q, want %q", got, want)
	}
	if _, err := findElement(context.Background(), nil, selectorArgs{}); err == nil || err.Error() != "give one of text, resource_id, content_desc or xpath" {
		t.Errorf("findElement error = %v, want %q", err, "give one of text, resource_id, content_desc or xpath")
	}
	if res := (&tapTool{session: nil}).Execute(context.Background(), `{}`); res.Content != "give text, resource_id, content_desc, xpath, or both x and y" {
		t.Errorf("tapTool empty-args = %q", res.Content)
	}
	if res := (&waitForTool{session: nil}).Execute(context.Background(), `{}`); res.Content != "give one of text, resource_id, content_desc or xpath" {
		t.Errorf("waitForTool empty-args = %q", res.Content)
	}
}

func TestLaunchNoPackageMessageUnchanged(t *testing.T) {
	h := newHub()
	s := newTestSession(t, h, "")
	tool := newLaunchTool(s, stubResolver{})

	res := execTool(t, tool, `{"repository_id":"`+uuid.New().String()+`","env":"stage"}`)

	want := "the stage deploy target has no android app package recorded — a human must set app_package (and app_url) on it before the app can be tested on a device"
	if res.Content != want {
		t.Errorf("launch no-package message = %q, want %q", res.Content, want)
	}
}

func TestLaunchHoldDeviceNoteUnchanged(t *testing.T) {
	h := newHub()
	s := newTestSession(t, h, "")
	tool := newLaunchTool(s, stubResolver{pkg: "ai.tasktrooper.app.stage"})

	res := execTool(t, tool, `{"repository_id":"`+uuid.New().String()+`","env":"stage"}`)

	want := "\nYou hold the device until you call mobile_release_device — release it as soon as you are done."
	if !strings.HasSuffix(res.Content, want) {
		t.Errorf("launch success message = %q, want suffix %q", res.Content, want)
	}
}

func TestScreenshotTooLargeMessageUnchanged(t *testing.T) {
	h := newHub()
	big := strings.Repeat("A", maxBase64Bytes+10)
	h.handler["GET /screenshot"] = func() (int, string) {
		return http.StatusOK, `{"value":"` + big + `"}`
	}
	s := newTestSession(t, h, "")

	res := execTool(t, newScreenshotTool(s), `{}`)

	want := "screenshot is too large for the model context (" + strconv.Itoa(len(big)) + " base64 bytes); use mobile_read_ui to inspect the screen instead"
	if res.Content != want {
		t.Errorf("screenshot too-large message = %q, want %q", res.Content, want)
	}
}

func TestWaitForTimeoutMessageUnchanged(t *testing.T) {
	h := newHub()
	h.handler["POST /element"] = func() (int, string) {
		return http.StatusNotFound, `{"value":{"error":"no such element","message":"no such element: Unable to locate element"}}`
	}
	s := newTestSession(t, h, "")

	res := execTool(t, newWaitForTool(s), `{"text":"Save","timeout_seconds":1}`)

	if !strings.Contains(res.Content, "did not appear within 1s — read the screen with mobile_read_ui to see what is actually there") {
		t.Errorf("wait_for timeout message = %q", res.Content)
	}
}
