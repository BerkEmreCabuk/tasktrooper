package desktop

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type Notifier struct {
	appName string
}

func NewNotifier(appName string) *Notifier {
	if strings.TrimSpace(appName) == "" {
		appName = "local-llm"
	}
	return &Notifier{appName: appName}
}

func (n *Notifier) Notify(title, message string) {
	if runtime.GOOS != "darwin" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		script := fmt.Sprintf(
			"display notification %s with title %s subtitle %s sound name \"default\"",
			appleScriptString(truncate(message, 200)),
			appleScriptString(n.appName),
			appleScriptString(truncate(title, 100)),
		)
		if err := exec.CommandContext(ctx, "osascript", "-e", script).Run(); err != nil {
			log.Debug().Err(err).Msg("macos notification failed")
		}
	}()
}

func (n *Notifier) Activate() {
	if runtime.GOOS != "darwin" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		script := fmt.Sprintf("tell application %s to activate", appleScriptString(n.appName))
		if err := exec.CommandContext(ctx, "osascript", "-e", script).Run(); err != nil {
			log.Debug().Err(err).Msg("macos activate failed")
		}
	}()
}

func appleScriptString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
