package llm

import (
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// A browser tool answers with a screenshot the model must be able to see.
// Anthropic tool_result content supports image blocks next to the text; a
// translation that keeps the plain-string form silently blinds the model.
func TestBuildAnthropicRequestToolResultImages(t *testing.T) {
	base := []domain.Message{
		{Role: domain.RoleUser, Content: "open the page"},
		{Role: domain.RoleAssistant, Content: "opening", ToolCalls: []domain.ToolCall{
			{ID: "call12345", Type: "function", Function: domain.FunctionCall{Name: "browser_screenshot", Arguments: `{}`}},
		}},
	}

	tests := []struct {
		name       string
		toolMsg    domain.Message
		wantString bool     // content stays the plain string (backward-compatible wire format)
		wantBlocks []string // expected block types, in order
	}{
		{
			name: "no images keeps plain string content",
			toolMsg: domain.Message{
				Role: domain.RoleTool, ToolCallID: "call12345", Name: "browser_screenshot",
				Content: "ok",
			},
			wantString: true,
		},
		{
			name: "text plus one image becomes text and image blocks",
			toolMsg: domain.Message{
				Role: domain.RoleTool, ToolCallID: "call12345", Name: "browser_screenshot",
				Content: "screenshot taken",
				Images:  []domain.ToolResultImage{{MediaType: "image/png", Data: "aGVsbG8="}},
			},
			wantBlocks: []string{"text", "image"},
		},
		{
			name: "image without text has no empty text block",
			toolMsg: domain.Message{
				Role: domain.RoleTool, ToolCallID: "call12345", Name: "browser_screenshot",
				Images: []domain.ToolResultImage{{MediaType: "image/png", Data: "aGVsbG8="}},
			},
			wantBlocks: []string{"image"},
		},
		{
			name: "multiple images all survive",
			toolMsg: domain.Message{
				Role: domain.RoleTool, ToolCallID: "call12345", Name: "browser_screenshot",
				Content: "two frames",
				Images: []domain.ToolResultImage{
					{MediaType: "image/png", Data: "Zmlyc3Q="},
					{MediaType: "image/jpeg", Data: "c2Vjb25k"},
				},
			},
			wantBlocks: []string{"text", "image", "image"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := buildAnthropicRequest("claude-sonnet-4", append(append([]domain.Message{}, base...), tt.toolMsg), nil, false, nil, 0)

			var result *anthropicContent
			for _, msg := range req.Messages {
				for i := range msg.Content {
					if msg.Content[i].Type == "tool_result" {
						result = &msg.Content[i]
					}
				}
			}
			if result == nil {
				t.Fatal("no tool_result block in request")
			}
			if result.ToolUseID != "call12345" {
				t.Errorf("tool_use_id = %q, want call12345", result.ToolUseID)
			}

			if tt.wantString {
				got, ok := result.Content.(string)
				if !ok {
					t.Fatalf("content = %T, want plain string when no images are attached", result.Content)
				}
				if got != tt.toolMsg.Content {
					t.Errorf("content = %q, want %q", got, tt.toolMsg.Content)
				}
				return
			}

			blocks, ok := result.Content.([]anthropicContent)
			if !ok {
				t.Fatalf("content = %T, want []anthropicContent when images are attached", result.Content)
			}
			if len(blocks) != len(tt.wantBlocks) {
				t.Fatalf("blocks = %d, want %d", len(blocks), len(tt.wantBlocks))
			}
			imgIdx := 0
			for i, b := range blocks {
				if b.Type != tt.wantBlocks[i] {
					t.Errorf("block[%d].Type = %q, want %q", i, b.Type, tt.wantBlocks[i])
				}
				switch b.Type {
				case "text":
					if b.Text != tt.toolMsg.Content {
						t.Errorf("text block = %q, want %q", b.Text, tt.toolMsg.Content)
					}
				case "image":
					want := tt.toolMsg.Images[imgIdx]
					if b.Source == nil {
						t.Fatalf("block[%d] image has no source", i)
					}
					if b.Source.Type != "base64" {
						t.Errorf("source.Type = %q, want base64", b.Source.Type)
					}
					if b.Source.MediaType != want.MediaType || b.Source.Data != want.Data {
						t.Errorf("source = %s/%s, want %s/%s", b.Source.MediaType, b.Source.Data, want.MediaType, want.Data)
					}
					imgIdx++
				}
			}
		})
	}
}

// OpenAI-compatible tool messages are text-only, so a tool-result screenshot is
// carried into the user turn right after the batch instead of being dropped: a
// screenshot the model never receives turns every visual verdict into a guess.
func TestBuildChatMessagesCarriesToolResultImagesToUserTurn(t *testing.T) {
	msgs := buildChatMessages([]domain.Message{
		{Role: domain.RoleUser, Content: "open the page"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{
			{ID: "call12345", Type: "function", Function: domain.FunctionCall{Name: "browser_screenshot", Arguments: `{}`}},
		}},
		{
			Role: domain.RoleTool, ToolCallID: "call12345", Name: "browser_screenshot",
			Content: "screenshot taken",
			Images: []domain.ToolResultImage{
				{MediaType: "image/png", Data: "Zmlyc3Q="},
				{MediaType: "image/png", Data: "c2Vjb25k"},
			},
		},
	})

	var toolMsg *chatMessage
	for i := range msgs {
		if msgs[i].Role == string(domain.RoleTool) {
			toolMsg = &msgs[i]
		}
	}
	if toolMsg == nil {
		t.Fatal("tool message missing")
	}
	if !strings.Contains(toolMsg.Content, "screenshot taken") {
		t.Errorf("content lost the tool's text: %q", toolMsg.Content)
	}
	if !strings.Contains(toolMsg.Content, "they are in the message right after this tool batch") {
		t.Errorf("content lost the carried-images note: %q", toolMsg.Content)
	}
	if strings.Contains(toolMsg.Content, "Zmlyc3Q=") {
		t.Errorf("base64 image data leaked into text content: %q", toolMsg.Content)
	}

	last := msgs[len(msgs)-1]
	if last.Role != string(domain.RoleUser) {
		t.Fatalf("images were not carried into a user turn, last role = %q", last.Role)
	}
	var images []string
	for _, p := range last.ContentParts {
		if p.Type == "image_url" && p.ImageURL != nil {
			images = append(images, p.ImageURL.URL)
		}
	}
	if len(images) != 2 {
		t.Fatalf("carried %d images, want 2", len(images))
	}
	if images[0] != "data:image/png;base64,Zmlyc3Q=" || images[1] != "data:image/png;base64,c2Vjb25k" {
		t.Errorf("carried images are wrong: %v", images)
	}
}

// A model that cannot see images must say so instead of describing a screenshot
// it never received — that invented description is what let a broken store
// badge pass a developer and a QA round in a row.
func TestToolImagePreambleForbidsAnInventedVisualVerdict(t *testing.T) {
	text := toolImagePreamble(1)
	if !strings.Contains(text, "If you cannot see images at all") {
		t.Errorf("preamble does not tell a blind model to speak up: %q", text)
	}
}
