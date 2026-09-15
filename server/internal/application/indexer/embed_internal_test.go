package indexer

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCapEmbedContentKeepsShortContentAndCutsLongOnARuneBoundary(t *testing.T) {
	if got := capEmbedContent("short"); got != "short" {
		t.Fatalf("short content changed: %q", got)
	}
	long := strings.Repeat("ş", maxEmbedContentChars)
	got := capEmbedContent(long)
	if len(got) > maxEmbedContentChars {
		t.Fatalf("len = %d, want at most %d", len(got), maxEmbedContentChars)
	}
	if !utf8.ValidString(got) {
		t.Fatal("cut split a UTF-8 sequence")
	}
}

func TestIsTransientEmbedError(t *testing.T) {
	transient := []string{
		`embeddings unreachable: Post "http://127.0.0.1:1/embeddings": read tcp: read: connection reset by peer`,
		"dial tcp 127.0.0.1:1: connect: connection refused",
		"embeddings returned 503: model loading",
	}
	for _, msg := range transient {
		if !isTransientEmbedError(errors.New(msg)) {
			t.Errorf("%q should be transient", msg)
		}
	}
	permanent := []string{
		"embeddings returned 500: inference_failed",
		"embeddings returned 400: bad input",
	}
	for _, msg := range permanent {
		if isTransientEmbedError(errors.New(msg)) {
			t.Errorf("%q should not be transient", msg)
		}
	}
}
