package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// DefaultQuotaParkWindow is how long a run waits when the CLI hit the usage
// limit without saying when it reopens. Guessing SHORT is the cheaper mistake:
// a sweep that wakes a task early finds the limit still in force and parks it
// again, while one that wakes it late leaves a task idle that nothing else
// would notice.
const DefaultQuotaParkWindow = 30 * time.Minute

// QuotaParkWindow backs off the fallback window across consecutive parks that
// found no reset time (doubling up to the 5h a Claude window actually runs).
func QuotaParkWindow(consecutiveParks int) time.Duration {
	if consecutiveParks < 0 {
		consecutiveParks = 0
	}
	window := DefaultQuotaParkWindow
	for i := 0; i < consecutiveParks; i++ {
		window *= 2
		if window >= 5*time.Hour {
			return 5 * time.Hour
		}
	}
	return window
}

// QuotaQueuedNoticePrefix marks a transcript line as a queued turn — parked,
// not failed, and due to answer itself once SessionQuotaSweeper resumes it.
const QuotaQueuedNoticePrefix = "**Queued:**"

// QuotaBlock is the CLI saying "this account has nothing left until T". It is
// an ERROR type, unlike ResourceBlock, because it comes back from an executor
// rather than from a tool: the run did not finish and has no response to hand
// over. What it shares with ResourceBlock is the treatment: neither is a
// failure of the work, so the board parks the task instead of failing it, and a
// sweeper — not a human, not a retry — releases it once ResumeAt passes.
// CLISessionID is what makes the resume a continuation rather than a restart.
type QuotaBlock struct {
	// ResumeAt is when the limit is expected to lift, always set: a caller
	// that could not parse a reset time uses DefaultQuotaParkWindow, because a
	// zero time would read as "resume immediately" to the sweeper and spin.
	ResumeAt time.Time
	// CLISessionID is the parked CLI session, empty when the limit was hit
	// before the session announced itself (survivable — the resumed run starts
	// fresh with the same context).
	CLISessionID string
	// Detail is the CLI's own wording, kept for the board card.
	Detail string
	// Provider names which host-executed CLI hit the limit; empty reads as a
	// generic sentence rather than a guess about which engine spent it.
	Provider LLMProviderType
}

// ProviderLabel is the human name for the block's provider, empty when none is
// set — naming "Claude Code" here would lie to someone whose block came from
// Cursor, AGY or OpenCode.
func (q *QuotaBlock) ProviderLabel() string {
	if q == nil {
		return ""
	}
	return LLMProviderLabel(q.Provider)
}

func (q *QuotaBlock) Error() string {
	if q == nil {
		return "usage limit reached"
	}
	when := q.ResumeAt.UTC().Format(time.RFC3339)
	if label := strings.ToLower(q.ProviderLabel()); label != "" {
		return fmt.Sprintf("%s usage limit reached, resuming at %s", label, when)
	}
	return fmt.Sprintf("usage limit reached, resuming at %s", when)
}

// QuotaBlockOf reports the usage-limit block behind err, anywhere in its wrap
// chain — the condition has to be recognisable at the transport, several wraps
// away from where it was raised.
func QuotaBlockOf(err error) (*QuotaBlock, bool) {
	var q *QuotaBlock
	if !errors.As(err, &q) || q == nil {
		return nil, false
	}
	return q, true
}

// UserMessage is the sentence a CHAT reader sees when the subscription is spent
// and the turn could not be queued: the only recourse left is retrying by hand.
// Local time, not UTC — "resumes at 14:20Z" is a sentence nobody can act on
// without arithmetic. Error() stays UTC/RFC3339: it is the log line.
func (q *QuotaBlock) UserMessage(lang string) string {
	if q == nil {
		switch lang {
		case "tr":
			return "Kullanım limiti doldu; limit yenilendikten sonra tekrar deneyin."
		default:
			return "The usage limit is spent; try again once it renews."
		}
	}
	when := q.resumeLabel()
	label := q.ProviderLabel()
	if label == "" {
		switch lang {
		case "tr":
			return fmt.Sprintf("Kullanım limiti doldu; %s civarında yenilenecek, sonra tekrar deneyin. "+
				"Beklemek istemiyorsanız bu ajanı API üzerinden çalışan bir sağlayıcıya taşıyabilirsiniz.", when)
		default:
			return fmt.Sprintf("The usage limit is spent; it renews around %s — try again after that. "+
				"If you would rather not wait, move this agent to an API-backed provider.", when)
		}
	}
	switch lang {
	case "tr":
		return fmt.Sprintf("%s kullanım limiti doldu; %s civarında yenilenecek, sonra tekrar deneyin. "+
			"Beklemek istemiyorsanız bu ajanı API üzerinden çalışan bir sağlayıcıya taşıyabilirsiniz.", label, when)
	default:
		return fmt.Sprintf("The %s usage limit is spent; it renews around %s — try again after that. "+
			"If you would rather not wait, move this agent to an API-backed provider.", label, when)
	}
}

// QueuedMessage is the sentence a CHAT reader sees when the turn HAS been
// queued and will rerun itself once ResumeAt passes — no action is asked,
// unlike UserMessage.
func (q *QuotaBlock) QueuedMessage(lang string) string {
	if q == nil {
		switch lang {
		case "tr":
			return "Kullanım limiti doldu; limit yenilenince mesajınız otomatik olarak gönderilecek."
		default:
			return "The usage limit is spent; your message will send automatically once it renews."
		}
	}
	when := q.resumeLabel()
	label := q.ProviderLabel()
	if label == "" {
		switch lang {
		case "tr":
			return fmt.Sprintf("Kullanım limiti doldu; %s civarında yenilenince bu mesaj otomatik olarak gönderilecek, "+
				"beklemenize gerek yok.", when)
		default:
			return fmt.Sprintf("The usage limit is spent; this message will send automatically once it renews around %s — "+
				"no need to wait or resend.", when)
		}
	}
	switch lang {
	case "tr":
		return fmt.Sprintf("%s kullanım limiti doldu; %s civarında yenilenince bu mesaj otomatik olarak gönderilecek, "+
			"beklemenize gerek yok.", label, when)
	default:
		return fmt.Sprintf("The %s usage limit is spent; this message will send automatically once it renews around %s — "+
			"no need to wait or resend.", label, when)
	}
}

// QuotaNotice is a QuotaBlock already turned into the sentence a particular
// reader gets: the LANGUAGE is known where the notice is raised (the session
// service), the TRANSPORT is the SSE writer that runs after the handler returns
// and must not make a DB read on an error path. The block stays underneath,
// findable with QuotaBlockOf.
type QuotaNotice struct {
	block   *QuotaBlock
	message string
}

// NewQuotaNotice localises block for lang; a nil block still yields the generic
// sentence because the caller is on an error path.
func NewQuotaNotice(block *QuotaBlock, lang string) *QuotaNotice {
	return &QuotaNotice{block: block, message: block.UserMessage(lang)}
}

// NewQuotaQueuedNotice is NewQuotaNotice's twin for a turn that was parked.
func NewQuotaQueuedNotice(block *QuotaBlock, lang string) *QuotaNotice {
	return &QuotaNotice{block: block, message: block.QueuedMessage(lang)}
}

// Error is the localised sentence itself: every generic error path prints
// err.Error(), so the default rendering is already right.
func (n *QuotaNotice) Error() string { return n.message }

// Unwrap exposes the block so QuotaBlockOf keeps working through the notice.
func (n *QuotaNotice) Unwrap() error { return n.block }

// resumeLabel renders ResumeAt in local time, including the date only when the
// reset is not today.
func (q *QuotaBlock) resumeLabel() string {
	local := q.ResumeAt.Local()
	now := time.Now().Local()
	if local.YearDay() == now.YearDay() && local.Year() == now.Year() {
		return local.Format("15:04")
	}
	return local.Format("2 Jan 15:04")
}
