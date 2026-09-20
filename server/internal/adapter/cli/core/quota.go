package core

import (
	"fmt"
	"sync"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Stroke is one blocked-state a CLI can signal: the marker its output emits
// and the code surfaced to the caller. Result prefers the code so a specific
// provider rejection reads more usefully than the generic mark.
type Stroke struct{ Mark, Code string }

func (s Stroke) Result() string {
	if s.Code != "" {
		return s.Code
	}
	return s.Mark
}

// WithSuffix extends a mark with its flavor's suffix (e.g. "-CURSOR").
func WithSuffix(s Stroke, suffix string) Stroke {
	s.Mark += suffix
	return s
}

// QuotaExceededAnywhere is the shared marker for spending caps anywhere in
// the output.
func QuotaExceededAnywhere() Stroke {
	return Stroke{Mark: "quota exceeded", Code: "quota exceeded"}
}

// BlockedDetail explains why a run ended blocked, using the flavor's detail
// template (QuotaDetailFmt) with the matched code.
func BlockedDetail(result string, detailFmt string) string {
	if result == "" {
		return ""
	}
	return fmt.Sprintf(detailFmt, result)
}

// MarkedLine renders the one-line "tasktrooper was blocked" hand-off for the
// successful-ish reply, using the flavor's prefix template.
func ObservedBlockedLine(prefixFmt, result string) string {
	return fmt.Sprintf(prefixFmt, result)
}

// Gate is the per-executor parking state one spent-session run leaves behind
// so the NEXT run knows to park immediately instead of spawning. It only ever
// extends: a newer, earlier estimate must not overwrite an already-later one.
type Gate struct {
	mu     sync.Mutex
	until  time.Time
	detail string
}

func (g *Gate) Arm(block *domain.QuotaBlock) {
	if block == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if block.ResumeAt.After(g.until) {
		g.until = block.ResumeAt
		g.detail = block.Detail
	}
}

func (g *Gate) Clear() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.until = time.Time{}
	g.detail = ""
}

func (g *Gate) State() (until time.Time, detail string, armed bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.until, g.detail, !g.until.IsZero()
}