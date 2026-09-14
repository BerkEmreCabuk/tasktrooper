package domain

import (
	"errors"
	"fmt"
)

// RunnerBlock is "this work needs the assignee's Mac and their Mac is not
// there".
//
// It is an ERROR type for the same reason QuotaBlock is: it comes back from an
// executor or from workspace preparation rather than from a tool, so the run
// did not finish and has no AgentResponse to carry a ResourceBlock on.
//
// What it shares with QuotaBlock is the treatment, and that is the whole point.
// Nothing failed. There is nothing to fix and nothing to retry now: the code
// lives on a laptop, and the laptop is asleep, or the tunnel dropped, or the
// person has not opened the desktop app yet. A run that FAILED on it would
// spend one of the task's three consecutive-failure lives on somebody's lid
// being shut, and would say "the run failed" about a task nobody has started.
//
// What differs from the quota is who releases it, and it is the one park with
// no clock and no probe of its own: a subscription reopens at a time the CLI
// printed, a phone can be asked whether it is free, but the only thing that
// knows whether a Mac is back is the control plane — so the sweeper asks it,
// by making the cheapest call the runner has (see
// application/board/runner_sweeper.go).
type RunnerBlock struct {
	// MemberUID is whose Mac was missing. Empty when the block came back from
	// the Mac itself (NotReady), where the member is not in doubt.
	MemberUID string
	// Detail is the control plane's or the Mac's own wording, kept for the
	// board card so a human sees the actual state rather than a paraphrase.
	Detail string
	// NotReady distinguishes "a Mac is attached but cannot serve this yet" —
	// the desktop app is still starting, the environment preflight has not
	// been pushed — from "there is no Mac". Both park, because both resolve by
	// themselves in seconds to minutes; they are told apart so the card can
	// say which, and so a user who HAS opened their laptop is not told to open
	// it.
	NotReady bool
}

func (b *RunnerBlock) Error() string {
	if b == nil {
		return "no Mac is attached for this member"
	}
	if b.NotReady {
		return "the member's Mac is attached but not ready yet: " + b.Detail
	}
	return "no Mac is attached for this member: " + b.Detail
}

// RunnerBlockOf reports the missing-Mac block behind err, anywhere in its wrap
// chain. It mirrors QuotaBlockOf, and for the same reason: the condition is
// raised at the transport and recognised by the board runner several wraps
// away.
func RunnerBlockOf(err error) (*RunnerBlock, bool) {
	var b *RunnerBlock
	if !errors.As(err, &b) || b == nil {
		return nil, false
	}
	return b, true
}

// BoardDetail is the sentence that goes on the blocked card.
//
// Plain, and it names the one action that resolves it. This is the text a
// person reads when a task they assigned has not moved, and "runner_not_
// attached" is not an explanation.
func (b *RunnerBlock) BoardDetail() string {
	if b == nil {
		return "Waiting for the assignee's Mac to connect."
	}
	if b.NotReady {
		return "The assignee's Mac is connected but still starting up; the task resumes as soon as it is ready."
	}
	return "Waiting for the assignee's Mac. Open the TaskTrooper desktop app on it and the task starts by itself."
}

// UserMessage is the sentence a person sees in a CHAT, where there is no card
// to park and no sweeper to wake it — so the block has to become an
// instruction to the human who just typed. Mirrors QuotaBlock.UserMessage,
// including the Turkish, because the same reader sees both.
func (b *RunnerBlock) UserMessage(lang string) string {
	if b != nil && b.NotReady {
		switch lang {
		case "tr":
			return "Mac bağlı ama henüz hazır değil; birkaç saniye sonra tekrar deneyin."
		default:
			return "The Mac is connected but not ready yet; try again in a few seconds."
		}
	}
	switch lang {
	case "tr":
		return "Bu iş, atanan kişinin Mac'inde çalışıyor ve şu an bağlı bir Mac yok. " +
			"TaskTrooper masaüstü uygulamasını açtığınızda kaldığı yerden devam eder."
	default:
		return "This work runs on the assignee's Mac and none is connected. " +
			"Open the TaskTrooper desktop app there and it continues by itself."
	}
}

// UnassignedRunError is the OTHER half of "which Mac?", and deliberately not a
// block: a task with no person on it names no laptop, and no amount of waiting
// produces one. Parking would hide it in `blocked` behind a message about a
// Mac, when what is missing is an assignee.
func UnassignedRunError(taskKey string) error {
	return fmt.Errorf(
		"%s has no assignee, so there is no Mac to run it on — agent work happens on the assignee's own machine now. "+
			"Assign the card to a member of this workspace and it starts by itself", taskKey)
}
