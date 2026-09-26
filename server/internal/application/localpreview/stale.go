package localpreview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

// nextDevLock is where Next.js records the dev server holding a directory; a
// second `next dev` in that directory exits instead of starting.
const nextDevLock = ".next/dev/lock"

// releaseStaleDevServer stops a `next dev` an earlier run left holding the
// task's checkout — typically one an agent started to check its own work and
// never stopped. Without this the preview's own `next dev` exits the moment
// it starts.
func releaseStaleDevServer(workspacePath string) {
	data, err := os.ReadFile(filepath.Join(workspacePath, filepath.FromSlash(nextDevLock)))
	if err != nil {
		return
	}
	var lock struct {
		PID int `json:"pid"`
	}
	if json.Unmarshal(data, &lock) != nil || lock.PID <= 0 {
		return
	}
	// A lock outlives a killed server and its pid can be reused; only a live
	// Next process is the one the lock names.
	if !strings.Contains(processCommand(lock.PID), "next") {
		return
	}
	log.Info().Int("pid", lock.PID).Str("workspace", workspacePath).
		Msg("local preview: stopping a stale next dev server holding the checkout")
	stopStale(lock.PID, stopGrace)
}
