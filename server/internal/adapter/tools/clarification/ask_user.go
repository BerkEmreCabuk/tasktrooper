package clarification

import (
	"context"
	"fmt"

	goccyjson "github.com/goccy/go-json"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type askUserArgs struct {
	Context   string                         `json:"context"`
	Questions []domain.ClarificationQuestion `json:"questions"`
}

type askUserTool struct{}

func NewAskUserTool() port.ToolExecutor {
	return &askUserTool{}
}

func (t *askUserTool) Name() string {
	return domain.AskUserToolName
}

func (t *askUserTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        domain.AskUserToolName,
			Description: prompt.AskUserToolDescription,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"context": map[string]interface{}{
						"type":        "string",
						"description": prompt.AskUserContextParamDescription,
					},
					"questions": map[string]interface{}{
						"type":        "array",
						"maxItems":    5,
						"description": prompt.AskUserQuestionsParamDescription,
						"items": map[string]interface{}{
							"type":                 "object",
							"additionalProperties": false,
							"properties": map[string]interface{}{
								"id": map[string]interface{}{
									"type":        "string",
									"description": prompt.AskUserQuestionIDParamDescription,
								},
								"prompt": map[string]interface{}{
									"type":        "string",
									"description": prompt.AskUserQuestionPromptParamDescription,
								},
								"allow_multiple": map[string]interface{}{
									"type":        "boolean",
									"description": prompt.AskUserQuestionAllowMultipleParamDescription,
								},
								"options": map[string]interface{}{
									"type":        "array",
									"minItems":    2,
									"maxItems":    6,
									"description": prompt.AskUserOptionsParamDescription,
									"items": map[string]interface{}{
										"type":                 "object",
										"additionalProperties": false,
										"properties": map[string]interface{}{
											"id": map[string]interface{}{
												"type":        "string",
												"description": prompt.AskUserOptionIDParamDescription,
											},
											"label": map[string]interface{}{
												"type":        "string",
												"description": prompt.AskUserOptionLabelParamDescription,
											},
										},
										"required": []string{"id", "label"},
									},
								},
							},
							"required": []string{"id", "prompt", "options"},
						},
					},
				},
				"required": []string{"context", "questions"},
			},
		},
	}
}

func (t *askUserTool) Execute(_ context.Context, arguments string) domain.ToolResult {
	var args askUserArgs
	if err := goccyjson.Unmarshal([]byte(arguments), &args); err != nil {
		return domain.ToolResult{
			Name:    domain.AskUserToolName,
			Content: fmt.Sprintf("invalid arguments: %v", err),
			IsError: true,
		}
	}
	req := domain.ClarificationRequest{
		Context:   args.Context,
		Questions: domain.NormalizeClarificationQuestions(args.Questions),
	}
	if !req.Valid() {
		return domain.ToolResult{
			Name:    domain.AskUserToolName,
			Content: "questions must include id, prompt, and at least two options each",
			IsError: true,
		}
	}
	if err := validateAskUserQuestions(req.Questions); err != nil {
		return domain.ToolResult{
			Name:    domain.AskUserToolName,
			Content: err.Error(),
			IsError: true,
		}
	}
	return domain.ToolResult{
		Name:          domain.AskUserToolName,
		Content:       "awaiting user clarification",
		Clarification: &req,
	}
}
