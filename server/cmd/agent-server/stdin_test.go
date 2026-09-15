package main

import (
	"io"
	"testing"
	"time"
)

func TestStdinClosedFiresWhenTheWriterCloses(t *testing.T) {
	r, w := io.Pipe()
	done := stdinClosed(true, r)

	select {
	case <-done:
		t.Fatal("fired while stdin was still open")
	case <-time.After(50 * time.Millisecond):
	}

	_, _ = w.Write([]byte("anything the supervisor sends is ignored\n"))
	_ = w.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("did not fire after stdin closed")
	}
}

func TestStdinClosedIsOffUnlessAsked(t *testing.T) {
	if stdinClosed(false, nil) != nil {
		t.Fatal("a disabled watch must be a nil channel, which never fires")
	}
}
