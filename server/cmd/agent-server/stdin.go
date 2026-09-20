package main

import (
	"io"
)

func stdinClosed(enabled bool, r io.Reader) <-chan struct{} {
	if !enabled {
		return nil
	}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, r)
		close(done)
	}()
	return done
}
