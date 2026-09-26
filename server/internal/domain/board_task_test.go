package domain

import "testing"

func TestOnlyAnAnalysisKeepsItsBranchLocal(t *testing.T) {
	for _, tt := range []TaskType{TaskTypeTask, TaskTypeBug, TaskTypeTechnical} {
		if !tt.PublishesBranch() {
			t.Errorf("%s must publish its branch", tt)
		}
	}
	if TaskTypeAnaliz.PublishesBranch() {
		t.Error("an analysis must never publish its branch")
	}
}
