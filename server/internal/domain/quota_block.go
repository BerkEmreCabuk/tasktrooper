package domain

import (
	"errors"
	"fmt"
	"time"
)

// DefaultQuotaParkWindow is how long a run waits when the CLI said the usage
// limit was reached but not when it reopens.
//
// Half an hour rather than the five-hour window a Claude subscription actually
// rolls on: the reset epoch is normally present, so this only covers the case
// where it is missing, and there guessing SHORT is the cheaper mistake. A sweep
// that wakes the task early finds the limit still in force and parks it again
// for another window — one wasted CLI start. A sweep that wakes it hours late
// leaves a task idle for hours after the quota came back, and nothing else in
// the system would notice.
const DefaultQuotaParkWindow = 30 * time.Minute

// QuotaBlock is the CLI saying "this account has nothing left to spend until
// T". It is an ERROR type, unlike ResourceBlock, because it comes back from an
// executor rather than from a tool: the run did not finish and has no response
// to hand over, so there is no AgentResponse to carry a block on.
//
// What it shares with ResourceBlock is the treatment. Neither is a failure of
// the work: there is nothing to fix, nothing to retry now, and a run that fails
// on it would spend one of the task's three consecutive-failure lives on a
// billing window. So the board runner parks the task instead of failing it, and
// a sweeper — not a human, not a retry — releases it once ResumeAt has passed
// (see application/board/quota_sweeper.go).
//
// CLISessionID is what makes the resume a continuation rather than a restart:
// the parked session already read the repository, wrote some of the change and
// knows what it was in the middle of. Handing that id back to `claude -p
// --resume <id>` is the difference between finishing the task and paying for
// the exploration twice.
type QuotaBlock struct {
	// ResumeAt is when the limit is expected to lift. Always set: a caller that
	// could not parse a reset time uses DefaultQuotaParkWindow rather than a
	// zero time, because a zero time would read as "resume immediately" to the
	// sweeper and spin.
	ResumeAt time.Time
	// CLISessionID is the parked Claude Code session. Empty when the limit was
	// hit before the session announced itself (an init event that never
	// arrived), which is survivable: the resumed run starts a fresh session
	// with the same task context instead.
	CLISessionID string
	// Detail is the CLI's own wording, kept for the board card so a human can
	// see which limit was hit rather than a paraphrase of it.
	Detail string
}

func (q *QuotaBlock) Error() string {
	if q == nil {
		return "claude code usage limit reached"
	}
	return fmt.Sprintf("claude code usage limit reached, resuming at %s", q.ResumeAt.UTC().Format(time.RFC3339))
}

// QuotaBlockOf reports the usage-limit block behind err, anywhere in its wrap
// chain. It mirrors RateLimitOf, and for the same reason: the condition has to
// be recognisable at the transport, several wraps away from where it was
// raised.
func QuotaBlockOf(err error) (*QuotaBlock, bool) {
	var q *QuotaBlock
	if !errors.As(err, &q) || q == nil {
		return nil, false
	}
	return q, true
}

// UserMessage is the sentence a person sees in a CHAT when the subscription is
// spent.
//
// A chat has no card to park and no sweeper to wake it: the board's answer to
// this error — move the task to blocked, resume at ResumeAt — has no equivalent
// in a conversation, where the only actor is the human who just typed. So the
// block has to become an instruction to that human, and the one fact that makes
// it actionable is WHEN, which is why the time is always named.
//
// Local time, not UTC: the reader is sitting at this host's clock, and "resumes
// at 14:20Z" is a sentence nobody can act on without doing arithmetic.
//
// Error() is left alone — it is the log line and the board card's detail, where
// UTC and RFC3339 are the right choices.
func (q *QuotaBlock) UserMessage(lang string) string {
	if q == nil {
		switch lang {
		case "tr":
			return "Claude Code kullanım limiti doldu; limit yenilendikten sonra tekrar deneyin."
		default:
			return "The Claude Code usage limit is spent; try again once it renews."
		}
	}
	when := q.resumeLabel()
	switch lang {
	case "tr":
		return fmt.Sprintf("Claude Code kullanım limiti doldu; %s civarında yenilenecek, sonra tekrar deneyin. "+
			"Beklemek istemiyorsanız bu ajanı API üzerinden çalışan bir sağlayıcıya taşıyabilirsiniz.", when)
	default:
		return fmt.Sprintf("The Claude Code usage limit is spent; it renews around %s — try again after that. "+
			"If you would rather not wait, move this agent to an API-backed provider.", when)
	}
}

// QuotaNotice is a QuotaBlock that has already been turned into the sentence a
// particular reader gets.
//
// It exists because the two things that need it sit on opposite sides of the
// process. The LANGUAGE is known in the session service, which has just loaded
// the tenant's settings; the TRANSPORT is the SSE writer, which runs after the
// HTTP handler has returned and has no business making a database read on an
// error path to find out what language to apologise in. Localising once, where
// the answer is already in hand, and carrying the finished sentence on the error
// settles that without either layer reaching into the other.
//
// The block itself is still underneath and still findable with QuotaBlockOf, so
// nothing that wants the structured facts (ResumeAt, the CLI session) loses
// them.
type QuotaNotice struct {
	block   *QuotaBlock
	message string
}

// NewQuotaNotice localises block for lang. A nil block still yields a usable
// notice — the generic sentence — because the caller is on an error path and
// must not have to branch.
func NewQuotaNotice(block *QuotaBlock, lang string) *QuotaNotice {
	return &QuotaNotice{block: block, message: block.UserMessage(lang)}
}

// Error is the localised sentence itself, not a description of it. That is
// deliberate: every generic error path in the transport prints err.Error(), so
// the default rendering of this error is already the right one even where
// nothing has been taught to recognise the type.
func (n *QuotaNotice) Error() string { return n.message }

// Unwrap exposes the block so QuotaBlockOf and errors.As keep working through
// the notice.
func (n *QuotaNotice) Unwrap() error { return n.block }

// resumeLabel renders ResumeAt for a human on this host.
//
// The date is included only when the reset is not today: "18:40" is unambiguous
// for the common case (a window that reopens in a few hours) and a bare "18:40"
// for tomorrow morning would be a lie by omission.
func (q *QuotaBlock) resumeLabel() string {
	local := q.ResumeAt.Local()
	now := time.Now().Local()
	if local.YearDay() == now.YearDay() && local.Year() == now.Year() {
		return local.Format("15:04")
	}
	return local.Format("2 Jan 15:04")
}
