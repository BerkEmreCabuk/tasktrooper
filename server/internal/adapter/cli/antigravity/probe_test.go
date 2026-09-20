package antigravity

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
// every other invocation with sessionOut and sessionCode — see claudecode's
// identical helper for why this is written per test rather than kept in
// testdata.
func fakeCLI(t *testing.T, version, sessionOut string, sessionCode int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-agy.sh")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then printf '%s\\n' " + shellQuote(version) + "; exit 0; fi\n" +
		"printf '%s\\n' " + shellQuote(sessionOut) + "\n" +
		"exit " + strconv.Itoa(sessionCode) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return path
}

func shellQuote(s string) string { return "'" + s + "'" }

// A binary that is not there is its own error, and the sentence names the fix.
func TestProbeReportsAMissingBinary(t *testing.T) {
	_, err := Probe(context.Background(), "agy-that-is-not-installed")
	require.ErrorIs(t, err, domain.ErrAgentCLIBinaryMissing)
	require.NotErrorIs(t, err, domain.ErrAgentCLIUnauthenticated)
	assert.Contains(t, err.Error(), "ANTIGRAVITY_BIN", "the message has to name the way out")
}

// An installed binary with no session is a DIFFERENT error, because it has a
// different fix.
func TestProbeReportsAnUnauthenticatedBinary(t *testing.T) {
	bin := fakeCLI(t, "1.1.23", "Error: not logged in. Run `agy login`.", 1)

	_, err := Probe(context.Background(), bin)
	require.ErrorIs(t, err, domain.ErrAgentCLIUnauthenticated)
	require.NotErrorIs(t, err, domain.ErrAgentCLIBinaryMissing)
	assert.Contains(t, err.Error(), "not logged in", "the CLI's own words are kept: they are what names the fix")
}

// A session failure that is NOT about credentials is reported as itself.
func TestProbeDoesNotCallEveryFailureAnAuthFailure(t *testing.T) {
	bin := fakeCLI(t, "1.1.23", "Error: EACCES: permission denied, open '/nope'", 1)

	_, err := Probe(context.Background(), bin)
	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrAgentCLIUnauthenticated)
	assert.NotErrorIs(t, err, domain.ErrAgentCLIBinaryMissing)
	assert.Contains(t, err.Error(), "permission denied", "the CLI's own words are what name the problem")
}

// The happy path reports the absolute path the executor will run and the
// version, which is what the connection row carries as evidence.
func TestProbeReportsPathAndVersion(t *testing.T) {
	bin := fakeCLI(t, "1.1.23", `{"result":"ok"}`, 0)

	res, err := Probe(context.Background(), bin)
	require.NoError(t, err)
	assert.Equal(t, bin, res.BinaryPath)
	assert.Equal(t, "1.1.23", res.Version)
}

// The probe session is one turn, in print mode, with no human at the terminal
// to approve an edit — the same shape a real run gets.
func TestProbeSessionAsksOneToolFreeTurn(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-agy.sh")
	argv := filepath.Join(dir, "argv.txt")
	require.NoError(t, os.WriteFile(bin, []byte(
		"#!/bin/sh\n"+
			"if [ \"$1\" = \"--version\" ]; then echo 1.1.23; exit 0; fi\n"+
			"echo \"$@\" > "+argv+"\n"+
			"exit 0\n"), 0o755))

	_, err := Probe(context.Background(), bin)
	require.NoError(t, err)

	recorded, err := os.ReadFile(argv)
	require.NoError(t, err)
	assert.Contains(t, string(recorded), "--output-format json")
	assert.Contains(t, string(recorded), "--dangerously-skip-permissions")
	assert.Contains(t, string(recorded), "-p Reply with exactly: ok")
}
