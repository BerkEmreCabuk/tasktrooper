package board

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// This file pins the exact, current byte output of every system-comment
// string this package builds and replays into an agent's own run history —
// runner.go's revisionCommentsMessage carries every non-"user" task comment
// back into the next run, so these are model-facing even though they read
// like status text. Moving them into catalog/system next needs a way to
// prove the move changed nothing.

func TestGolden_UnsettledCriteriaReport(t *testing.T) {
	one := []domain.AcceptanceCriterion{{ID: goldenCriterionID1, Text: "Export includes the archived rows"}}
	assert.Equal(t,
		"This run ended with 1 acceptance criterion/criteria unsettled after 3 completion checks:\n"+
			"- Export includes the archived rows\n"+
			"\nThey were neither implemented nor cancelled with a reason, so the task stays in this column: "+
			"the hand-off to code_review is refused while a criterion is open. Either the work is still missing, "+
			"or the criterion needs a decision only a person can make.",
		unsettledCriteriaReport(one, 3))

	two := []domain.AcceptanceCriterion{
		{ID: goldenCriterionID1, Text: "Export includes the archived rows"},
		{ID: goldenCriterionID2, Text: "CSV rows are UTF-8"},
	}
	assert.Equal(t,
		"This run ended with 2 acceptance criterion/criteria unsettled after 3 completion checks:\n"+
			"- Export includes the archived rows\n"+
			"- CSV rows are UTF-8\n"+
			"\nThey were neither implemented nor cancelled with a reason, so the task stays in this column: "+
			"the hand-off to code_review is refused while a criterion is open. Either the work is still missing, "+
			"or the criterion needs a decision only a person can make.",
		unsettledCriteriaReport(two, 3))
}

func TestGolden_UnsettledCriteriaSummary(t *testing.T) {
	assert.Equal(t, "unsettled acceptance criteria: 2 still open after the criteria sweep", unsettledCriteriaSummary(2))
}

func TestGolden_PlanVerificationFailureComment(t *testing.T) {
	assert.Equal(t,
		"Otomatik doğrulama başarısız: bu run'ın sonucu hedefi karşılamıyor, bu yüzden görev code_review'a devredilmedi.\n"+
			"\nBir sonraki run bu maddeleri kapatmalı; kapanmadan görev ilerlemez.",
		planVerificationFailureComment(domain.VerificationResult{}))

	assert.Equal(t,
		"Otomatik doğrulama başarısız: bu run'ın sonucu hedefi karşılamıyor, bu yüzden görev code_review'a devredilmedi.\n"+
			"\nThe export drops archived rows.\n"+
			"\nAçık bulgular:\n"+
			"1. Restore the archived rows to the export\n"+
			"2. Add a regression test\n"+
			"\nBir sonraki run bu maddeleri kapatmalı; kapanmadan görev ilerlemez.",
		planVerificationFailureComment(domain.VerificationResult{
			Summary: "The export drops archived rows.",
			Issues:  []string{"Restore the archived rows to the export", "Add a regression test"},
		}))
}

func TestGolden_VerificationFailureComment(t *testing.T) {
	assert.Equal(t,
		"Automated verification failed — build/vet errors:\n\n```\nexit status 1\n```",
		verificationFailureComment("exit status 1"))
}

func TestGolden_StuckColumnComment(t *testing.T) {
	assert.Equal(t,
		"Review tamamlandı ama kart hâlâ `code_review` kolonunda: değerlendirme sonrası "+
			"`ready_for_qa` veya `need_revision` geçişi yapılmadı. Kolonu elle taşımak gerekiyor.",
		stuckColumnComment(domain.TaskColumnCodeReview, domain.TaskColumnReadyForQA, ""))

	assert.Equal(t,
		"Review tamamlandı ama kart hâlâ `code_review` kolonunda: değerlendirme sonrası "+
			"`ready_for_qa` veya `need_revision` geçişi yapılmadı. Geçiş kapısı 1 kabul kriterini qa verdict'i "+
			"olmadan geçirmiyor: Export includes the archived rows. Kolonu elle taşımak gerekiyor.",
		stuckColumnComment(domain.TaskColumnCodeReview, domain.TaskColumnReadyForQA,
			" Geçiş kapısı 1 kabul kriterini qa verdict'i olmadan geçirmiyor: Export includes the archived rows."))
}

func TestGolden_ReviewNoPRReason(t *testing.T) {
	assert.Equal(t,
		"Code review did not start: the task branch has no pull request. "+
			"A review is done on the PR, so the branch must be pushed and a PR opened before code_review. "+
			"Details: no origin remote configured",
		reviewNoPRReason(errors.New("no origin remote configured")))
}

func TestGolden_ReviewEntryLabels(t *testing.T) {
	a := domain.TaskDocumentAnnotation{Quote: "the export drops archived rows", Body: "please restore them"}
	assert.Equal(t,
		"\n### Comment 1 — id 00000000-0000-0000-0000-000000000000\n"+
			"Passage: \"the export drops archived rows\"\n"+
			"Comment: please restore them\n",
		reviewEntry(1, a, ""))
}

func TestGolden_PostDeployNotesComment(t *testing.T) {
	assert.Equal(t, "Deploy sonrası yapılacaklar:\nAndroid store listing needs the new screenshots.",
		postDeployNotesComment("Android store listing needs the new screenshots."))
}

func TestGolden_DeploySkipComment(t *testing.T) {
	assert.Equal(t,
		"Released without a verified deploy: no deploy workflow configured, so nothing was actually built or "+
			"shipped by CI for this task. Configure a deploy workflow mapping for this repository, or deploy and verify manually.",
		deploySkipComment("no deploy workflow configured"))
}

func TestGolden_PipelineCouldNotStartComment(t *testing.T) {
	assert.Equal(t, "Pipeline could not start: workspace unavailable", pipelineCouldNotStartComment("workspace unavailable"))
}

func TestGolden_DiffSkipSummary(t *testing.T) {
	approvedAt := time.Date(2026, 3, 4, 12, 30, 0, 0, time.UTC)
	assert.Equal(t,
		"Skipped code_review: the diff is identical (patch-id abcdef123456) to the one approved at 2026-03-04 12:30Z.",
		diffSkipSummary(domain.TaskColumnCodeReview, "abcdef123456", approvedAt))
}

func TestGolden_RejectedDraftNote(t *testing.T) {
	assert.Equal(t, "\n\nRejected draft (not attached as the analysis):\n\nDraft text.", rejectedDraftNote("Draft text."))
}

func TestGolden_RejectedRunReportReplanNote(t *testing.T) {
	assert.Equal(t, "\n\nWhat the rejected run reported (execute this, do not re-plan it):\n\nRan the tests.",
		rejectedRunReportReplanNote("Ran the tests."))
}

func TestGolden_RejectedRunReportReapproveNote(t *testing.T) {
	assert.Equal(t, "\n\nWhat the rejected run reported (execute this, do not re-approve it):\n\nApproved.",
		rejectedRunReportReapproveNote("Approved."))
}
