package domain

import (
	"strings"
	"time"
)

type GitStatus struct {
	Initialized bool   `json:"initialized"`
	HasOrigin   bool   `json:"has_origin"`
	Warning     string `json:"warning,omitempty"`
}

// GitPresenceState is what is actually at a repository's root path.
//
// It exists because "is there a git repository here" and "is the code here at
// all" are different questions, and answering both with one bool conflated
// them: a root path that does not exist on this host was reported as "not a
// git repository yet", which sends the user off to run `git init` on a folder
// that is not there. That is not a hypothetical — a tenant's runtime can be a
// cloud pod or the user's own paired Mac, and the same root_path resolves on
// one and not the other.
type GitPresenceState string

const (
	// GitPresenceRepository: the path holds a working copy. Note that this
	// covers both an ordinary clone (.git is a directory) and a worktree or
	// submodule checkout (.git is a FILE holding a `gitdir:` pointer) — git
	// works in all of them, so all of them are repositories here.
	GitPresenceRepository GitPresenceState = "repository"
	// GitPresenceNoRepository: the folder is there and readable, but has no
	// .git at all. This is the only state `git init` is the answer to.
	GitPresenceNoRepository GitPresenceState = "no_repository"
	// GitPresencePathMissing: nothing exists at the recorded path.
	GitPresencePathMissing GitPresenceState = "path_missing"
	// GitPresenceUnreadable: something is there but the answer could not be
	// obtained — permissions, an I/O error, a root path that is a file.
	GitPresenceUnreadable GitPresenceState = "unreadable"
)

// GitPresence is the answer to "what is at this path", carrying enough to say
// something true to the user rather than only enough to gate a git command.
type GitPresence struct {
	State GitPresenceState
	// Reason is the operating system's own words for GitPresenceUnreadable
	// ("permission denied", "input/output error") and is empty in every other
	// state. The path deliberately does not appear in it: every surface that
	// shows this warning already shows root_path beside it.
	Reason string
}

// IsRepository is the plain gate — "can git be run here" — that most callers
// want, and the whole of what HasGit reports.
func (p GitPresence) IsRepository() bool { return p.State == GitPresenceRepository }

// CanRestoreWorkingCopy answers "may this repository's code be fetched onto the
// machine that is running the tenant right now", and when it may not, why not.
//
// It reads the four-state GitPresence rather than re-deriving anything from the
// filesystem, because the distinctions the restore offer needs are exactly the
// ones GitPresence already draws:
//
//   - a working copy is present → there is nothing to restore, and cloning over
//     it would destroy whatever is uncommitted in it;
//   - something is at the path but is not a repository → NOT ours to remove.
//     A user who put files there did so on purpose; the honest answer is to say
//     what is in the way, never to clear it;
//   - the path could not be read → the answer is unknown, and acting on an
//     unknown is how a permissions glitch turns into a re-clone;
//   - nothing is at the path → this is the case the button exists for, and the
//     only remaining question is whether we know where the code lives.
//
// The refusal sentence lives here, next to the states it describes, for the
// same reason GitPresence.Warning does: prose and condition change together.
// English, like the rest of the repository surface.
func CanRestoreWorkingCopy(presence GitPresence, remoteURL string) (bool, string) {
	switch presence.State {
	case GitPresenceRepository:
		return false, "There is already a working copy at this path — nothing to restore"
	case GitPresenceNoRepository:
		return false, "A folder already exists at this path but is not a git repository. " +
			"Nothing was changed or deleted — move or remove that folder yourself, then try again"
	case GitPresenceUnreadable:
		if presence.Reason != "" {
			return false, "The project folder could not be read, so it is not known whether code is there: " + presence.Reason
		}
		return false, "The project folder could not be read, so it is not known whether code is there"
	}
	if strings.TrimSpace(remoteURL) == "" {
		return false, "No git remote is recorded for this project, so there is nowhere to fetch it from"
	}
	return true, ""
}

// RepositoryRestoreStatus is where a restore attempt got to. A clone is minutes
// of network, so the attempt outlives the request that started it and the UI
// reads this instead of watching a connection.
type RepositoryRestoreStatus string

const (
	RepositoryRestoreRunning   RepositoryRestoreStatus = "running"
	RepositoryRestoreCompleted RepositoryRestoreStatus = "completed"
	RepositoryRestoreFailed    RepositoryRestoreStatus = "failed"
)

// RepositoryRestore is the in-flight (or last) attempt to bring a registered
// repository's working copy onto this machine.
//
// Error carries git's own words on failure — an unreachable host, a repository
// the token cannot see, a full disk are four different problems and "restore
// failed" is none of them.
type RepositoryRestore struct {
	Status RepositoryRestoreStatus `json:"status"`
	// RootPath is where the checkout is being placed: this runtime's own
	// workspace layout, not the path the record was written with.
	RootPath   string     `json:"root_path,omitempty"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// Warning is the sentence a user sees, "" when the path is a working copy.
//
// The copy lives here, next to the states it describes, for the same reason
// QuotaBlock.UserMessage does: the sentence and the condition it explains have
// to change together, and neither the adapter that stats the disk nor the HTTP
// layer that serialises the answer is the right place to keep prose.
//
// English, like the rest of the repository surface — see the note on
// Repository.GitWarning.
func (p GitPresence) Warning() string {
	switch p.State {
	case GitPresenceRepository:
		return ""
	case GitPresencePathMissing:
		return "The project folder was not found at its recorded path — it may have been moved or deleted, " +
			"or this tenant may currently be running on a machine that does not have it"
	case GitPresenceUnreadable:
		if p.Reason != "" {
			return "The project folder could not be read: " + p.Reason
		}
		return "The project folder could not be read"
	default:
		return "This project is not a git repository yet"
	}
}
