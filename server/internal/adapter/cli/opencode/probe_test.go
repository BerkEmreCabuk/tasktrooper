package opencode

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
// identical helper for why this is written per test rather than kept as a
// testdata fixture: what is being probed IS the binary's behaviour.
func fakeCLI(t *testing.T, version, sessionOut string, sessionCode int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-opencode.sh")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then printf '%s\\n' " + shellQuote(version) + "; exit 0; fi\n" +
		"printf '%s\\n' " + shellQuote(sessionOut) + "\n" +
		"exit " + strconv.Itoa(sessionCode) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return path
}

func shellQuote(s string) string { return "'" + s + "'" }

func TestProbeReportsAMissingBinary(t *testing.T) {
	_, err := Probe(context.Background(), "opencode-that-is-not-installed")
	require.ErrorIs(t, err, domain.ErrAgentCLIBinaryMissing)
	require.NotErrorIs(t, err, domain.ErrAgentCLIUnauthenticated)
	assert.Contains(t, err.Error(), "OPENCODE_BIN", "the message has to name the way out")
}

func TestProbeReportsAnUnauthenticatedBinary(t *testing.T) {
	bin := fakeCLI(t, "1.2.3", "Error: not logged in. Run 'opencode auth login'.", 1)

	_, err := Probe(context.Background(), bin)
	require.ErrorIs(t, err, domain.ErrAgentCLIUnauthenticated)
	require.NotErrorIs(t, err, domain.ErrAgentCLIBinaryMissing)
	assert.Contains(t, err.Error(), "not logged in", "the CLI's own words are kept: they are what names the fix")
}

func TestProbeDoesNotCallEveryFailureAnAuthFailure(t *testing.T) {
	bin := fakeCLI(t, "1.2.3", "Error: EACCES: permission denied, open '/nope'", 1)

	_, err := Probe(context.Background(), bin)
	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrAgentCLIUnauthenticated)
	assert.NotErrorIs(t, err, domain.ErrAgentCLIBinaryMissing)
	assert.Contains(t, err.Error(), "permission denied")
}

func TestProbeReportsPathAndVersion(t *testing.T) {
	bin := fakeCLI(t, "1.2.3", `{"ok":true}`, 0)

	res, err := Probe(context.Background(), bin)
	require.NoError(t, err)
	assert.Equal(t, bin, res.BinaryPath)
	assert.Equal(t, "1.2.3", res.Version)
}

func TestProbeReportsABinaryThatWillNotRunAtAll(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-opencode.sh")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\nexit 127\n"), 0o755))

	_, err := Probe(context.Background(), bin)
	require.ErrorIs(t, err, domain.ErrAgentCLIBinaryMissing)
}
