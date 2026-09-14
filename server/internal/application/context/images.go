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

// pixelsPerImageToken is the vision-token rule the providers converge on: a
// tile of roughly 750 pixels costs one prompt token (width*height/750). It is
// an approximation of tiling, not a billing contract — but the budget only
// needs to know that six screenshots cost thousands of tokens, not zero.
const pixelsPerImageToken = 750

// maxImageTokens ceilings a single image because providers downscale before
// they bill: anything past roughly 1.15 megapixels is resized down, and
// 1.15 MP / 750 px is about 1540 tokens. 1600 is the realistic worst case for
// one picture, so a 12 MP photo must not be counted as 16k tokens and trigger
// a pointless history purge.
const maxImageTokens = 1600

// minImageTokens floors a single image. Every provider pays a fixed per-image
// overhead on top of the pixels (OpenAI bills 85 tokens for a low-detail
// image), and more importantly no image may ever look free to the budget —
// that was the whole bug.
const minImageTokens = 85

// imageHeaderProbes are the base64 prefix lengths handed to the header parser,
// smallest first. A PNG or GIF header lands in the first few hundred bytes and
// a plain JPEG's SOF in the first few KB, so the cheap probe answers almost
// every real image at almost no cost — and CountTokens is called once per
// trim iteration, so the common case has to stay cheap. The large probe exists
// for a JPEG whose EXIF/ICC segments push SOF further in (APP1 alone may be
// 64 KB); past ~72 KB of raw header we give up and use the size fallback
// rather than decoding a 5 MB attachment. Both lengths are multiples of 4 so a
// prefix is always a whole number of base64 quanta.
var imageHeaderProbes = [2]int{4 << 10, 96 << 10}

// imageTokens estimates what one image costs in prompt tokens.
//
// Preferred path: read the pixel dimensions out of the image header without
// decoding the pixels, then apply the pixels-per-token rule.
//
// Fallback for a header we cannot parse (WebP, an unknown format, truncated
// data): scale the encoded byte size by a per-format ratio. It is coarse, but
// a big unreadable image lands on the ceiling either way, which is the answer
// that matters for a budget.
func imageTokens(img domain.ToolResultImage) int {
	if img.Data == "" {
		return 0
	}
	if w, h, ok := imageDimensions(img.Data); ok && w > 0 && h > 0 {
		return clampImageTokens(int64(w)*int64(h), pixelsPerImageToken)
	}
	return clampImageTokens(int64(base64DecodedLen(img.Data)), int64(bytesPerImageToken(img.MediaType)))
}

// messageImageTokens is the image share of one message's token cost.
func messageImageTokens(m domain.Message) int {
	total := 0
	for _, img := range m.Images {
		total += imageTokens(img)
	}
	return total
}

// clampImageTokens divides units (pixels or bytes) by what one token buys and
// holds the result inside the range a provider could plausibly bill.
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

// imageDimensions parses width and height out of the image header, feeding
// image.DecodeConfig a bounded prefix of the decoded bytes so the cost of
// counting is independent of the attachment's size.
func imageDimensions(data string) (int, int, bool) {
	for _, probe := range imageHeaderProbes {
		prefix := data
		if len(prefix) > probe {
			prefix = prefix[:probe]
		}
		raw := make([]byte, base64.StdEncoding.DecodedLen(len(prefix)))
		// A truncated prefix is not valid base64 on its own, and wrapped
		// base64 hides newlines: both make Decode stop early with an error
		// after writing the bytes it did decode, and those bytes are exactly
		// the header we are after.
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

// base64DecodedLen is the raw byte count a base64 string stands for, computed
// without allocating the decoded buffer.
func base64DecodedLen(data string) int {
	n := len(data)
	for n > 0 && data[n-1] == '=' {
		n--
	}
	return n * 3 / 4
}

// bytesPerImageToken is how many encoded bytes one vision token is worth when
// the header could not be read, per format. The ratio has to depend on the
// format: a lossless PNG screenshot spends roughly 0.4 bytes per pixel (~300
// bytes per 750-pixel token) while a quality-80 JPEG photo spends closer to
// 0.15 (~110), and WebP beats JPEG again — one shared ratio would either
// triple-count screenshots or under-count photos. An unrecognized format takes
// the smallest ratio on purpose: over-counting an image we cannot identify is
// safe, under-counting it is the bug.
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
