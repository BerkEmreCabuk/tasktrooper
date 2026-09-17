package cursor

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

// fakeCursorAgent writes a stand-in binary that answers --version with
// version and `status` with statusOut/statusCode — see claudecode's
// identical fakeCLI for why the response is baked into the script rather than
// read from a fixture file: probeVersion/probeAuth pin cmd.Dir to
// os.TempDir(), not the test's own working directory, so a file-based fixture
// would have to fight that rather than the script just answering directly.
func fakeCursorAgent(t *testing.T, version, statusOut string, statusCode int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-cursor-agent.sh")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then printf '%s\\n' " + shellQuote(version) + "; exit 0; fi\n" +
		"if [ \"$1\" = \"status\" ]; then printf '%s\\n' " + shellQuote(statusOut) + "; exit " + strconv.Itoa(statusCode) + "; fi\n" +
		"exit 1\n"
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return path
}

func shellQuote(s string) string { return "'" + s + "'" }

func TestProbeReportsAMissingBinary(t *testing.T) {
	_, err := Probe(context.Background(), "definitely-not-a-real-cursor-agent-binary")
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrAgentCLIBinaryMissing)
}

// `status` is the CLI's own documented way to report auth, unlike
// claudecode/antigravity/opencode which spend a real turn — see probe.go's
// comment on probeAuth for why.
func TestProbeReportsAnUnauthenticatedBinary(t *testing.T) {
	bin := fakeCursorAgent(t, "1.2.3", "Not authenticated. Run `cursor-agent login`.", 1)

	_, err := Probe(context.Background(), bin)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrAgentCLIUnauthenticated)
	assert.NotErrorIs(t, err, domain.ErrAgentCLIBinaryMissing)
}

func TestProbeDoesNotCallEveryFailureAnAuthFailure(t *testing.T) {
	bin := fakeCursorAgent(t, "1.2.3", "internal error: connection reset", 1)

	_, err := Probe(context.Background(), bin)
	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrAgentCLIUnauthenticated,
		"an unrecognised failure must be reported as itself, not guessed as an auth problem")
}

func TestProbeReportsPathAndVersion(t *testing.T) {
	bin := fakeCursorAgent(t, "cursor-agent 2026.01.01", "Logged in as dev@example.com", 0)

	result, err := Probe(context.Background(), bin)
	require.NoError(t, err)
	assert.Equal(t, bin, result.BinaryPath)
	assert.Equal(t, "cursor-agent 2026.01.01", result.Version)
}
