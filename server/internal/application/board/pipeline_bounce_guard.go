package board

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// One bounce per failed head SHA until a human weighs in.
// Streak is read from pipeline rows, not memory - it must survive restarts and land on whichever pod answers.
type PipelineBounceGuard struct {
	pipelines PipelineHistoryReader
	tasks     TaskCommenter
	parker    ResourceParker
	parks     *ParkJournal
	events    TaskEventHistory
	reader    TaskRunbookReader
}

type PipelineHistoryReader interface {
	ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.TaskPipeline, error)
}

// No parker, no suppression: a bounce with no park stops nothing.
func NewPipelineBounceGuard(pipelines PipelineHistoryReader, tasks TaskCommenter, parker ResourceParker) *PipelineBounceGuard {
	return &PipelineBounceGuard{pipelines: pipelines, tasks: tasks, parker: parker}
}

func (g *PipelineBounceGuard) SetParkJournal(j *ParkJournal) {
	if g != nil {
		g.parks = j
	}
}

func (g *PipelineBounceGuard) SetEventHistory(e TaskEventHistory) {
	if g != nil {
		g.events = e
	}
}

func (g *PipelineBounceGuard) SetTaskReader(r TaskRunbookReader) {
	if g != nil {
		g.reader = r
	}
}

type bounceAction int

const (
	bounceFailOpen bounceAction = iota
	bouncePark
	bounceExplain
	bounceSilent
)

func (g *PipelineBounceGuard) Hold(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, pipeline domain.TaskPipeline) bool {
	if g == nil || g.pipelines == nil {
		return false
	}
	headSHA := strings.TrimSpace(pipeline.HeadSHA)
	if headSHA == "" {
		return false
	}
	history, err := g.pipelines.ListByTask(ctx, task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).
			Msg("pipeline bounce guard: reading the task's pipeline history failed, letting the bounce through")
		return false
	}
	since, ok := g.humanTouchWindow(ctx, task.ID, pipeline.CreatedAt)
	if !ok {
		return false
	}
	prior := priorSameCommitFailures(history, pipeline, headSHA, since)
	if prior == 0 {
		return false
	}

	log.Warn().
		Str("task_id", task.ID.String()).
		Str("pipeline_id", pipeline.ID.String()).
		Str("head_sha", headSHA).
		Int("prior_failures", prior).
		Msg("pipeline bounce guard: same commit failed again, refusing to cycle the task")

	target, action := g.parkTarget(ctx, repositoryID, task)
	switch action {
	case bounceFailOpen:
		return false
	case bouncePark:
		if !g.park(ctx, repositoryID, target, headSHA) {
			return false
		}
		g.comment(ctx, repositoryID, target, pipeline, headSHA)
	case bounceExplain:
		g.comment(ctx, repositoryID, target, pipeline, headSHA)
	}
	return true
}

func (g *PipelineBounceGuard) humanTouchWindow(ctx context.Context, taskID uuid.UUID, reaches time.Time) (time.Time, bool) {
	if g.events == nil {
		return time.Time{}, true
	}
	history, err := g.events.ListByTask(ctx, taskID, reviewLoopHistoryDepth)
	if err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).
			Msg("pipeline bounce guard: reading board history for the human-touch reset failed, letting the bounce through")
		return time.Time{}, false
	}
	if len(history) >= reviewLoopHistoryDepth && !historyReaches(history, reaches) {
		log.Warn().Str("task_id", taskID.String()).Int("events", len(history)).
			Msg("pipeline bounce guard: board history window does not reach this pipeline, letting the bounce through")
		return time.Time{}, false
	}
	at, _ := lastHumanEventAt(history)
	return at, true
}

func historyReaches(history []domain.BoardEvent, at time.Time) bool {
	if len(history) == 0 {
		return false
	}
	return !history[len(history)-1].CreatedAt.Before(at)
}

func priorSameCommitFailures(history []domain.TaskPipeline, pipeline domain.TaskPipeline, headSHA string, since time.Time) int {
	n := 0
	for _, other := range history {
		if other.ID == pipeline.ID {
			continue
		}
		if other.Status != domain.PipelineStatusFailed {
			continue
		}
		if isDeployTrigger(other.Trigger) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(other.HeadSHA), headSHA) {
			continue
		}
		if !pipelineReachedVerdict(other) {
			continue
		}
		if other.CreatedAt.After(pipeline.CreatedAt) {
			continue
		}
		if !since.IsZero() && !other.CreatedAt.After(since) {
			continue
		}
		n++
	}
	return n
}

func pipelineReachedVerdict(p domain.TaskPipeline) bool {
	if p.FinishedAt == nil {
		return false
	}
	switch strings.TrimSpace(p.Provider) {
	case "", domain.PipelineProviderNone:
		return false
	}
	return !pipelineBookkeepingNote(p.Note)
}

func pipelineBookkeepingNote(note string) bool {
	lower := strings.ToLower(note)
	return strings.Contains(lower, "supersede") || strings.Contains(lower, "interrupted")
}

func (g *PipelineBounceGuard) parkTarget(ctx context.Context, repositoryID uuid.UUID, snapshot domain.BoardTask) (domain.BoardTask, bounceAction) {
	if g.reader == nil {
		if parkableTask(snapshot) {
			return snapshot, bouncePark
		}
		return snapshot, heldAction(snapshot)
	}
	fresh, err := g.reader.GetTask(ctx, repositoryID, snapshot.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", snapshot.ID.String()).
			Msg("pipeline bounce guard: re-reading the task before parking failed, letting the bounce through")
		return snapshot, bounceFailOpen
	}
	if !parkableTask(fresh) {
		log.Info().Str("task_id", fresh.ID.String()).Str("column", string(fresh.Column)).
			Str("blocked_resource", fresh.BlockedResource).
			Msg("pipeline bounce guard: the task is already parked, leaving the park alone")
		return fresh, heldAction(fresh)
	}
	if fresh.Column != snapshot.Column {
		log.Warn().Str("task_id", fresh.ID.String()).
			Str("was", string(snapshot.Column)).Str("column", string(fresh.Column)).
			Msg("pipeline bounce guard: the task left the column this pipeline was judging, holding it silently")
		return fresh, bounceSilent
	}
	return fresh, bouncePark
}

func heldAction(task domain.BoardTask) bounceAction {
	if task.Column != domain.TaskColumnBlocked {
		return bounceSilent
	}
	if strings.EqualFold(strings.TrimSpace(task.BlockedResource), domain.ResourceHumanDecision) {
		return bounceSilent
	}
	return bounceExplain
}

func parkableTask(task domain.BoardTask) bool {
	return task.Column != domain.TaskColumnBlocked && strings.TrimSpace(task.BlockedResource) == ""
}

func (g *PipelineBounceGuard) comment(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, pipeline domain.TaskPipeline, headSHA string) {
	if g.tasks == nil {
		return
	}
	body := "Pipeline aynı commit için yine kırmızı (" + domain.ShortSHA(headSHA) + ") ve arada YENİ bir commit gelmedi. " +
		"Bu görev bu commit yüzünden zaten bir kez geri gönderildi; sonuç değişmediği için board onu tekrar döngüye sokmayacak — " +
		"kart `blocked` kolonuna alındı.\n\n" +
		"Yapılması gereken bir insanda: CI'ı düzeltin (build hatası, ya da hesap/faturalandırma kaynaklı olarak " +
		"\"job was not started\" diyen bir Actions çalıştırması) ve ardından kartı elle ilerletin. " +
		"Ajan çalıştırmak bu noktada aynı sonucu üretir ve kotayı harcar."
	if note := strings.TrimSpace(pipeline.Note); note != "" {
		body += "\n\nSon pipeline notu: " + truncateTail(note, 500)
	}
	if _, err := g.tasks.AddComment(ctx, repositoryID, task.ID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content:    body,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).
			Msg("pipeline bounce guard: the loop-stopped comment failed")
	}
}

func (g *PipelineBounceGuard) park(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, headSHA string) bool {
	if g.parker == nil {
		return false
	}
	if task.Column == domain.TaskColumnBlocked {
		return false
	}
	detail := "pipeline is still failing for " + domain.ShortSHA(headSHA) +
		" with no new commit — a human has to fix CI or move this task on"
	previous, err := g.parker.BlockOnResource(ctx, repositoryID, task.ID, domain.ResourceHumanDecision, detail)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).
			Msg("pipeline bounce guard: parking the task failed; letting the bounce through instead")
		return false
	}
	g.parks.Record(ctx, repositoryID, task, previous,
		domain.ResourceHumanDecision, domain.MoveReasonPipelineLoopParked)
	return true
}
