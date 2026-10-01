package board

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	recordOpenQuestionsToolName = "record_open_questions"
	listOpenQuestionsToolName   = "list_open_questions"
)

// QuestionManager is the agent's side of an analiz run's open questions for
// the human — recording, editing, withdrawing and reading them back —
// kept optional for the reason AnnotationManager is.
type QuestionManager interface {
	RecordQuestions(ctx context.Context, repositoryID, taskID uuid.UUID, add []domain.NewQuestionInput, update []domain.UpdateQuestionInput, withdraw []string) ([]domain.TaskQuestion, error)
	ListQuestions(ctx context.Context, repositoryID, taskID uuid.UUID) ([]domain.TaskQuestion, error)
}

type questionView struct {
	Key               string `json:"key"`
	Kind              string `json:"kind"`
	Blocking          bool   `json:"blocking"`
	Status            string `json:"status"`
	Prompt            string `json:"prompt"`
	RecommendedAnswer string `json:"recommended_answer,omitempty"`
	Answer            string `json:"answer,omitempty"`
}

func questionViews(items []domain.TaskQuestion) []questionView {
	out := make([]questionView, 0, len(items))
	for _, q := range items {
		out = append(out, questionView{
			Key:               q.Key,
			Kind:              string(q.Kind),
			Blocking:          q.Blocking,
			Status:            string(q.Status),
			Prompt:            q.Prompt,
			RecommendedAnswer: q.RecommendedAnswer,
			Answer:            q.Answer,
		})
	}
	return out
}

var questionKindEnum = []string{string(domain.QuestionKindProduct), string(domain.QuestionKindTechnical)}

type recordQuestionsTool struct{ kit *ToolKit }

type recordQuestionsArgs struct {
	TaskID    string `json:"task_id"`
	Questions []struct {
		Prompt            string `json:"prompt"`
		Kind              string `json:"kind"`
		Blocking          bool   `json:"blocking"`
		RecommendedAnswer string `json:"recommended_answer"`
	} `json:"questions"`
	Update []struct {
		Key               string  `json:"key"`
		Prompt            *string `json:"prompt"`
		Kind              *string `json:"kind"`
		Blocking          *bool   `json:"blocking"`
		RecommendedAnswer *string `json:"recommended_answer"`
	} `json:"update"`
	Withdraw []string `json:"withdraw"`
}

func newRecordQuestionsTool(kit *ToolKit) port.ToolExecutor {
	return &recordQuestionsTool{kit: kit}
}

func (t *recordQuestionsTool) Name() string { return recordOpenQuestionsToolName }

func (t *recordQuestionsTool) Definition() domain.ToolDefinition {
	questionItem := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"prompt":             map[string]interface{}{"type": "string"},
			"kind":               map[string]interface{}{"type": "string", "enum": questionKindEnum},
			"blocking":           map[string]interface{}{"type": "boolean"},
			"recommended_answer": map[string]interface{}{"type": "string"},
		},
		"required": []string{"prompt", "kind", "blocking"},
	}
	updateItem := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"key":                map[string]interface{}{"type": "string"},
			"prompt":             map[string]interface{}{"type": "string"},
			"kind":               map[string]interface{}{"type": "string", "enum": questionKindEnum},
			"blocking":           map[string]interface{}{"type": "boolean"},
			"recommended_answer": map[string]interface{}{"type": "string"},
		},
		"required": []string{"key"},
	}
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: recordOpenQuestionsToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
					"questions": map[string]interface{}{
						"type":     "array",
						"maxItems": domain.MaxQuestionsPerTask,
						"items":    questionItem,
					},
					"update": map[string]interface{}{
						"type":  "array",
						"items": updateItem,
					},
					"withdraw": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
	}
}

func (t *recordQuestionsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args recordQuestionsArgs
	if arguments != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return toolError(recordOpenQuestionsToolName, fmt.Sprintf("invalid arguments: %v", err))
		}
	}
	taskID, err := t.kit.resolveTaskArg(ctx, args.TaskID)
	if err != nil {
		return toolError(recordOpenQuestionsToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(recordOpenQuestionsToolName, err.Error())
	}
	manager, ok := t.kit.Tasks.(QuestionManager)
	if !ok {
		return toolError(recordOpenQuestionsToolName, "this build cannot record open questions")
	}

	add := make([]domain.NewQuestionInput, 0, len(args.Questions))
	for _, q := range args.Questions {
		add = append(add, domain.NewQuestionInput{
			Prompt: q.Prompt, Kind: domain.QuestionKind(q.Kind), Blocking: q.Blocking, RecommendedAnswer: q.RecommendedAnswer,
		})
	}
	update := make([]domain.UpdateQuestionInput, 0, len(args.Update))
	for _, u := range args.Update {
		var kind *domain.QuestionKind
		if u.Kind != nil {
			k := domain.QuestionKind(*u.Kind)
			kind = &k
		}
		update = append(update, domain.UpdateQuestionInput{
			Key: strings.TrimSpace(u.Key), Prompt: u.Prompt, Kind: kind, Blocking: u.Blocking, RecommendedAnswer: u.RecommendedAnswer,
		})
	}

	items, err := manager.RecordQuestions(ctx, repositoryID, taskID, add, update, args.Withdraw)
	if err != nil {
		return toolError(recordOpenQuestionsToolName, err.Error())
	}
	return toolJSON(recordOpenQuestionsToolName, map[string]interface{}{
		"task_id":   taskID.String(),
		"count":     len(items),
		"questions": questionViews(items),
	})
}

type listOpenQuestionsTool struct{ kit *ToolKit }

type listOpenQuestionsArgs struct {
	TaskID string `json:"task_id"`
}

func newListOpenQuestionsTool(kit *ToolKit) port.ToolExecutor {
	return &listOpenQuestionsTool{kit: kit}
}

func (t *listOpenQuestionsTool) Name() string { return listOpenQuestionsToolName }

func (t *listOpenQuestionsTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: listOpenQuestionsToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
				},
			},
		},
	}
}

func (t *listOpenQuestionsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args listOpenQuestionsArgs
	if arguments != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return toolError(listOpenQuestionsToolName, fmt.Sprintf("invalid arguments: %v", err))
		}
	}
	taskID, err := t.kit.resolveTaskArg(ctx, args.TaskID)
	if err != nil {
		return toolError(listOpenQuestionsToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(listOpenQuestionsToolName, err.Error())
	}
	manager, ok := t.kit.Tasks.(QuestionManager)
	if !ok {
		return toolError(listOpenQuestionsToolName, "this build cannot read open questions")
	}
	items, err := manager.ListQuestions(ctx, repositoryID, taskID)
	if err != nil {
		return toolError(listOpenQuestionsToolName, err.Error())
	}
	return toolJSON(listOpenQuestionsToolName, map[string]interface{}{
		"task_id":   taskID.String(),
		"count":     len(items),
		"questions": questionViews(items),
	})
}
