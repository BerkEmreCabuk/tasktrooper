package board

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type stubToolchains struct {
	result    port.Toolchain
	err       error
	available bool
	workspace string
	calls     int
}

func (s *stubToolchains) Available() bool { return s.available }

func (s *stubToolchains) Detect(_ context.Context, workspace string) (port.Toolchain, error) {
	s.calls++
	s.workspace = workspace
	return s.result, s.err
}

func toolchainJob() RunJob {
	return RunJob{Task: domain.BoardTask{ID: uuid.New()}}
}

func TestDetectToolchainReadsTheRunsOwnWorkspace(t *testing.T) {
	stub := &stubToolchains{available: true, result: port.Toolchain{
		Pins: []port.ToolchainPin{{Language: "node", Version: "20.11.0", Exact: true, Source: ".nvmrc"}},
		Env:  map[string]string{"NODE_VERSION": "20.11.0"},
	}}
	r := &Runner{toolchains: stub}

	env := r.detectToolchain(context.Background(), toolchainJob(), "/data/workspaces/t-1")

	assert.Equal(t, map[string]string{"NODE_VERSION": "20.11.0"}, env)
	assert.Equal(t, "/data/workspaces/t-1", stub.workspace)
}

func TestDetectToolchainFailureDoesNotStopTheRun(t *testing.T) {
	for _, err := range []error{errors.New("boom"), context.DeadlineExceeded} {
		r := &Runner{toolchains: &stubToolchains{available: true, err: err}}
		assert.Nil(t, r.detectToolchain(context.Background(), toolchainJob(), "/data/workspaces/t-1"))
	}
}

func TestDetectToolchainReportsNothingRatherThanADefault(t *testing.T) {
	r := &Runner{toolchains: &stubToolchains{available: true, result: port.Toolchain{Env: map[string]string{}}}}
	assert.Nil(t, r.detectToolchain(context.Background(), toolchainJob(), "/data/workspaces/t-1"))
}

func TestDetectToolchainIsSkippedWithoutADetector(t *testing.T) {
	assert.Nil(t, (&Runner{}).detectToolchain(context.Background(), toolchainJob(), "/w"))

	unavailable := &stubToolchains{available: false, result: port.Toolchain{Env: map[string]string{"GOTOOLCHAIN": "go1.24.0"}}}
	assert.Nil(t, (&Runner{toolchains: unavailable}).detectToolchain(context.Background(), toolchainJob(), "/w"))
	assert.Zero(t, unavailable.calls)
}
