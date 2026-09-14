package domain_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// decode marshals the task and hands back the raw object, which is what a client
// actually sees — a struct assertion would pass on a renamed json tag.
func decode(t *testing.T, task domain.BoardTask) map[string]json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(task)
	require.NoError(t, err)
	var out map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

// The park state is part of every task response, present or not. `omitempty`
// made "this task is not parked" and "this build does not send park state" the
// same absence, which a client cannot tell apart — so an unparked task says so
// explicitly, with an empty resource and a null resume time.
func TestUnparkedTaskStillReportsItsParkState(t *testing.T) {
	fields := decode(t, domain.BoardTask{Column: domain.TaskColumnInProgress})

	resource, ok := fields["blocked_resource"]
	require.True(t, ok, "blocked_resource must be present on every task")
	assert.JSONEq(t, `""`, string(resource))

	resumeAt, ok := fields["blocked_resume_at"]
	require.True(t, ok, "blocked_resume_at must be present on every task")
	assert.JSONEq(t, `null`, string(resumeAt))
}

// The quota park is the only block with a known end, and the card renders it as
// "resumes at HH:MM" — so the time has to travel as RFC3339, which is what the
// clients parse.
func TestQuotaParkedTaskCarriesItsResumeTime(t *testing.T) {
	resumeAt := time.Date(2026, 8, 17, 14, 30, 0, 0, time.UTC)
	fields := decode(t, domain.BoardTask{
		Column:          domain.TaskColumnBlocked,
		BlockedResource: domain.ResourceClaudeCodeQuota,
		BlockedResumeAt: &resumeAt,
	})

	assert.JSONEq(t, `"`+domain.ResourceClaudeCodeQuota+`"`, string(fields["blocked_resource"]))
	assert.JSONEq(t, `"2026-08-17T14:30:00Z"`, string(fields["blocked_resume_at"]))
}

// A device, deploy or work-order park has no predictable end. Guessing one would
// put a countdown on a card that cannot honour it, so the resume time stays null
// and the UI shows the resource alone.
func TestNonQuotaParksCarryNoResumeTime(t *testing.T) {
	for _, resource := range []string{domain.ResourceMobileDevice, domain.ResourceDeployWatch, domain.ResourceWorkOrder} {
		fields := decode(t, domain.BoardTask{Column: domain.TaskColumnBlocked, BlockedResource: resource})
		assert.JSONEq(t, `"`+resource+`"`, string(fields["blocked_resource"]))
		assert.JSONEq(t, `null`, string(fields["blocked_resume_at"]), resource)
	}
}
