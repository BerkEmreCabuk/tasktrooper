package mobile

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// hub is a stand-in Appium server. Handlers are keyed by "METHOD /path" with
// the session id already stripped, so a test says what it is answering rather
// than reconstructing session-scoped URLs.
type hub struct {
	mu       sync.Mutex
	requests []string
	bodies   map[string]json.RawMessage
	// busy makes session creation fail the way a taken device does.
	busy    bool
	handler map[string]func() (int, string)
}

func newHub() *hub {
	return &hub{bodies: map[string]json.RawMessage{}, handler: map[string]func() (int, string){}}
}

func (h *hub) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			_, _ = r.Body.Read(body)
		}
		path := r.URL.Path
		if strings.HasPrefix(path, "/session/") {
			rest := strings.SplitN(strings.TrimPrefix(path, "/session/"), "/", 2)
			if len(rest) == 2 {
				path = "/" + rest[1]
			} else {
				path = ""
			}
		}
		key := r.Method + " " + path
		h.mu.Lock()
		h.requests = append(h.requests, key)
		if len(body) > 0 {
			h.bodies[key] = json.RawMessage(body)
		}
		busy, fn := h.busy, h.handler[key]
		h.mu.Unlock()

		if r.Method == http.MethodPost && r.URL.Path == "/session" {
			if busy {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"value":{"error":"session not created","message":"Could not start a new session. Device is busy: already in use by another session"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"value":{"sessionId":"abc123","capabilities":{}}}`))
			return
		}
		if fn != nil {
			status, payload := fn()
			w.WriteHeader(status)
			_, _ = w.Write([]byte(payload))
			return
		}
		_, _ = w.Write([]byte(`{"value":null}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (h *hub) saw(key string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.requests {
		if r == key {
			return true
		}
	}
	return false
}

func (h *hub) body(key string) map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out map[string]any
	_ = json.Unmarshal(h.bodies[key], &out)
	return out
}

func newTestSession(t *testing.T, h *hub, pin string) *Session {
	t.Helper()
	srv := h.serve(t)
	s := NewSession(Config{HubURL: srv.URL, DeviceUDID: "TESTDEVICE", DevicePIN: pin})
	t.Cleanup(s.Close)
	return s
}

type stubResolver struct {
	pkg string
	url string
	err error
}

func (r stubResolver) ResolveApp(context.Context, uuid.UUID, string) (string, string, error) {
	return r.pkg, r.url, r.err
}

func execTool(t *testing.T, tool interface {
	Execute(context.Context, string) domain.ToolResult
}, args string) domain.ToolResult {
	t.Helper()
	return tool.Execute(context.Background(), args)
}

// A taken device must never reach the agent as a tool error. This is the whole
// contract of the queueing design: an error would have the model retry or give
// up, and both waste the run — the correct outcome is a parked task.
func TestBusyDeviceReturnsResourceBlockNotError(t *testing.T) {
	h := newHub()
	h.busy = true
	s := newTestSession(t, h, "")

	res := execTool(t, newTapTool(s), `{"text":"Sign in"}`)

	require.NotNil(t, res.ResourceBlock, "a busy device must be a resource block")
	assert.Equal(t, domain.ResourceMobileDevice, res.ResourceBlock.Resource)
	assert.False(t, res.IsError, "a busy device is not a tool error")
}

// The lock-screen PIN travels as a session capability, so the device is
// unlocked before the first tool call rather than after a screenshot of the
// lock screen has already been reported as a broken app.
func TestSessionUnlocksWithConfiguredPIN(t *testing.T) {
	h := newHub()
	s := newTestSession(t, h, "4321")

	execTool(t, newTapTool(s), `{"x":10,"y":20}`)

	caps := h.body("POST /session")["capabilities"].(map[string]any)["alwaysMatch"].(map[string]any)
	assert.Equal(t, "pin", caps["appium:unlockType"])
	assert.Equal(t, "4321", caps["appium:unlockKey"])
	assert.Equal(t, "TESTDEVICE", caps["appium:udid"])
}

// No PIN configured must not send an unlock capability at all — sending an
// empty key makes Appium attempt an unlock with a blank PIN on every session.
func TestSessionWithoutPINSendsNoUnlockCapability(t *testing.T) {
	h := newHub()
	s := newTestSession(t, h, "")

	execTool(t, newTapTool(s), `{"x":1,"y":1}`)

	caps := h.body("POST /session")["capabilities"].(map[string]any)["alwaysMatch"].(map[string]any)
	_, hasType := caps["appium:unlockType"]
	assert.False(t, hasType)
}

// The guard: launch resolves the package from the deploy target, and a target
// with none recorded refuses rather than opening whatever was asked for.
func TestLaunchOnlyOpensTheRegisteredPackage(t *testing.T) {
	h := newHub()
	s := newTestSession(t, h, "")
	tool := newLaunchTool(s, stubResolver{pkg: "ai.tasktrooper.app.stage"})

	res := execTool(t, tool, `{"repository_id":"`+uuid.New().String()+`","env":"stage"}`)

	require.False(t, res.IsError, res.Content)
	assert.Equal(t, "ai.tasktrooper.app.stage",
		h.body("POST /appium/device/activate_app")["appId"])
}

func TestLaunchRefusesWhenNoPackageIsRegistered(t *testing.T) {
	h := newHub()
	s := newTestSession(t, h, "")
	tool := newLaunchTool(s, stubResolver{})

	res := execTool(t, tool, `{"repository_id":"`+uuid.New().String()+`","env":"stage"}`)

	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "app package")
	assert.False(t, h.saw("POST /session"), "a refused launch must not take the device lease")
}

// A resource-id given without the package prefix is the common case and must
// still match; the raw Appium "id" strategy would find nothing and the agent
// would read that as a missing button.
func TestSelectorMapsBareResourceIDToAPrefixMatch(t *testing.T) {
	using, value, ok := selectorArgs{ResourceID: "login_button"}.strategy()
	require.True(t, ok)
	assert.Equal(t, "-android uiautomator", using)
	assert.Contains(t, value, `resourceIdMatches(".*/login_button")`)

	using, value, ok = selectorArgs{ResourceID: "com.x:id/login_button"}.strategy()
	require.True(t, ok)
	assert.Equal(t, "id", using)
	assert.Equal(t, "com.x:id/login_button", value)
}

// A label containing a quote must not break out of the UiSelector expression.
func TestSelectorEscapesQuotesInText(t *testing.T) {
	_, value, ok := selectorArgs{Text: `say "hi"`}.strategy()
	require.True(t, ok)
	assert.Contains(t, value, `\"hi\"`)
	assert.NotContains(t, value, `text("say "hi")`)
}

func TestScreenshotIsAttachedAsAnImage(t *testing.T) {
	h := newHub()
	png := base64.StdEncoding.EncodeToString([]byte("not-really-a-png-but-decodes"))
	h.handler["GET /screenshot"] = func() (int, string) {
		return http.StatusOK, `{"value":"` + png + `"}`
	}
	s := newTestSession(t, h, "")

	res := execTool(t, newScreenshotTool(s), `{}`)

	require.Len(t, res.Images, 1)
	assert.Equal(t, "image/png", res.Images[0].MediaType)
	assert.Equal(t, png, res.Images[0].Data)
}

// press_button exposes named keys only. An arbitrary keycode on a shared
// physical phone is a key-injection surface with no QA justification.
func TestPressButtonRejectsUnknownNames(t *testing.T) {
	h := newHub()
	s := newTestSession(t, h, "")

	res := execTool(t, newPressButtonTool(s), `{"button":"26"}`)

	assert.True(t, res.IsError)
	assert.False(t, h.saw("POST /appium/device/press_keycode"))
}

// Release must actually hand the phone back — the next run's lease depends on
// the DELETE reaching the hub, not just on local state being cleared.
func TestReleaseDeletesTheSession(t *testing.T) {
	h := newHub()
	s := newTestSession(t, h, "")
	execTool(t, newTapTool(s), `{"x":1,"y":1}`)

	res := execTool(t, newReleaseTool(s), `{}`)

	assert.False(t, res.IsError)
	assert.True(t, h.saw("DELETE "), "the session must be deleted on the hub")
}

// Probe must not take the device: a sweep that acquired the lease would hand
// the resumed run a phone that is already claimed — by the sweeper.
func TestProbeDoesNotCreateASession(t *testing.T) {
	h := newHub()
	h.handler["GET /sessions"] = func() (int, string) { return http.StatusOK, `{"value":[]}` }
	s := newTestSession(t, h, "")

	assert.True(t, s.Probe(context.Background()))
	assert.False(t, h.saw("POST /session"))
}

func TestProbeReportsBusyWhenTheHubHasASession(t *testing.T) {
	h := newHub()
	h.handler["GET /sessions"] = func() (int, string) { return http.StatusOK, `{"value":[{"id":"abc"}]}` }
	s := newTestSession(t, h, "")

	assert.False(t, s.Probe(context.Background()))
}

// An unreachable hub counts as busy: resuming a task onto a phone that is not
// there only fails the run.
func TestProbeTreatsAnUnreachableHubAsBusy(t *testing.T) {
	s := NewSession(Config{HubURL: "http://127.0.0.1:1", DeviceUDID: "X"})
	assert.False(t, s.Probe(context.Background()))
}

func TestNoToolsWithoutAConfiguredDevice(t *testing.T) {
	assert.Nil(t, NewExecutors(NewSession(Config{}), stubResolver{}))
	assert.Nil(t, NewExecutors(nil, stubResolver{}))
	assert.NotNil(t, NewExecutors(NewSession(Config{HubURL: "http://x", DeviceUDID: "y"}), stubResolver{}))
}
