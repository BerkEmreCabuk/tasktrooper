package llm

import (
	"net/http"
	"testing"
	"time"
)

// The client must drop an idle connection before the local embedder does
// (65s), or a request lands on a socket the server has already closed.
func TestOpenAICompatClientClosesIdleConnectionsBeforeTheEmbedder(t *testing.T) {
	c, ok := newOpenAICompatClientExt("http://127.0.0.1:1", "m", "", time.Minute, nil).(*openAICompatClient)
	if !ok {
		t.Fatal("unexpected client type")
	}
	tr, ok := c.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", c.httpClient.Transport)
	}
	if tr.IdleConnTimeout <= 0 || tr.IdleConnTimeout >= 65*time.Second {
		t.Fatalf("IdleConnTimeout = %s, want a value below the embedder's 65s keep-alive", tr.IdleConnTimeout)
	}
}
