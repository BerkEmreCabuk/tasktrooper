package context

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ImageBudgetSuite struct {
	suite.Suite
}

func TestImageBudgetSuite(t *testing.T) {
	suite.Run(t, new(ImageBudgetSuite))
}

func (s *ImageBudgetSuite) TestImageTokensFromPNGHeader() {
	img := pngImage(s.T(), 1200, 800, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	// 1200*800 = 960000 pixels / 750 = 1280 tokens.
	s.Equal(1280, imageTokens(img))
}

func (s *ImageBudgetSuite) TestImageTokensFromJPEGHeader() {
	img := jpegImage(s.T(), 640, 480, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	// 640*480 = 307200 pixels / 750 = 409.6 -> 410 tokens.
	s.Equal(410, imageTokens(img))
}

func (s *ImageBudgetSuite) TestImageTokensClampedForHugeImage() {
	img := pngImage(s.T(), 2000, 1500, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	// 3 megapixels would be 4000 tokens; providers downscale first.
	s.Equal(maxImageTokens, imageTokens(img))
}

func (s *ImageBudgetSuite) TestImageTokensFlooredForTinyImage() {
	img := pngImage(s.T(), 8, 8, color.RGBA{R: 255, A: 255})
	s.Equal(minImageTokens, imageTokens(img))
}

func (s *ImageBudgetSuite) TestImageTokensFallsBackToByteSizeWhenHeaderUnreadable() {
	jpg := garbageImage("image/jpeg", 100_000)
	png := garbageImage("image/png", 100_000)
	unknown := garbageImage("image/webp", 100_000)

	// 100000 bytes / 110 = 909.09 -> 910; the PNG ratio is coarser because
	// lossless bytes carry fewer pixels.
	s.Equal(910, imageTokens(jpg))
	s.Equal(334, imageTokens(png))
	s.Greater(imageTokens(unknown), imageTokens(jpg))

	for _, img := range []domain.ToolResultImage{jpg, png, unknown} {
		s.GreaterOrEqual(imageTokens(img), minImageTokens)
		s.LessOrEqual(imageTokens(img), maxImageTokens)
	}
}

func (s *ImageBudgetSuite) TestImageTokensFallbackStaysBounded() {
	s.Equal(maxImageTokens, imageTokens(garbageImage("image/jpeg", 5<<20)))
	s.Equal(minImageTokens, imageTokens(garbageImage("image/jpeg", 12)))
	s.Equal(minImageTokens, imageTokens(domain.ToolResultImage{MediaType: "application/octet-stream", Data: "?"}))
}

func (s *ImageBudgetSuite) TestImageTokensIgnoresMediaTypeParametersAndCase() {
	s.Equal(imageTokens(garbageImage("image/jpeg", 60_000)),
		imageTokens(garbageImage("IMAGE/JPEG; charset=binary", 60_000)))
}

func (s *ImageBudgetSuite) TestImageTokensZeroWithoutData() {
	s.Equal(0, imageTokens(domain.ToolResultImage{MediaType: "image/png"}))
}

func (s *ImageBudgetSuite) TestCountTokensAddsImageCostToText() {
	img := pngImage(s.T(), 1200, 800, color.RGBA{B: 255, A: 255})

	textOnly := []domain.Message{{Role: domain.RoleUser, Content: "abcd"}}
	withImage := []domain.Message{{Role: domain.RoleUser, Content: "abcd", Images: []domain.ToolResultImage{img}}}

	s.Equal(1, CountTokens(textOnly))
	s.Equal(1+1280, CountTokens(withImage))
	// A picture with no words still costs.
	s.Equal(1280, CountTokens([]domain.Message{{Role: domain.RoleUser, Images: []domain.ToolResultImage{img}}}))
	// Empty image slices change nothing about the text accounting.
	s.Equal(1, CountTokens([]domain.Message{{Role: domain.RoleUser, Content: "abcd", Images: []domain.ToolResultImage{}}}))
}

func (s *ImageBudgetSuite) TestApplyDropsImagesWhenNothingElseCanGo() {
	b := Budget{MaxTokens: 1400, ReserveOutput: 0, KeepRecentMessages: 1}
	images := sixImages(s.T())
	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: "sys"},
		{Role: domain.RoleUser, Content: "look at these six screens", Images: images},
	}

	result := b.Apply(messages)

	s.Require().Len(result, 2)
	s.LessOrEqual(CountTokens(result), b.TokenLimit())
	// The words survive; only the pictures were shed.
	s.Equal("look at these six screens", result[1].Content)
	s.Equal(domain.RoleSystem, result[0].Role)
	// Six 1280-token images against a 1400 limit leaves room for exactly one,
	// and the one kept is the newest.
	s.Require().Len(result[1].Images, 1)
	s.Equal(images[5], result[1].Images[0])
}

func (s *ImageBudgetSuite) TestApplyDoesNotMutateCallerImages() {
	b := Budget{MaxTokens: 1400, ReserveOutput: 0, KeepRecentMessages: 1}
	images := sixImages(s.T())
	first := images[0]
	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: "sys"},
		{Role: domain.RoleUser, Content: "look at these six screens", Images: images},
	}

	_ = b.Apply(messages)

	s.Require().Len(messages[1].Images, 6)
	s.Equal(first, messages[1].Images[0])
	s.Require().Len(images, 6)
	s.Equal(first, images[0])
	s.Greater(CountTokens(messages), b.TokenLimit())
}

func (s *ImageBudgetSuite) TestApplyDropsOldestImagesFirstAcrossMessages() {
	b := Budget{MaxTokens: 1400, ReserveOutput: 0, KeepRecentMessages: 2}
	oldImage := pngImage(s.T(), 1200, 800, color.RGBA{R: 255, A: 255})
	newImage := pngImage(s.T(), 1200, 800, color.RGBA{G: 255, A: 255})
	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: "s"},
		{Role: domain.RoleUser, Content: "one", Images: []domain.ToolResultImage{oldImage}},
		{Role: domain.RoleUser, Content: "two", Images: []domain.ToolResultImage{newImage}},
	}

	result := b.Apply(messages)

	s.Require().Len(result, 3)
	s.LessOrEqual(CountTokens(result), b.TokenLimit())
	s.Empty(result[1].Images)
	s.Equal("one", result[1].Content)
	s.Require().Len(result[2].Images, 1)
	s.Equal(newImage, result[2].Images[0])
}

func (s *ImageBudgetSuite) TestApplyKeepsTextWhenEveryImageIsGone() {
	b := Budget{MaxTokens: 20, ReserveOutput: 0, KeepRecentMessages: 1}
	messages := []domain.Message{
		{Role: domain.RoleUser, Content: "only words left", Images: sixImages(s.T())},
	}

	result := b.Apply(messages)

	s.Require().Len(result, 1)
	s.Equal("only words left", result[0].Content)
	s.Empty(result[0].Images)
	s.LessOrEqual(CountTokens(result), b.TokenLimit())
}

func sixImages(t *testing.T) []domain.ToolResultImage {
	t.Helper()
	images := make([]domain.ToolResultImage, 0, 6)
	for i := range 6 {
		images = append(images, pngImage(t, 1200, 800, color.RGBA{R: uint8(10 * i), G: uint8(20 * i), B: uint8(30 * i), A: 255}))
	}
	return images
}

func pngImage(t *testing.T, width, height int, fill color.RGBA) domain.ToolResultImage {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, uniformImage(width, height, fill)); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return domain.ToolResultImage{MediaType: "image/png", Data: base64.StdEncoding.EncodeToString(buf.Bytes())}
}

func jpegImage(t *testing.T, width, height int, fill color.RGBA) domain.ToolResultImage {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, uniformImage(width, height, fill), &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return domain.ToolResultImage{MediaType: "image/jpeg", Data: base64.StdEncoding.EncodeToString(buf.Bytes())}
}

func uniformImage(width, height int, fill color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetRGBA(x, y, fill)
		}
	}
	return img
}

// garbageImage is base64 that no decoder can parse: it exercises the byte-size
// fallback with a media type the estimator can still trust.
func garbageImage(mediaType string, rawBytes int) domain.ToolResultImage {
	raw := bytes.Repeat([]byte{0xAB, 0x03, 0xFE}, rawBytes/3+1)[:rawBytes]
	return domain.ToolResultImage{MediaType: mediaType, Data: base64.StdEncoding.EncodeToString(raw)}
}
