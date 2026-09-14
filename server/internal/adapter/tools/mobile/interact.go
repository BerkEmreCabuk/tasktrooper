package mobile

import (
	"context"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	tapToolName         = "mobile_tap"
	typeTextToolName    = "mobile_type_text"
	swipeToolName       = "mobile_swipe"
	pressButtonToolName = "mobile_press_button"
	rotateToolName      = "mobile_rotate"
	unlockToolName      = "mobile_unlock_device"
	releaseToolName     = "mobile_release_device"
)

// --- tap ---------------------------------------------------------------

type tapTool struct{ session device }

func newTapTool(s device) port.ToolExecutor { return &tapTool{session: s} }

func (t *tapTool) Name() string { return tapToolName }

func (t *tapTool) Definition() domain.ToolDefinition {
	props := selectorProperties()
	props["x"] = map[string]interface{}{"type": "integer", "description": "Absolute x pixel; only with y, and only when no selector can address the target"}
	props["y"] = map[string]interface{}{"type": "integer", "description": "Absolute y pixel; only with x"}
	return def(tapToolName,
		"Tap an element on the connected Android device. Address it by text, resource_id or content_desc; x/y is a fallback for canvases and maps.",
		props)
}

type tapArgs struct {
	selectorArgs
	X int `json:"x"`
	Y int `json:"y"`
}

func (t *tapTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a tapArgs
	if res := decodeArgs(tapToolName, arguments, &a); res != nil {
		return *res
	}
	if _, _, ok := a.strategy(); !ok {
		if a.X <= 0 && a.Y <= 0 {
			return toolError(tapToolName, "give text, resource_id, content_desc, xpath, or both x and y")
		}
		if err := tapAt(ctx, t.session, a.X, a.Y); err != nil {
			return runError(tapToolName, "tap", err)
		}
		return toolOK(tapToolName, fmt.Sprintf("tapped at (%d, %d)", a.X, a.Y))
	}
	id, err := findElement(ctx, t.session, a.selectorArgs)
	if err != nil {
		return runError(tapToolName, "tap", err)
	}
	if err := t.session.run(ctx, "POST", "/element/"+id+"/click", map[string]interface{}{}, nil); err != nil {
		return runError(tapToolName, "tap", err)
	}
	return toolOK(tapToolName, "tapped "+a.describe())
}

// tapAt is a W3C pointer sequence: press, hold briefly, release. The brief hold
// is not decoration — a zero-duration press is dropped by some views as a
// stray touch rather than a tap.
func tapAt(ctx context.Context, s device, x, y int) error {
	return s.run(ctx, "POST", "/actions", map[string]interface{}{
		"actions": []map[string]interface{}{{
			"type":       "pointer",
			"id":         "finger1",
			"parameters": map[string]string{"pointerType": "touch"},
			"actions": []map[string]interface{}{
				{"type": "pointerMove", "duration": 0, "x": x, "y": y},
				{"type": "pointerDown", "button": 0},
				{"type": "pause", "duration": 80},
				{"type": "pointerUp", "button": 0},
			},
		}},
	}, nil)
}

// --- type text ---------------------------------------------------------

type typeTextTool struct{ session device }

func newTypeTextTool(s device) port.ToolExecutor { return &typeTextTool{session: s} }

func (t *typeTextTool) Name() string { return typeTextToolName }

func (t *typeTextTool) Definition() domain.ToolDefinition {
	props := selectorProperties()
	props["value"] = map[string]interface{}{"type": "string", "description": "Text to type into the field"}
	props["clear"] = map[string]interface{}{"type": "boolean", "description": "Clear the field first (default: true)"}
	return def(typeTextToolName,
		"Type text into a field on the connected Android device.",
		props, "value")
}

type typeTextArgs struct {
	selectorArgs
	Value string `json:"value"`
	// Pointer so an omitted clear defaults to true while an explicit false is
	// honoured — a plain bool would make "append to this field" unreachable.
	Clear *bool `json:"clear"`
}

func (t *typeTextTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a typeTextArgs
	if res := decodeArgs(typeTextToolName, arguments, &a); res != nil {
		return *res
	}
	if a.Value == "" {
		return toolError(typeTextToolName, "value is required")
	}
	id, err := findElement(ctx, t.session, a.selectorArgs)
	if err != nil {
		return runError(typeTextToolName, "type_text", err)
	}
	if a.Clear == nil || *a.Clear {
		if err := t.session.run(ctx, "POST", "/element/"+id+"/clear", map[string]interface{}{}, nil); err != nil {
			return runError(typeTextToolName, "type_text (clear)", err)
		}
	}
	if err := t.session.run(ctx, "POST", "/element/"+id+"/value", map[string]interface{}{
		"text": a.Value,
	}, nil); err != nil {
		return runError(typeTextToolName, "type_text", err)
	}
	return toolOK(typeTextToolName, fmt.Sprintf("typed %d characters into %s", len(a.Value), a.describe()))
}

// --- swipe -------------------------------------------------------------

type swipeTool struct{ session device }

func newSwipeTool(s device) port.ToolExecutor { return &swipeTool{session: s} }

func (t *swipeTool) Name() string { return swipeToolName }

func (t *swipeTool) Definition() domain.ToolDefinition {
	return def(swipeToolName,
		"Swipe on the connected Android device — scroll a list, dismiss a sheet, page a carousel.",
		map[string]interface{}{
			"direction": map[string]interface{}{
				"type": "string", "enum": []string{"up", "down", "left", "right"},
				"description": "Direction the finger moves. \"up\" scrolls the content down the page.",
			},
			"distance": map[string]interface{}{
				"type":        "number",
				"description": "Fraction of the screen to travel, 0.1–0.9 (default: 0.6)",
			},
		}, "direction")
}

type swipeArgs struct {
	Direction string  `json:"direction"`
	Distance  float64 `json:"distance"`
}

func (t *swipeTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a swipeArgs
	if res := decodeArgs(swipeToolName, arguments, &a); res != nil {
		return *res
	}
	if a.Distance <= 0 {
		a.Distance = 0.6
	}
	if a.Distance > 0.9 {
		a.Distance = 0.9
	}
	w, h, err := screenSize(ctx, t.session)
	if err != nil {
		return runError(swipeToolName, "swipe", err)
	}
	cx, cy := w/2, h/2
	dx, dy := 0, 0
	switch strings.ToLower(a.Direction) {
	case "up":
		dy = -int(float64(h) * a.Distance / 2)
	case "down":
		dy = int(float64(h) * a.Distance / 2)
	case "left":
		dx = -int(float64(w) * a.Distance / 2)
	case "right":
		dx = int(float64(w) * a.Distance / 2)
	default:
		return toolError(swipeToolName, "direction must be up, down, left or right")
	}
	err = t.session.run(ctx, "POST", "/actions", map[string]interface{}{
		"actions": []map[string]interface{}{{
			"type":       "pointer",
			"id":         "finger1",
			"parameters": map[string]string{"pointerType": "touch"},
			"actions": []map[string]interface{}{
				{"type": "pointerMove", "duration": 0, "x": cx - dx, "y": cy - dy},
				{"type": "pointerDown", "button": 0},
				{"type": "pause", "duration": 100},
				// A swipe that teleports is read as a fling or ignored; the
				// travel time is what makes it a scroll.
				{"type": "pointerMove", "duration": 600, "x": cx + dx, "y": cy + dy},
				{"type": "pointerUp", "button": 0},
			},
		}},
	}, nil)
	if err != nil {
		return runError(swipeToolName, "swipe", err)
	}
	return toolOK(swipeToolName, fmt.Sprintf("swiped %s by %.0f%% of the screen", a.Direction, a.Distance*100))
}

func screenSize(ctx context.Context, s device) (int, int, error) {
	var out struct {
		Value struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"value"`
	}
	if err := s.run(ctx, "GET", "/window/rect", nil, &out); err != nil {
		return 0, 0, err
	}
	if out.Value.Width == 0 || out.Value.Height == 0 {
		return 0, 0, fmt.Errorf("device reported a %dx%d screen", out.Value.Width, out.Value.Height)
	}
	return out.Value.Width, out.Value.Height, nil
}

// --- press button ------------------------------------------------------

type pressButtonTool struct{ session device }

func newPressButtonTool(s device) port.ToolExecutor { return &pressButtonTool{session: s} }

func (t *pressButtonTool) Name() string { return pressButtonToolName }

// androidKeycodes are the hardware/system keys worth exposing by name. The
// numeric keycode is deliberately not an argument: a free integer is an
// arbitrary key-injection surface on a shared device, and every key a QA run
// legitimately needs is here.
var androidKeycodes = map[string]int{
	"back": 4, "home": 3, "recent_apps": 187, "enter": 66, "delete": 67,
	"volume_up": 24, "volume_down": 25, "power": 26, "search": 84, "menu": 82,
}

func (t *pressButtonTool) Definition() domain.ToolDefinition {
	names := make([]string, 0, len(androidKeycodes))
	for k := range androidKeycodes {
		names = append(names, k)
	}
	return def(pressButtonToolName,
		"Press a hardware or system button on the connected Android device.",
		map[string]interface{}{
			"button": map[string]interface{}{"type": "string", "enum": names, "description": "Button to press"},
		}, "button")
}

func (t *pressButtonTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a struct {
		Button string `json:"button"`
	}
	if res := decodeArgs(pressButtonToolName, arguments, &a); res != nil {
		return *res
	}
	code, ok := androidKeycodes[strings.ToLower(strings.TrimSpace(a.Button))]
	if !ok {
		return toolError(pressButtonToolName, "unknown button: "+a.Button)
	}
	if err := t.session.run(ctx, "POST", "/appium/device/press_keycode", map[string]interface{}{
		"keycode": code,
	}, nil); err != nil {
		return runError(pressButtonToolName, "press_button", err)
	}
	return toolOK(pressButtonToolName, "pressed "+a.Button)
}

// --- rotate ------------------------------------------------------------

type rotateTool struct{ session device }

func newRotateTool(s device) port.ToolExecutor { return &rotateTool{session: s} }

func (t *rotateTool) Name() string { return rotateToolName }

func (t *rotateTool) Definition() domain.ToolDefinition {
	return def(rotateToolName,
		"Rotate the connected Android device, to check a layout in the other orientation.",
		map[string]interface{}{
			"orientation": map[string]interface{}{
				"type": "string", "enum": []string{"portrait", "landscape"},
				"description": "Target orientation",
			},
		}, "orientation")
}

func (t *rotateTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a struct {
		Orientation string `json:"orientation"`
	}
	if res := decodeArgs(rotateToolName, arguments, &a); res != nil {
		return *res
	}
	want := strings.ToUpper(strings.TrimSpace(a.Orientation))
	if want != "PORTRAIT" && want != "LANDSCAPE" {
		return toolError(rotateToolName, "orientation must be portrait or landscape")
	}
	if err := t.session.run(ctx, "POST", "/orientation", map[string]interface{}{
		"orientation": want,
	}, nil); err != nil {
		return runError(rotateToolName, "rotate", err)
	}
	return toolOK(rotateToolName, "rotated to "+strings.ToLower(want))
}

// --- unlock ------------------------------------------------------------

type unlockTool struct{ session device }

func newUnlockTool(s device) port.ToolExecutor { return &unlockTool{session: s} }

func (t *unlockTool) Name() string { return unlockToolName }

func (t *unlockTool) Definition() domain.ToolDefinition {
	return def(unlockToolName,
		"Wake and unlock the connected Android device. The lease already unlocks it, so this is only needed when the screen locked itself mid-run (a long read, a slow build) and the app is hidden behind the lock screen.",
		map[string]interface{}{})
}

// Execute never takes the PIN as an argument. The credential is the operator's,
// configured once; an agent that could pass one could also be talked into
// trying others, and the attempts would land on a real phone.
func (t *unlockTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var locked struct {
		Value bool `json:"value"`
	}
	if err := t.session.run(ctx, "POST", "/appium/device/is_locked", map[string]interface{}{}, &locked); err != nil {
		return runError(unlockToolName, "unlock", err)
	}
	if !locked.Value {
		return toolOK(unlockToolName, "device is already unlocked")
	}
	if err := t.session.run(ctx, "POST", "/appium/device/unlock", map[string]interface{}{}, nil); err != nil {
		return runError(unlockToolName, "unlock", err)
	}
	return toolOK(unlockToolName, "device unlocked")
}

// --- release -----------------------------------------------------------

type releaseTool struct{ session device }

func newReleaseTool(s device) port.ToolExecutor { return &releaseTool{session: s} }

func (t *releaseTool) Name() string { return releaseToolName }

func (t *releaseTool) Definition() domain.ToolDefinition {
	return def(releaseToolName,
		"Hand the shared test device back when you are done with it, so a task waiting on it can start immediately instead of waiting out the idle timeout. Call this as the last mobile step of your run.",
		map[string]interface{}{})
}

func (t *releaseTool) Execute(ctx context.Context, _ string) domain.ToolResult {
	t.session.Release(ctx)
	return toolOK(releaseToolName, "released the test device")
}
