package tenantboot

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

// Booting is what lets the agents endpoint tell an empty roster from one that is
// still being written: true while the steps run, false once they have finished.
func TestBootingIsTrueOnlyWhileStepsRun(t *testing.T) {
	svc, _, _ := newService()
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

	id := uuid.New()
	if svc.Booting(id) {
		t.Fatal("booting before the tenant was sighted")
	}
	if err := svc.Sight(context.Background(), tenant.Identity{TenantID: id, Role: tenant.RoleOwner}); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	if !svc.Booting(id) {
		t.Fatal("not booting while a step is still running")
	}

	close(release)
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("steps never finished")
	}
	deadline := time.Now().Add(3 * time.Second)
	for svc.Booting(id) {
		if time.Now().After(deadline) {
			t.Fatal("still booting after every step finished")
		}
		time.Sleep(10 * time.Millisecond)
	}

	var none *Service
	if none.Booting(id) {
		t.Fatal("a nil service reports booting")
	}
}
