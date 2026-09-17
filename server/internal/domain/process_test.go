package domain

import (
	"os/exec"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExitSignalReadsAKernelReportedSignal(t *testing.T) {
	err := exec.Command("sh", "-c", "kill -TERM $$").Run()
	require.Error(t, err)

	sig, ok := ExitSignal(err)
	assert.True(t, ok)
	assert.Equal(t, syscall.SIGTERM, sig)
}

// The Node CLIs trap SIGTERM and exit 143 themselves, so the kernel never
// reports them as signaled.
func TestExitSignalReadsATrappedSignalFromTheExitCode(t *testing.T) {
	err := exec.Command("sh", "-c", "exit 143").Run()
	require.Error(t, err)

	sig, ok := ExitSignal(err)
	assert.True(t, ok)
	assert.Equal(t, syscall.SIGTERM, sig)
}

func TestExitSignalIgnoresAnOrdinaryFailure(t *testing.T) {
	err := exec.Command("sh", "-c", "exit 1").Run()
	require.Error(t, err)

	_, ok := ExitSignal(err)
	assert.False(t, ok)
}
