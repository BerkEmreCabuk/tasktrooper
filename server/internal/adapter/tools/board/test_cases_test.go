package board

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// A model asked for an optional uuid sends "" about as often as it omits the
// field. Decoding that into *uuid.UUID used to fail the whole call, so one
// empty string cost a batch of twenty recorded cases.
func TestTestCaseArgTreatsAnEmptyCriterionIDAsNoCriterion(t *testing.T) {
	in, err := testCaseArg{Title: "expired token is rejected", CriterionID: ""}.toInput()
	require.NoError(t, err)
	require.Nil(t, in.CriterionID, "an empty criterion_id is a case no criterion states, not an error")

	id := uuid.New()
	in, err = testCaseArg{Title: "happy path", CriterionID: "  " + id.String() + "  "}.toInput()
	require.NoError(t, err)
	require.NotNil(t, in.CriterionID)
	require.Equal(t, id, *in.CriterionID)

	_, err = testCaseArg{Title: "happy path", CriterionID: "criterion 1"}.toInput()
	require.Error(t, err, "a criterion_id that is not a uuid has to be named, not silently dropped")
	require.Contains(t, err.Error(), "list_acceptance_criteria")
}

// The tool schemas and the SQL CHECK constraints are one contract; an enum that
// drifts here is a tool call the database refuses at the end of a QA round.
func TestTestCaseEnumsMatchTheDomain(t *testing.T) {
	require.Equal(t, len(domain.TestCaseStatuses), len(testCaseStatusEnum()))
	require.Contains(t, testCaseStatusEnum(), string(domain.TestCaseStatusInvalid))
	require.Equal(t, len(domain.TestCaseCategories), len(testCaseCategoryEnum()))
	require.Contains(t, testCaseCategoryEnum(), string(domain.TestCaseCategoryRegression))
}
