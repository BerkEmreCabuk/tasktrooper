package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeCLI writes a stand-in binary that answers --version with version and
// every other invocation with sessionOut and sessionCode.
//
// Written per test rather than kept in testdata because what is being probed IS
// the binary's behaviour: the three cases below differ only in what the program
// says and what it exits with, and a fixture file selected by name would put
// that behind a layer of indirection for no gain.
func fakeCLI(t *testing.T, version, sessionOut string, sessionCode int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-claude.sh")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then printf '%s\\n' " + shellQuote(version) + "; exit 0; fi\n" +
		"printf '%s\\n' " + shellQuote(sessionOut) + "\n" +
		"exit " + strconv.Itoa(sessionCode) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return path
}

// shellQuote is enough for the fixed literals above and nothing more: none of
// them contains a single quote, and a test helper that pretended to be a
// general quoter would invite one that does.
func shellQuote(s string) string { return "'" + s + "'" }

// A binary that is not there is its own error, and the sentence names the fix.
func TestProbeReportsAMissingBinary(t *testing.T) {
	_, err := Probe(context.Background(), "claude-that-is-not-installed", "")
	require.ErrorIs(t, err, domain.ErrAgentCLIBinaryMissing)
	require.NotErrorIs(t, err, domain.ErrAgentCLIUnauthenticated)
	assert.Contains(t, err.Error(), "CLAUDE_CODE_BIN", "the message has to name the way out")
}

// An installed binary with no session is a DIFFERENT error, because it has a
// different fix. Collapsing the two into "connect failed" sends half the users
// who hit it to an installer they do not need.
func TestProbeReportsAnUnauthenticatedBinary(t *testing.T) {
	bin := fakeCLI(t, "1.2.3 (Claude Code)", "Invalid API key · Please run /login", 1)

	_, err := Probe(context.Background(), bin, "")
	require.ErrorIs(t, err, domain.ErrAgentCLIUnauthenticated)
	require.NotErrorIs(t, err, domain.ErrAgentCLIBinaryMissing)
	assert.Contains(t, err.Error(), "/login", "the CLI's own words are kept: they are what names the fix")
}

// A session failure that is NOT about credentials is reported as itself. Mapping
// every non-zero exit onto "unauthenticated" would have the operator logging in
// again and again while the real fault sat in the output we discarded.
func TestProbeDoesNotCallEveryFailureAnAuthFailure(t *testing.T) {
	bin := fakeCLI(t, "1.2.3", "Error: EACCES: permission denied, open '/nope'", 1)

	_, err := Probe(context.Background(), bin, "")
	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrAgentCLIUnauthenticated)
	assert.NotErrorIs(t, err, domain.ErrAgentCLIBinaryMissing)
	assert.Contains(t, err.Error(), "permission denied", "the CLI's own words are what name the problem")
}

// The happy path reports the absolute path the executor will run and the
// version, which is what the connection row carries as evidence.
func TestProbeReportsPathAndVersion(t *testing.T) {
	bin := fakeCLI(t, "1.2.3 (Claude Code)", `{"result":"ok"}`, 0)

	res, err := Probe(context.Background(), bin, "")
	require.NoError(t, err)
	assert.Equal(t, bin, res.BinaryPath)
	assert.Equal(t, "1.2.3 (Claude Code)", res.Version)
}

// The probe session loads the SAME settings sources a board run will. Probing
// under different settings than the runs it vouches for can pass on a host
// where every run afterwards fails.
func TestProbeSessionUsesTheExecutorsSettingSources(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-claude.sh")
	argv := filepath.Join(dir, "argv.txt")
	require.NoError(t, os.WriteFile(bin, []byte(
		"#!/bin/sh\n"+
			"if [ \"$1\" = \"--version\" ]; then echo 1.2.3; exit 0; fi\n"+
			"echo \"$@\" > "+argv+"\n"+
			"exit 0\n"), 0o755))

	_, err := Probe(context.Background(), bin, "user,project,local")
	require.NoError(t, err)

	recorded, err := os.ReadFile(argv)
	require.NoError(t, err)
	assert.Contains(t, string(recorded), "--setting-sources user,project,local")
	assert.Contains(t, string(recorded), "--max-turns 1", "a probe is one turn: it exists to authenticate, not to work")
}
