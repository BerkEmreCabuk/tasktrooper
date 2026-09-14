package board

// Where a run's toolchain comes from once the checkout is on somebody's Mac.
//
// The two mechanisms are mutually exclusive rather than layered, and that is
// the thing worth pinning: the local resolver reads a directory THIS process
// can open, and on a remote run the directory is a path in somebody's home
// folder. Left in place it would have shipped a Linux container's PATH to
// macOS; asked of the Mac it reads the repository's own pin files.

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type stubToolchains struct {
	env       map[string]string
	err       error
	available bool
	member    string
	workspace string
	calls     int
}

func (s *stubToolchains) Available() bool { return s.available }

func (s *stubToolchains) Detect(_ context.Context, memberUID, workspace string) (map[string]string, error) {
	s.calls++
	s.member, s.workspace = memberUID, workspace
	return s.env, s.err
}

func toolchainJob(member string) RunJob {
	return RunJob{Task: domain.BoardTask{ID: uuid.New(), AssigneeUserID: member}}
}

// The Mac is asked about the ASSIGNEE'S machine and the workspace that was
// just prepared on it, and its answer is handed back untouched.
func TestRemoteToolchainAsksTheAssigneesMac(t *testing.T) {
	stub := &stubToolchains{available: true, env: map[string]string{"NODE_VERSION": "20.11.0"}}
	r := &Runner{toolchains: stub}
	assert.True(t, r.remoteToolchains())

	env := r.detectRemoteToolchain(context.Background(), toolchainJob("ayse"), "repos/acme-api")

	assert.Equal(t, map[string]string{"NODE_VERSION": "20.11.0"}, env)
	assert.Equal(t, "ayse", stub.member)
	assert.Equal(t, "repos/acme-api", stub.workspace)
}

// A detection that could not be made does NOT fail the run. The session starts
// on the Mac's own defaults, which is exactly where every remote run was before
// this call existed — so the worst case of the tunnel hiccuping is the status
// quo, while failing would turn a repository that pins nothing into a task that
// never starts.
func TestRemoteToolchainFailureDoesNotStopTheRun(t *testing.T) {
	for _, err := range []error{
		errors.New("runner: toolchain.detect failed (upstream): boom"),
		&domain.RunnerBlock{MemberUID: "ayse", Detail: "the tunnel went away"},
		context.DeadlineExceeded,
	} {
		r := &Runner{toolchains: &stubToolchains{available: true, err: err}}
		assert.Nil(t, r.detectRemoteToolchain(context.Background(), toolchainJob("ayse"), "repos/acme"))
	}
}

// Absence stays absence. An empty answer is a complete answer — the checkout
// declares nothing — and it must omit the parameter rather than be filled in
// with a "system" or "latest" default nobody wrote in the repository.
func TestRemoteToolchainReportsNothingRatherThanADefault(t *testing.T) {
	r := &Runner{toolchains: &stubToolchains{available: true, env: map[string]string{}}}
	assert.Nil(t, r.detectRemoteToolchain(context.Background(), toolchainJob("ayse"), "repos/acme"))
}

// An unwired detector leaves the LOCAL resolver in charge, which is correct
// wherever the checkout really is on this filesystem: a self-hosted install and
// the desktop bundle must keep resolving their own overlay exactly as they did.
func TestWithoutADetectorTheLocalResolverStays(t *testing.T) {
	assert.False(t, (&Runner{}).remoteToolchains())
	assert.False(t, (&Runner{toolchains: &stubToolchains{available: false}}).remoteToolchains())
}
