package board

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func usageWith(tools ...string) *registry.ToolUsage {
	usage := registry.NewToolUsage()
	for _, name := range tools {
		usage.Record(name)
	}
	return usage
}

func TestIsUngroundedAnalysis(t *testing.T) {
	analiz := domain.BoardTask{TaskType: domain.TaskTypeAnaliz}
	answered := domain.AgentResponse{Message: domain.Message{Content: "here is the spec"}}

	cases := []struct {
		name  string
		task  domain.BoardTask
		resp  domain.AgentResponse
		usage *registry.ToolUsage
		want  bool
	}{
		{
			name:  "analiz that only shelled out is ungrounded",
			task:  analiz,
			resp:  answered,
			usage: usageWith("load_skill", "run_terminal", "run_terminal"),
			want:  true,
		},
		{
			name:  "analiz that searched the code is grounded",
			task:  analiz,
			resp:  answered,
			usage: usageWith("run_terminal", "codebase_search"),
			want:  false,
		},
		{
			name:  "implementation task is not gated",
			task:  domain.BoardTask{TaskType: domain.TaskTypeTask},
			resp:  answered,
			usage: usageWith("run_terminal"),
			want:  false,
		},
		{
			name:  "analiz that asked the human a question is exempt",
			task:  analiz,
			resp:  domain.AgentResponse{Clarification: &domain.ClarificationRequest{Context: "which platform?"}},
			usage: usageWith("run_terminal"),
			want:  false,
		},
		{
			name:  "unmeasured run never blocks",
			task:  analiz,
			resp:  answered,
			usage: nil,
			want:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wf := taskWF
			if tc.task.TaskType == domain.TaskTypeAnaliz {
				wf = analizWF
			}
			if got := isUngroundedAnalysis(wf, tc.task, tc.resp, tc.usage); got != tc.want {
				t.Fatalf("isUngroundedAnalysis = %v, want %v", got, tc.want)
			}
		})
	}
}
