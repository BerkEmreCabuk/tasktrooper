package bootseed

import (
	"context"
	"testing"
	"time"
)

func TestBootingIsTrueOnlyWhileStepsRun(t *testing.T) {
	svc, _ := newService()
	release := make(chan struct{})
	finished := make(chan struct{})
	svc.AddStep("slow", func(context.Context) error {
		<-release
		return nil
	})
	svc.AddStep("last", func(context.Context) error {
		close(finished)
		return nil
	})

	if svc.Booting() {
		t.Fatal("booting before Ensure was called")
	}
	if err := svc.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !svc.Booting() {
		t.Fatal("not booting while a step is still running")
	}

	close(release)
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("steps never finished")
	}
	deadline := time.Now().Add(3 * time.Second)
	for svc.Booting() {
		if time.Now().After(deadline) {
			t.Fatal("still booting after every step finished")
		}
		time.Sleep(10 * time.Millisecond)
	}

	var none *Service
	if none.Booting() {
		t.Fatal("a nil service reports booting")
	}
}
