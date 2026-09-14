package mobile

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	launchToolName     = "mobile_launch_app"
	screenshotToolName = "mobile_screenshot"
	readUIToolName     = "mobile_read_ui"
	waitForToolName    = "mobile_wait_for"
)

// maxBase64Bytes caps what one screenshot may occupy in the model context.
// Phone screenshots are portrait and smaller than a desktop viewport, so this
// is generous in practice; a shot above it is refused rather than silently
// halved, because a device screenshot has no quality knob to fall back to the
// way the browser's JPEG retake does.
const maxBase64Bytes = 1536 * 1024

// AppResolver answers "which app is this repository's mobile build, and where
// is the artifact". It is the guard: mobile_launch_app can only ever open a
// package a human registered on a deploy target, so the tool cannot be talked
// into opening the operator's banking app on a phone that is, physically,
// somebody's actual phone.
type AppResolver interface {
	ResolveApp(ctx context.Context, repositoryID uuid.UUID, env string) (appPackage, appURL string, err error)
}

// --- launch ------------------------------------------------------------

type launchTool struct {
	session device
	targets AppResolver
}

func newLaunchTool(s device, targets AppResolver) port.ToolExecutor {
	return &launchTool{session: s, targets: targets}
}

func (t *launchTool) Name() string { return launchToolName }

func (t *launchTool) Definition() domain.ToolDefinition {
	return def(launchToolName,
		"Install (if needed) and open this repository's Android build on the shared test device, and take the device lease. "+
			"Call this before any other mobile_* tool. The package and artifact come from the repository's deploy target — you cannot open an arbitrary app.",
		map[string]interface{}{
			"repository_id": map[string]interface{}{"type": "string", "description": "Repository UUID"},
			"env": map[string]interface{}{
				"type": "string", "enum": domain.DeployEnvs(),
				"description": "Which environment's build to test (default: stage)",
			},
		}, "repository_id")
}

func (t *launchTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a struct {
		RepositoryID string `json:"repository_id"`
		Env          string `json:"env"`
	}
	if res := decodeArgs(launchToolName, arguments, &a); res != nil {
		return *res
	}
	repoID, err := uuid.Parse(strings.TrimSpace(a.RepositoryID))
	if err != nil {
		return toolError(launchToolName, "invalid repository_id")
	}
	env := strings.TrimSpace(a.Env)
	if env == "" {
		env = domain.DeployEnvStage
	}
	if t.targets == nil {
		return toolError(launchToolName, "no deploy targets are configured in this installation")
	}
	appPackage, appURL, err := t.targets.ResolveApp(ctx, repoID, env)
	if err != nil {
		return toolError(launchToolName, err.Error())
	}
	if appPackage == "" {
		return toolError(launchToolName, fmt.Sprintf(
			"the %s deploy target has no android app package recorded — a human must set app_package (and app_url) on it before the app can be tested on a device", env))
	}
	if err := t.session.launch(ctx, appPackage, appURL); err != nil {
		return runError(launchToolName, "launch_app", err)
	}
	msg := fmt.Sprintf("launched %s on the shared test device (env: %s)", appPackage, env)
	if appURL != "" {
		msg += "\ninstalled build: " + appURL
	}
	msg += "\nYou hold the device until you call " + releaseToolName + " — release it as soon as you are done."
	return toolOK(launchToolName, msg)
}

// --- screenshot --------------------------------------------------------

type screenshotTool struct{ session device }

func newScreenshotTool(s device) port.ToolExecutor { return &screenshotTool{session: s} }

func (t *screenshotTool) Name() string { return screenshotToolName }

func (t *screenshotTool) Definition() domain.ToolDefinition {
	return def(screenshotToolName,
		"Take a screenshot of the connected Android device and attach it to the result so you can see it.",
		map[string]interface{}{})
}

func (t *screenshotTool) Execute(ctx context.Context, _ string) domain.ToolResult {
	var out struct {
		Value string `json:"value"`
	}
	if err := t.session.run(ctx, "GET", "/screenshot", nil, &out); err != nil {
		return runError(screenshotToolName, "screenshot", err)
	}
	encoded := strings.TrimSpace(out.Value)
	if encoded == "" {
		return toolError(screenshotToolName, "the device returned an empty screenshot")
	}
	if len(encoded) > maxBase64Bytes {
		return toolError(screenshotToolName, fmt.Sprintf(
			"screenshot is too large for the model context (%d base64 bytes); use %s to inspect the screen instead",
			len(encoded), readUIToolName))
	}
	// Decoded only to report a byte count and to catch a hub that answered with
	// something that is not an image at all — a truncated body reaching the
	// model as a broken attachment reads as "the app renders nothing".
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return toolError(screenshotToolName, "the device returned a screenshot that is not valid base64")
	}
	// Best effort: a screenshot is still useful without the caption.
	caption := ""
	if w, h, err := screenSize(ctx, t.session); err == nil {
		caption = fmt.Sprintf(", screen %dx%d", w, h)
	}
	return domain.ToolResult{
		Name:    screenshotToolName,
		Content: fmt.Sprintf("screenshot attached: image/png, %d bytes%s", len(decoded), caption),
		Images:  []domain.ToolResultImage{{MediaType: "image/png", Data: encoded}},
	}
}

// --- read ui -----------------------------------------------------------

type readUITool struct{ session device }

func newReadUITool(s device) port.ToolExecutor { return &readUITool{session: s} }

func (t *readUITool) Name() string { return readUIToolName }

// maxSourceBytes bounds the XML hierarchy. A dense screen serialises to a few
// hundred kilobytes, most of it layout containers with no text and no id.
const maxSourceBytes = 60 * 1024

func (t *readUITool) Definition() domain.ToolDefinition {
	return def(readUIToolName,
		"Read the view hierarchy of the current screen as XML — the mobile equivalent of reading the DOM. Use it to find the resource-id or label to address with the other mobile_* tools, and to assert what is on screen.",
		map[string]interface{}{
			"filter": map[string]interface{}{
				"type":        "string",
				"description": "Keep only lines containing this substring (case-insensitive). Use it on a busy screen to find one element.",
			},
		})
}

func (t *readUITool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a struct {
		Filter string `json:"filter"`
	}
	if res := decodeArgs(readUIToolName, arguments, &a); res != nil {
		return *res
	}
	var out struct {
		Value string `json:"value"`
	}
	if err := t.session.run(ctx, "GET", "/source", nil, &out); err != nil {
		return runError(readUIToolName, "read_ui", err)
	}
	source := out.Value
	if filter := strings.TrimSpace(a.Filter); filter != "" {
		var kept []string
		needle := strings.ToLower(filter)
		for _, line := range strings.Split(source, "\n") {
			if strings.Contains(strings.ToLower(line), needle) {
				kept = append(kept, strings.TrimSpace(line))
			}
		}
		if len(kept) == 0 {
			return toolOK(readUIToolName, "no element on the current screen matches "+filter)
		}
		source = strings.Join(kept, "\n")
	}
	return toolOK(readUIToolName, truncate(source, maxSourceBytes))
}

// --- wait for ----------------------------------------------------------

type waitForTool struct{ session device }

func newWaitForTool(s device) port.ToolExecutor { return &waitForTool{session: s} }

func (t *waitForTool) Name() string { return waitForToolName }

const (
	defaultWaitSeconds = 10
	maxWaitSeconds     = 60
	waitPollInterval   = 500 * time.Millisecond
)

func (t *waitForTool) Definition() domain.ToolDefinition {
	props := selectorProperties()
	props["timeout_seconds"] = map[string]interface{}{
		"type":        "integer",
		"description": fmt.Sprintf("How long to wait (default: %d, max: %d)", defaultWaitSeconds, maxWaitSeconds),
	}
	return def(waitForToolName,
		"Wait until an element appears on the connected Android device. Use it after a tap that starts a load, instead of taking a screenshot and hoping the screen has settled.",
		props)
}

type waitForArgs struct {
	selectorArgs
	TimeoutSeconds int `json:"timeout_seconds"`
}

func (t *waitForTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a waitForArgs
	if res := decodeArgs(waitForToolName, arguments, &a); res != nil {
		return *res
	}
	if _, _, ok := a.strategy(); !ok {
		return toolError(waitForToolName, "give one of text, resource_id, content_desc or xpath")
	}
	timeout := a.TimeoutSeconds
	if timeout <= 0 {
		timeout = defaultWaitSeconds
	}
	if timeout > maxWaitSeconds {
		timeout = maxWaitSeconds
	}
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	// Polled rather than handed to Appium's implicit wait: an implicit wait is
	// session-wide state that would then silently slow down every later
	// findElement, including the ones whose job is to fail fast.
	for {
		_, err := findElement(ctx, t.session, a.selectorArgs)
		if err == nil {
			return toolOK(waitForToolName, "found "+a.describe())
		}
		// A taken device or a dead session will not resolve by waiting.
		if res := fatalWaitResult(waitForToolName, err); res != nil {
			return *res
		}
		if time.Now().After(deadline) {
			return toolError(waitForToolName, fmt.Sprintf(
				"%s did not appear within %ds — read the screen with %s to see what is actually there",
				a.describe(), timeout, readUIToolName))
		}
		select {
		case <-ctx.Done():
			return toolError(waitForToolName, "wait_for: "+ctx.Err().Error())
		case <-time.After(waitPollInterval):
		}
	}
}

// fatalWaitResult returns non-nil for the errors that mean "stop waiting":
// everything structural, as opposed to the element simply not being there yet.
func fatalWaitResult(name string, err error) *domain.ToolResult {
	res := runError(name, "wait_for", err)
	if res.ResourceBlock != nil {
		return &res
	}
	if strings.Contains(err.Error(), "appium unreachable") || isStaleSession(err) {
		return &res
	}
	return nil
}
