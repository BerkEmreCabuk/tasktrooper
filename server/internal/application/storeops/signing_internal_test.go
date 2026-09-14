package storeops

import (
	"strings"
	"testing"
	"time"
)

// provisioningProfileName is unexported, so this white-box test lives in
// package storeops rather than storeops_test alongside SigningSuite.

// Two profile names minted a second apart for the same bundle ID must
// differ — ASC rejects re-registering a name still occupied by the profile
// it replaced (this service never deletes the ASC-side profile it
// supersedes), so every mint needs an unpredictable-enough, always-moving
// name. now is threaded in explicitly so the "differs across calls"
// contract is verifiable without depending on a real clock or racing the
// second boundary.
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

// Two calls at the exact same instant intentionally collide — the
// uniqueness comes from the timestamp advancing, not from any other
// entropy source, so this documents that boundary rather than asserting
// around it.
func TestProvisioningProfileNameStableForSameInstant(t *testing.T) {
	bundleID := "com.example.app"
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	if provisioningProfileName(bundleID, now) != provisioningProfileName(bundleID, now) {
		t.Fatal("expected the same instant to produce the same name")
	}
}
