package domain

const AskUserToolName = "ask_user"

type ClarificationOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type ClarificationQuestion struct {
	ID            string                `json:"id"`
	Prompt        string                `json:"prompt"`
	AllowMultiple bool                  `json:"allow_multiple,omitempty"`
	Options       []ClarificationOption `json:"options"`
}

type ClarificationRequest struct {
	Context   string                  `json:"context,omitempty"`
	Questions []ClarificationQuestion `json:"questions"`
}

func (r ClarificationRequest) Valid() bool {
	if len(r.Questions) == 0 {
		return false
	}
	for _, q := range r.Questions {
		if q.ID == "" || q.Prompt == "" || len(q.Options) < 2 {
			return false
		}
		for _, o := range q.Options {
			if o.ID == "" || o.Label == "" {
				return false
			}
		}
	}
	return true
}

func ClarificationStepPayload(req ClarificationRequest, source string) map[string]any {
	return map[string]any{
		"source":         source,
		"question_count": len(req.Questions),
		"context":        req.Context,
		"questions":      req.Questions,
	}
}
