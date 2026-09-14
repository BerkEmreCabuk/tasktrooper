package llm

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The two images a user attaches to a chat message. Distinct media types and
// payloads so a translation that mixes them up cannot pass.
var userImages = []domain.ToolResultImage{
	{MediaType: "image/png", Data: "Zmlyc3Q="},
	{MediaType: "image/jpeg", Data: "c2Vjb25k"},
}

// Anthropic takes multimodal user input as a content-block array — the same
// shape a tool_result uses for its screenshots.
func TestBuildAnthropicRequestUserMessageImages(t *testing.T) {
	req := buildAnthropicRequest("claude-sonnet-4", []domain.Message{
		{Role: domain.RoleUser, Content: "what is in these?", Images: userImages},
	}, nil, false, nil, 0)

	if len(req.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(req.Messages))
	}
	blocks := req.Messages[0].Content
	want := []string{"text", "image", "image"}
	if len(blocks) != len(want) {
		t.Fatalf("blocks = %d, want %d (%+v)", len(blocks), len(want), blocks)
	}
	if blocks[0].Type != "text" || blocks[0].Text != "what is in these?" {
		t.Errorf("block[0] = %+v, want the message text", blocks[0])
	}
	for i, img := range userImages {
		b := blocks[i+1]
		if b.Type != "image" || b.Source == nil {
			t.Fatalf("block[%d] = %+v, want an image block", i+1, b)
		}
		if b.Source.Type != "base64" {
			t.Errorf("block[%d].source.Type = %q, want base64", i+1, b.Source.Type)
		}
		if b.Source.MediaType != img.MediaType || b.Source.Data != img.Data {
			t.Errorf("block[%d].source = %s/%s, want %s/%s", i+1, b.Source.MediaType, b.Source.Data, img.MediaType, img.Data)
		}
	}
}

// An image with no accompanying text must not produce an empty text block.
func TestBuildAnthropicRequestUserImageWithoutText(t *testing.T) {
	req := buildAnthropicRequest("claude-sonnet-4", []domain.Message{
		{Role: domain.RoleUser, Images: userImages[:1]},
	}, nil, false, nil, 0)

	blocks := req.Messages[0].Content
	if len(blocks) != 1 || blocks[0].Type != "image" {
		t.Fatalf("blocks = %+v, want a single image block", blocks)
	}
}

// The no-image path is the overwhelmingly common one and must keep the exact
// serialization it has always had.
func TestBuildAnthropicRequestUserMessageWithoutImagesUnchanged(t *testing.T) {
	req := buildAnthropicRequest("claude-sonnet-4", []domain.Message{
		{Role: domain.RoleUser, Content: "plain text"},
	}, nil, false, nil, 0)

	blocks := req.Messages[0].Content
	if len(blocks) != 1 || blocks[0].Type != "text" || blocks[0].Text != "plain text" {
		t.Fatalf("blocks = %+v, want one text block", blocks)
	}
	if blocks[0].Source != nil {
		t.Errorf("text block carries an image source: %+v", blocks[0].Source)
	}
}

// OpenAI-compatible servers take multimodal user input as a content array of
// text and image_url parts, the image carried as a base64 data: URI.
func TestBuildChatMessagesUserMessageImagesBecomeParts(t *testing.T) {
	msgs := buildChatMessages([]domain.Message{
		{Role: domain.RoleUser, Content: "what is in these?", Images: userImages},
	})
	if len(msgs) != 1 {
		t.Fatalf("messages = %d, want 1", len(msgs))
	}

	raw, err := json.Marshal(msgs[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got struct {
		Role    string `json:"role"`
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			ImageURL *struct {
				URL string `json:"url"`
			} `json:"image_url"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("content did not serialize as an array: %v (%s)", err, raw)
	}
	if got.Role != "user" {
		t.Errorf("role = %q, want user", got.Role)
	}
	if len(got.Content) != 3 {
		t.Fatalf("parts = %d, want 3 (%s)", len(got.Content), raw)
	}
	if got.Content[0].Type != "text" || got.Content[0].Text != "what is in these?" {
		t.Errorf("part[0] = %+v, want the message text", got.Content[0])
	}
	wantURLs := []string{
		"data:image/png;base64,Zmlyc3Q=",
		"data:image/jpeg;base64,c2Vjb25k",
	}
	for i, want := range wantURLs {
		p := got.Content[i+1]
		if p.Type != "image_url" || p.ImageURL == nil {
			t.Fatalf("part[%d] = %+v, want an image_url part", i+1, p)
		}
		if p.ImageURL.URL != want {
			t.Errorf("part[%d].image_url.url = %q, want %q", i+1, p.ImageURL.URL, want)
		}
	}
}

// Regression guard: strict OpenAI-compatible servers reject an array where they
// expect a string, so a user message without images must stay a plain string.
func TestBuildChatMessagesUserMessageWithoutImagesStaysAString(t *testing.T) {
	msgs := buildChatMessages([]domain.Message{
		{Role: domain.RoleUser, Content: "plain text"},
	})
	raw, err := json.Marshal(msgs[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(got.Content) != `"plain text"` {
		t.Errorf("content = %s, want the plain string %q", got.Content, "plain text")
	}
	if strings.Contains(string(raw), "image_url") {
		t.Errorf("no-image message grew image parts: %s", raw)
	}
}

// Gemini takes multimodal user input as inline_data parts behind the text.
func TestBuildGeminiContentsUserMessageImages(t *testing.T) {
	contents, _ := buildGeminiContents([]domain.Message{
		{Role: domain.RoleUser, Content: "what is in these?", Images: userImages},
	})
	if len(contents) != 1 {
		t.Fatalf("contents = %d, want 1", len(contents))
	}
	parts := contents[0].Parts
	if len(parts) != 3 {
		t.Fatalf("parts = %d, want 3", len(parts))
	}
	if parts[0].Text != "what is in these?" {
		t.Errorf("part[0].Text = %q, want the message text", parts[0].Text)
	}
	for i, img := range userImages {
		blob := parts[i+1].InlineData
		if blob == nil {
			t.Fatalf("part[%d] has no inline data", i+1)
		}
		if blob.MIMEType != img.MediaType {
			t.Errorf("part[%d].mimeType = %q, want %q", i+1, blob.MIMEType, img.MediaType)
		}
		// The SDK re-encodes raw bytes, so what the API sees is our base64 back.
		if got := base64.StdEncoding.EncodeToString(blob.Data); got != img.Data {
			t.Errorf("part[%d] data = %q, want %q", i+1, got, img.Data)
		}
	}
}

// Undecodable base64 is dropped rather than shipped: one bad image must not
// make the API reject the whole conversation.
func TestBuildGeminiContentsSkipsUndecodableImage(t *testing.T) {
	contents, _ := buildGeminiContents([]domain.Message{
		{Role: domain.RoleUser, Content: "hi", Images: []domain.ToolResultImage{
			{MediaType: "image/png", Data: "not base64!!"},
			{MediaType: "image/png", Data: "Zmlyc3Q="},
		}},
	})
	parts := contents[0].Parts
	if len(parts) != 2 {
		t.Fatalf("parts = %d, want 2 (text + the one decodable image)", len(parts))
	}
	if parts[1].InlineData == nil {
		t.Fatal("the decodable image was dropped too")
	}
}

// Gemini's no-image user turn is unchanged: one text part, nothing else.
func TestBuildGeminiContentsUserMessageWithoutImagesUnchanged(t *testing.T) {
	contents, _ := buildGeminiContents([]domain.Message{
		{Role: domain.RoleUser, Content: "plain text"},
	})
	parts := contents[0].Parts
	if len(parts) != 1 || parts[0].Text != "plain text" || parts[0].InlineData != nil {
		t.Fatalf("parts = %+v, want a single text part", parts)
	}
	if contents[0].Role != genai.RoleUser {
		t.Errorf("role = %q, want user", contents[0].Role)
	}
}
