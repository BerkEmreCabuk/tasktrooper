package storeops

import (
	"strings"
	"testing"
	"time"
)

func TestProvisioningProfileNameDiffersAcrossCalls(t *testing.T) {
	bundleID := "com.example.app"
	t1 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Second)

	n1 := provisioningProfileName(bundleID, t1)
	n2 := provisioningProfileName(bundleID, t2)

	if n1 == n2 {
		t.Fatalf("expected distinct names for different timestamps, got %q both times", n1)
	}
	if !strings.Contains(n1, bundleID) || !strings.Contains(n2, bundleID) {
		t.Fatalf("expected the bundle id %q in both names, got %q and %q", bundleID, n1, n2)
	}
}

func TestProvisioningProfileNameStableForSameInstant(t *testing.T) {
	bundleID := "com.example.app"
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	if provisioningProfileName(bundleID, now) != provisioningProfileName(bundleID, now) {
		t.Fatal("expected the same instant to produce the same name")
	}
}
