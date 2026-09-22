package domain

// StageNotOnWorkflowError is what validateStageConfigured returns when a
// move (or a task-type change) targets a column this task's type has no
// workflow_stages row for. The column is real on the board; this type's
// curated stage list simply never put it there, so nothing configured for
// it would ever fire — entering is refused rather than silently inert.
type StageNotOnWorkflowError struct {
	TaskType TaskType
	Target   TaskColumn
	message  string
}

func NewStageNotOnWorkflowError(taskType TaskType, target TaskColumn, message string) *StageNotOnWorkflowError {
	return &StageNotOnWorkflowError{TaskType: taskType, Target: target, message: message}
}

func (e *StageNotOnWorkflowError) Error() string { return e.message }
