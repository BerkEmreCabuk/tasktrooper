package context

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/gif"  // registered so imageDimensions can read a GIF header
	_ "image/jpeg" // registered so imageDimensions can read a JPEG header
	_ "image/png"  // registered so imageDimensions can read a PNG header
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The vision-token rule providers converge on: a tile of ~750 pixels costs one prompt token. An approximation, but the budget only needs to know six screenshots cost thousands of tokens, not zero.
const pixelsPerImageToken = 750

// Providers downscale before billing (past ~1.15 MP), so one picture never costs more than this; a 12 MP photo must not trigger a pointless purge.
const maxImageTokens = 1600

// Every provider charges a fixed per-image overhead and no image may look free to the budget — that was the whole bug.
const minImageTokens = 85

// Smallest-first base64 probe lengths; most headers answer in the first 4 KB, and past ~72 KB we fall back on size rather than decode a big attachment. Both are multiples of 4 so a prefix is whole base64 quanta.
var imageHeaderProbes = [2]int{4 << 10, 96 << 10}

// imageTokens: read the pixel dimensions out of the header without decoding the pixels; fall back (WebP, truncated data) to scaling the encoded size, where a big unreadable image hits the ceiling either way.
func imageTokens(img domain.ToolResultImage) int {
	if img.Data == "" {
		return 0
	}
	if w, h, ok := imageDimensions(img.Data); ok && w > 0 && h > 0 {
		return clampImageTokens(int64(w)*int64(h), pixelsPerImageToken)
	}
	return clampImageTokens(int64(base64DecodedLen(img.Data)), int64(bytesPerImageToken(img.MediaType)))
}

func messageImageTokens(m domain.Message) int {
	total := 0
	for _, img := range m.Images {
		total += imageTokens(img)
	}
	return total
}

func clampImageTokens(units, perToken int64) int {
	if perToken < 1 {
		perToken = 1
	}
	tokens := (units + perToken - 1) / perToken
	if tokens > maxImageTokens {
		return maxImageTokens
	}
	if tokens < minImageTokens {
		return minImageTokens
	}
	return int(tokens)
}

// imageDimensions decodes only a bounded header prefix, so counting cost is independent of the attachment's size.
func imageDimensions(data string) (int, int, bool) {
	for _, probe := range imageHeaderProbes {
		prefix := data
		if len(prefix) > probe {
			prefix = prefix[:probe]
		}
		raw := make([]byte, base64.StdEncoding.DecodedLen(len(prefix)))
		// A truncated prefix is not valid base64 and wrapped base64 hides newlines, but Decode writes the bytes it did decode — exactly the header we are after.
		n, _ := base64.StdEncoding.Decode(raw, []byte(prefix))
		if n > 0 {
			if cfg, _, err := image.DecodeConfig(bytes.NewReader(raw[:n])); err == nil {
				return cfg.Width, cfg.Height, true
			}
		}
		if len(data) <= probe {
			break
		}
	}
	return 0, 0, false
}

func base64DecodedLen(data string) int {
	n := len(data)
	for n > 0 && data[n-1] == '=' {
		n--
	}
	return n * 3 / 4
}

// Encoded bytes per vision token when the header could not be read, per format — a lossless PNG is ~3x costlier per pixel than a JPEG, and under-counting an unidentified image is the bug.
func bytesPerImageToken(mediaType string) int {
	switch normalizeMediaType(mediaType) {
	case "image/png", "image/bmp", "image/tiff":
		return 300
	case "image/gif":
		return 200
	case "image/jpeg", "image/jpg":
		return 110
	default:
		return 80
	}
}

func normalizeMediaType(mediaType string) string {
	if i := strings.IndexByte(mediaType, ';'); i >= 0 {
		mediaType = mediaType[:i]
	}
	return strings.ToLower(strings.TrimSpace(mediaType))
}
